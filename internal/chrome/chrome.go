// Package chrome locates a Chromium binary and prints pages to PDF
// through CDP Page.printToPDF via chromedp.
package chrome

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/emulation"
	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"

	"github.com/rvanbaalen/bilihtmltopdf/internal/args"
)

// EnvChromePath is the environment variable that overrides Chromium
// binary discovery in FindChrome.
const EnvChromePath = "WKHTMLTOPDF_CHROME"

// PrintRequest describes one Page.printToPDF invocation: the page to load
// (with its wkhtmltopdf-level options) plus the resolved print parameters
// in CDP units (inches, scale factor).
type PrintRequest struct {
	// ChromePath is the Chromium binary to launch (from FindChrome).
	ChromePath string
	// Page holds the object's load/render settings (input, cookies,
	// headers, JS delay, run-scripts, media type, ...).
	Page args.PageOptions
	// Landscape sets landscape paper orientation.
	Landscape bool
	// PaperWidth is the paper width in inches.
	PaperWidth float64
	// PaperHeight is the paper height in inches.
	PaperHeight float64
	// MarginTop is the top margin in inches.
	MarginTop float64
	// MarginBottom is the bottom margin in inches.
	MarginBottom float64
	// MarginLeft is the left margin in inches.
	MarginLeft float64
	// MarginRight is the right margin in inches.
	MarginRight float64
	// Scale is the CDP scale factor (from --zoom).
	Scale float64
	// PrintBackground prints CSS backgrounds.
	PrintBackground bool
	// GenerateOutline embeds a document outline (CDP
	// generateDocumentOutline) for --outline and TOC extraction.
	GenerateOutline bool
	// PageRanges is a CDP page range string, e.g. "1-3"; empty for all.
	PageRanges string
	// Warn receives runtime compatibility notices (e.g. blocked local
	// file access); nil discards them.
	Warn func(msg string)
}

// FindChrome returns the path of the Chromium binary to use, trying in
// order: the WKHTMLTOPDF_CHROME environment variable, a bundled
// chrome-headless-shell next to this executable, then a system
// Chrome/Chromium/Edge install. Returns an error with a download
// hint when nothing is found.
func FindChrome() (string, error) {
	if p := os.Getenv(EnvChromePath); p != "" {
		if fileExists(p) {
			return p, nil
		}
		return "", fmt.Errorf("%s points to %q, which does not exist", EnvChromePath, p)
	}
	if exe, err := os.Executable(); err == nil {
		for _, p := range bundledShellPaths(filepath.Dir(exe)) {
			if fileExists(p) {
				return p, nil
			}
		}
	}
	for _, cand := range systemChromePaths() {
		if filepath.IsAbs(cand) {
			if fileExists(cand) {
				return cand, nil
			}
			continue
		}
		if p, err := exec.LookPath(cand); err == nil {
			return p, nil
		}
	}
	return "", errors.New("no Chrome, Chromium, or Edge installation found; " +
		"install Google Chrome, or download chrome-headless-shell from " +
		"https://googlechromelabs.github.io/chrome-for-testing/ and point " +
		EnvChromePath + " at its binary")
}

// bundledShellPaths returns where the installers place chrome-headless-shell
// relative to the bilihtmltopdf executable directory: the package layout
// (bin/ beside lib/, e.g. /usr/local/bin + /usr/local/lib) and the archive
// layout (lib/ beside the extracted binary).
func bundledShellPaths(exeDir string) []string {
	name := "chrome-headless-shell"
	if goruntime.GOOS == "windows" {
		name += ".exe"
	}
	return []string{
		filepath.Clean(filepath.Join(exeDir, "..", "lib", "chrome-headless-shell", name)),
		filepath.Clean(filepath.Join(exeDir, "lib", "chrome-headless-shell", name)),
	}
}

// systemChromePaths returns well-known browser locations for the current
// OS; relative entries are $PATH lookups, absolute ones plain paths.
func systemChromePaths() []string {
	switch goruntime.GOOS {
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
		}
	case "windows":
		var paths []string
		for _, root := range []string{os.Getenv("PROGRAMFILES"), os.Getenv("PROGRAMFILES(X86)"), os.Getenv("LOCALAPPDATA")} {
			if root == "" {
				continue
			}
			paths = append(paths,
				filepath.Join(root, `Google\Chrome\Application\chrome.exe`),
				filepath.Join(root, `Chromium\Application\chrome.exe`),
				filepath.Join(root, `Microsoft\Edge\Application\msedge.exe`),
			)
		}
		return paths
	default:
		return []string{
			"google-chrome",
			"google-chrome-stable",
			"chromium",
			"chromium-browser",
			"microsoft-edge",
			// Snap installs live outside the minimal PATH that service
			// managers give web-server processes (php-fpm et al.).
			"/snap/bin/chromium",
			"/snap/bin/chromium-browser",
		}
	}
}

// fileExists reports whether path exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// PrintPDF loads the page described by req in a headless Chromium and
// returns the Page.printToPDF result bytes. Input "-" is read from
// stdin and rendered via Page.setDocumentContent.
func PrintPDF(ctx context.Context, req PrintRequest) ([]byte, error) {
	target, isStdin, err := navigationTarget(req.Page.Input)
	if err != nil {
		return nil, err
	}
	var literalHTML string
	if isStdin {
		raw, err := io.ReadAll(os.Stdin)
		if err != nil {
			return nil, fmt.Errorf("reading stdin: %w", err)
		}
		literalHTML = string(raw)
	}

	pdf, err := printOnce(ctx, req, target, isStdin, literalHTML, false)
	if err != nil && isSandboxStartupError(err) {
		// Ubuntu 23.10+ restricts unprivileged user namespaces via
		// AppArmor, which kills Chromium's sandbox. The original
		// wkhtmltopdf ran entirely unsandboxed, so degrading beats
		// failing — but say so.
		fmt.Fprintln(os.Stderr, "Warning: Chromium sandbox unavailable "+
			"(unprivileged user namespaces are restricted on this system); "+
			"retrying with --no-sandbox. Render only trusted HTML.")
		pdf, err = printOnce(ctx, req, target, isStdin, literalHTML, true)
	}
	return pdf, err
}

// printOnce runs a single render attempt in a fresh Chromium process.
func printOnce(ctx context.Context, req PrintRequest, target string, isStdin bool, literalHTML string, noSandbox bool) ([]byte, error) {
	opts := allocatorOptions(req)
	if noSandbox {
		opts = append(opts, chromedp.NoSandbox)
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	defer cancelAlloc()
	tabCtx, cancelTab := chromedp.NewContext(allocCtx)
	defer cancelTab()

	tasks, err := buildTasks(req, target, isStdin, literalHTML)
	if err != nil {
		return nil, err
	}
	if ic := buildInterception(req, target); ic != nil {
		ic.listen(tabCtx)
		tasks = append(chromedp.Tasks{
			fetch.Enable().WithPatterns(ic.patterns()).WithHandleAuthRequests(ic.handleAuth),
		}, tasks...)
	}

	var pdf []byte
	tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		pdf, _, err = buildPrintParams(req).Do(ctx)
		return err
	}))
	if err := chromedp.Run(tabCtx, tasks); err != nil {
		return nil, fmt.Errorf("rendering %s: %w", req.Page.Input, err)
	}
	return pdf, nil
}

// isSandboxStartupError reports whether err is Chromium refusing to start
// because its sandbox cannot be set up (restricted user namespaces).
func isSandboxStartupError(err error) bool {
	return err != nil && strings.Contains(err.Error(), "No usable sandbox")
}

// allocatorOptions maps the request's process-level settings onto exec
// allocator flags (headless=new plus image/local-file toggles).
func allocatorOptions(req PrintRequest) []chromedp.ExecAllocatorOption {
	opts := append([]chromedp.ExecAllocatorOption{}, chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.ExecPath(req.ChromePath),
		chromedp.Flag("headless", "new"),
		// wkhtmltopdf's Qt loader discarded SSL errors unconditionally;
		// drop-in behavior means self-signed and mismatched certs must
		// not silently kill page or stylesheet loads.
		chromedp.Flag("ignore-certificate-errors", true),
	)
	if !req.Page.LoadImages {
		opts = append(opts, chromedp.Flag("blink-settings", "imagesEnabled=false"))
	}
	if req.Page.EnableLocalFileAccess {
		opts = append(opts, chromedp.Flag("allow-file-access-from-files", true))
	}
	return opts
}

// buildTasks assembles the pre-navigation setup, navigation, and
// post-load wait actions for one page render.
func buildTasks(req PrintRequest, target string, isStdin bool, literalHTML string) (chromedp.Tasks, error) {
	p := req.Page
	var tasks chromedp.Tasks

	if !p.EnableJavascript {
		tasks = append(tasks, emulation.SetScriptExecutionDisabled(true))
	}

	if headers := extraHeaders(p); len(headers) > 0 {
		tasks = append(tasks, network.Enable(), network.SetExtraHTTPHeaders(headers))
	}
	if cookieTasks := cookieActions(p.Cookies, target); len(cookieTasks) > 0 {
		tasks = append(tasks, network.Enable())
		tasks = append(tasks, cookieTasks...)
	}

	if p.ViewportSize != "" {
		w, h, err := parseViewport(p.ViewportSize)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, chromedp.EmulateViewport(w, h))
	}

	media := "screen"
	if p.PrintMediaType {
		media = "print"
	}
	tasks = append(tasks, emulation.SetEmulatedMedia().WithMedia(media))

	var styleScript string
	if p.UserStyleSheet != "" {
		var err error
		styleScript, err = userStyleScript(p.UserStyleSheet)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, chromedp.ActionFunc(func(ctx context.Context) error {
			_, err := page.AddScriptToEvaluateOnNewDocument(styleScript).Do(ctx)
			return err
		}))
	}

	if isStdin {
		tasks = append(tasks, chromedp.Navigate("about:blank"), setDocumentContent(literalHTML))
		// setDocumentContent replaces the DOM, dropping the injected
		// style; re-apply it directly.
		if styleScript != "" {
			tasks = append(tasks, evalScript(styleScript))
		}
	} else {
		tasks = append(tasks, chromedp.Navigate(target))
	}

	for _, s := range p.RunScripts {
		tasks = append(tasks, evalScript(s))
	}
	if p.JavascriptDelay > 0 {
		tasks = append(tasks, chromedp.Sleep(time.Duration(p.JavascriptDelay)*time.Millisecond))
	}
	if p.WindowStatus != "" {
		tasks = append(tasks, waitWindowStatus(p.WindowStatus))
	}
	return tasks, nil
}

// buildPrintParams maps a PrintRequest onto CDP Page.printToPDF
// parameters. Zero paper/scale values fall back to Chrome defaults.
func buildPrintParams(req PrintRequest) *page.PrintToPDFParams {
	p := page.PrintToPDF().
		WithLandscape(req.Landscape).
		WithPrintBackground(req.PrintBackground).
		WithMarginTop(req.MarginTop).
		WithMarginBottom(req.MarginBottom).
		WithMarginLeft(req.MarginLeft).
		WithMarginRight(req.MarginRight).
		WithGenerateDocumentOutline(req.GenerateOutline).
		// Chrome emits no outline unless the PDF is also tagged.
		WithGenerateTaggedPDF(req.GenerateOutline)
	if req.PaperWidth > 0 && req.PaperHeight > 0 {
		p = p.WithPaperWidth(req.PaperWidth).WithPaperHeight(req.PaperHeight)
	}
	if req.Scale > 0 {
		p = p.WithScale(req.Scale)
	}
	if req.PageRanges != "" {
		p = p.WithPageRanges(req.PageRanges)
	}
	return p
}

// navigationTarget classifies an input as stdin ("-") or a navigable
// URL, converting bare file paths to file:// URLs.
func navigationTarget(input string) (target string, isStdin bool, err error) {
	if input == "-" {
		return "", true, nil
	}
	lower := strings.ToLower(input)
	for _, scheme := range []string{"http://", "https://", "file://"} {
		if strings.HasPrefix(lower, scheme) {
			return input, false, nil
		}
	}
	abs, err := filepath.Abs(input)
	if err != nil {
		return "", false, fmt.Errorf("resolving input path %q: %w", input, err)
	}
	p := filepath.ToSlash(abs)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u := url.URL{Scheme: "file", Path: p}
	return u.String(), false, nil
}

// extraHeaders returns the headers applied to every request:
// --custom-header values only under --custom-header-propagation.
// Without propagation the headers ride main-document requests only (via
// interception), and --username/--password answer auth challenges
// instead of being sent preemptively, both matching wkhtmltopdf.
func extraHeaders(p args.PageOptions) network.Headers {
	headers := network.Headers{}
	if p.CustomHeaderPropagation {
		for name, value := range p.CustomHeaders {
			headers[name] = value
		}
	}
	return headers
}

// basicAuthHeader returns the Authorization header value for HTTP
// basic auth.
func basicAuthHeader(username, password string) string {
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(username+":"+password))
}

// interception makes per-request decisions through CDP Fetch pausing:
// blocking file:// subresources unless allowed (wkhtmltopdf's
// blockLocalFileAccess), adding non-propagated custom headers to
// main-document requests only, and answering basic-auth challenges.
type interception struct {
	blockLocal bool
	allowed    []string          // canonical paths exempt from blocking
	docHeaders map[string]string // headers for Document requests only
	username   string
	password   string
	handleAuth bool
	warn       func(string)

	mu       sync.Mutex
	authSent map[string]bool // URLs already offered credentials once
}

// buildInterception returns the interception a request needs, or nil
// when plain loading suffices.
func buildInterception(req PrintRequest, target string) *interception {
	p := req.Page
	ic := &interception{warn: req.Warn, authSent: map[string]bool{}}
	if ic.warn == nil {
		ic.warn = func(string) {}
	}
	if !p.EnableLocalFileAccess {
		ic.blockLocal = true
		// The input file itself is always readable, like the fork's
		// networkAccessManager.allow(url.toLocalFile()).
		if u, err := url.Parse(target); err == nil && u.Scheme == "file" {
			ic.allow(u.Path)
		}
		for _, a := range p.AllowedPaths {
			ic.allow(a)
		}
	}
	if len(p.CustomHeaders) > 0 && !p.CustomHeaderPropagation {
		ic.docHeaders = p.CustomHeaders
	}
	if p.Username != "" || p.Password != "" {
		ic.handleAuth = true
		ic.username, ic.password = p.Username, p.Password
	}
	if !ic.blockLocal && ic.docHeaders == nil && !ic.handleAuth {
		return nil
	}
	return ic
}

// allow records path (file or directory) as exempt from local-file
// blocking, in canonical form.
func (ic *interception) allow(path string) {
	if p := canonicalPath(path); p != "" {
		ic.allowed = append(ic.allowed, p)
	}
}

// allowedPath reports whether path or any of its ancestor directories
// was allowed, mirroring the fork's parent-walk in createRequest.
func (ic *interception) allowedPath(path string) bool {
	p := canonicalPath(path)
	for old := ""; p != old && p != ""; old, p = p, filepath.Dir(p) {
		for _, a := range ic.allowed {
			if a == p {
				return true
			}
		}
	}
	return false
}

// canonicalPath cleans path to an absolute, symlink-resolved form so
// allowlist comparisons cannot be dodged with ../ or links.
func canonicalPath(path string) string {
	p, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	// Nonexistent leaf (e.g. a request for a missing file): resolve its
	// parent so symlinked directories like macOS /tmp still compare.
	if r, err := filepath.EvalSymlinks(filepath.Dir(p)); err == nil {
		return filepath.Join(r, filepath.Base(p))
	}
	return p
}

// patterns returns the Fetch request patterns to pause on. Auth
// handling needs every request paused; otherwise only the schemes and
// resource types with pending decisions are intercepted.
func (ic *interception) patterns() []*fetch.RequestPattern {
	if ic.handleAuth {
		return []*fetch.RequestPattern{{URLPattern: "*"}}
	}
	var pats []*fetch.RequestPattern
	if ic.blockLocal {
		pats = append(pats, &fetch.RequestPattern{URLPattern: "file://*"})
	}
	if ic.docHeaders != nil {
		pats = append(pats, &fetch.RequestPattern{URLPattern: "*", ResourceType: network.ResourceTypeDocument})
	}
	return pats
}

// listen attaches the Fetch event handlers to the tab context. Paused
// requests are resolved on goroutines because CDP commands cannot be
// issued from inside an event callback.
func (ic *interception) listen(ctx context.Context) {
	chromedp.ListenTarget(ctx, func(ev interface{}) {
		switch e := ev.(type) {
		case *fetch.EventRequestPaused:
			go ic.onRequest(executorCtx(ctx), e)
		case *fetch.EventAuthRequired:
			go ic.onAuth(executorCtx(ctx), e)
		}
	})
}

// executorCtx returns ctx bound to the tab's CDP executor so fetch
// commands can run outside chromedp.Run.
func executorCtx(ctx context.Context) context.Context {
	return cdp.WithExecutor(ctx, chromedp.FromContext(ctx).Target)
}

// onRequest resolves one paused request: fail blocked local files,
// add custom headers to document requests, continue everything else.
func (ic *interception) onRequest(ctx context.Context, e *fetch.EventRequestPaused) {
	if ic.blockLocal {
		if u, err := url.Parse(e.Request.URL); err == nil && u.Scheme == "file" && !ic.allowedPath(u.Path) {
			ic.warn("Blocked access to file " + u.Path)
			_ = fetch.FailRequest(e.RequestID, network.ErrorReasonAccessDenied).Do(ctx)
			return
		}
	}
	if ic.docHeaders != nil && e.ResourceType == network.ResourceTypeDocument {
		headers := make([]*fetch.HeaderEntry, 0, len(e.Request.Headers)+len(ic.docHeaders))
		for name, value := range e.Request.Headers {
			if s, ok := value.(string); ok && !headerIn(name, ic.docHeaders) {
				headers = append(headers, &fetch.HeaderEntry{Name: name, Value: s})
			}
		}
		for name, value := range ic.docHeaders {
			headers = append(headers, &fetch.HeaderEntry{Name: name, Value: value})
		}
		_ = fetch.ContinueRequest(e.RequestID).WithHeaders(headers).Do(ctx)
		return
	}
	_ = fetch.ContinueRequest(e.RequestID).Do(ctx)
}

// headerIn reports whether name is present in headers, case-insensitively.
func headerIn(name string, headers map[string]string) bool {
	for h := range headers {
		if strings.EqualFold(h, name) {
			return true
		}
	}
	return false
}

// onAuth answers a server auth challenge with the --username/--password
// credentials once per URL, then cancels to avoid a retry loop.
func (ic *interception) onAuth(ctx context.Context, e *fetch.EventAuthRequired) {
	resp := &fetch.AuthChallengeResponse{
		Response: fetch.AuthChallengeResponseResponseProvideCredentials,
		Username: ic.username,
		Password: ic.password,
	}
	ic.mu.Lock()
	if ic.authSent[e.Request.URL] {
		resp = &fetch.AuthChallengeResponse{Response: fetch.AuthChallengeResponseResponseCancelAuth}
	} else {
		ic.authSent[e.Request.URL] = true
	}
	ic.mu.Unlock()
	_ = fetch.ContinueWithAuth(e.RequestID, resp).Do(ctx)
}

// cookieActions builds Network.setCookie actions scoped to the target
// URL; non-http(s) targets take no cookies. Values are percent-decoded
// per the wkhtmltopdf convention ("value should be url encoded"),
// falling back to the raw value when decoding fails.
func cookieActions(cookies map[string]string, target string) []chromedp.Action {
	if len(cookies) == 0 {
		return nil
	}
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil
	}
	var actions []chromedp.Action
	for name, value := range cookies {
		// PathUnescape matches QUrl::fromPercentEncoding: '+' stays '+'.
		if dec, err := url.PathUnescape(value); err == nil {
			value = dec
		}
		actions = append(actions, network.SetCookie(name, value).WithURL(target))
	}
	return actions
}

// parseViewport parses a --viewport-size value like "1280x1024".
func parseViewport(size string) (width, height int64, err error) {
	w, h, ok := strings.Cut(size, "x")
	if ok {
		width, err = strconv.ParseInt(w, 10, 64)
		if err == nil {
			height, err = strconv.ParseInt(h, 10, 64)
		}
	}
	if !ok || err != nil || width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("invalid --viewport-size %q, expected WIDTHxHEIGHT", size)
	}
	return width, height, nil
}

// userStyleScript returns JS that applies --user-style-sheet: remote
// URLs become a <link>, local files are read and inlined as <style>.
func userStyleScript(sheet string) (string, error) {
	lower := strings.ToLower(sheet)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return styleLinkScript(sheet), nil
	}
	css, err := os.ReadFile(sheet)
	if err != nil {
		return "", fmt.Errorf("reading --user-style-sheet: %w", err)
	}
	return styleInjectionScript(string(css)), nil
}

// styleInjectionScript returns JS appending css in a <style> element
// once the DOM is available.
func styleInjectionScript(css string) string {
	lit, _ := json.Marshal(css)
	return fmt.Sprintf(`(() => {
  const add = () => {
    const s = document.createElement('style');
    s.textContent = %s;
    (document.head || document.documentElement).appendChild(s);
  };
  if (document.readyState === 'loading') { document.addEventListener('DOMContentLoaded', add); } else { add(); }
})();`, lit)
}

// styleLinkScript returns JS appending a stylesheet <link> for href
// once the DOM is available.
func styleLinkScript(href string) string {
	lit, _ := json.Marshal(href)
	return fmt.Sprintf(`(() => {
  const add = () => {
    const l = document.createElement('link');
    l.rel = 'stylesheet';
    l.href = %s;
    (document.head || document.documentElement).appendChild(l);
  };
  if (document.readyState === 'loading') { document.addEventListener('DOMContentLoaded', add); } else { add(); }
})();`, lit)
}

// setDocumentContent replaces the main frame's document with html.
func setDocumentContent(html string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		tree, err := page.GetFrameTree().Do(ctx)
		if err != nil {
			return err
		}
		return page.SetDocumentContent(tree.Frame.ID, html).Do(ctx)
	})
}

// evalScript evaluates a JS expression, ignoring its result and any
// page-side exception (wkhtmltopdf --run-script is best-effort).
func evalScript(expr string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		_, _, err := cdpruntime.Evaluate(expr).Do(ctx)
		return err
	})
}

// waitWindowStatus polls window.status every 50ms until it equals
// want or the context expires (--window-status).
func waitWindowStatus(want string) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		for {
			var status string
			if err := chromedp.Evaluate(`String(window.status)`, &status).Do(ctx); err != nil {
				return err
			}
			if status == want {
				return nil
			}
			select {
			case <-ctx.Done():
				return fmt.Errorf("timed out waiting for window.status == %q", want)
			case <-time.After(50 * time.Millisecond):
			}
		}
	})
}
