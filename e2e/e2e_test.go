// Package e2e runs the built wkhtmltopdf binary against real fixtures in
// testdata/e2e and asserts on the produced PDFs with pdftotext/pdfimages
// and pdfcpu (via internal/pdfops). Requires a local Chrome/Chromium and
// poppler; tests skip when either is missing.
package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/rvanbaalen/bilihtmltopdf/internal/chrome"
	"github.com/rvanbaalen/bilihtmltopdf/internal/pdfops"
)

// binPath is the wkhtmltopdf binary built by TestMain.
var binPath string

// fixture returns the absolute path of a testdata/e2e fixture file.
func fixture(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "testdata", "e2e", name))
	if err != nil {
		t.Fatalf("resolving fixture %s: %v", name, err)
	}
	return p
}

// TestMain builds the binary once and verifies the external tool deps.
func TestMain(m *testing.M) {
	if _, err := exec.LookPath("pdftotext"); err != nil {
		fmt.Fprintln(os.Stderr, "skipping e2e: pdftotext (poppler) not installed")
		return
	}
	if _, err := chrome.FindChrome(); err != nil {
		fmt.Fprintln(os.Stderr, "skipping e2e: "+err.Error())
		return
	}
	dir, err := os.MkdirTemp("", "wkhtmltopdf-e2e-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, "e2e: temp dir: "+err.Error())
		os.Exit(1)
	}
	defer os.RemoveAll(dir)
	binPath = filepath.Join(dir, "wkhtmltopdf")
	build := exec.Command("go", "build", "-o", binPath, "github.com/rvanbaalen/bilihtmltopdf/cmd/wkhtmltopdf")
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "e2e: building binary: "+err.Error())
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// runBin executes the binary and returns exit code, stdout, and stderr.
func runBin(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	cmd := exec.Command(binPath, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	code := 0
	if err != nil {
		ee, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("running %v: %v", args, err)
		}
		code = ee.ExitCode()
	}
	return code, out.String(), errb.String()
}

// convert runs the binary expecting success and returns stderr.
func convert(t *testing.T, args ...string) string {
	t.Helper()
	code, _, stderr := runBin(t, args...)
	if code != 0 {
		t.Fatalf("wkhtmltopdf %v: exit %d\nstderr:\n%s", args, code, stderr)
	}
	return stderr
}

// pageText extracts one page's text with pdftotext -layout.
func pageText(t *testing.T, pdf string, page int) string {
	t.Helper()
	out, err := exec.Command("pdftotext", "-layout",
		"-f", fmt.Sprint(page), "-l", fmt.Sprint(page), pdf, "-").Output()
	if err != nil {
		t.Fatalf("pdftotext %s page %d: %v", pdf, page, err)
	}
	return string(out)
}

// pageCount returns the PDF's page count via pdfops.
func pageCount(t *testing.T, pdf string) int {
	t.Helper()
	b, err := os.ReadFile(pdf)
	if err != nil {
		t.Fatalf("reading %s: %v", pdf, err)
	}
	n, err := pdfops.PageCount(b)
	if err != nil {
		t.Fatalf("page count of %s: %v", pdf, err)
	}
	return n
}

// wantContains fails unless text contains all wanted substrings.
func wantContains(t *testing.T, what, text string, wanted ...string) {
	t.Helper()
	for _, w := range wanted {
		if !strings.Contains(text, w) {
			t.Errorf("%s: missing %q in:\n%s", what, w, text)
		}
	}
}

// TestModernCSS asserts @layer cascade, @container queries, and grid
// support render into the PDF, across two pages.
func TestModernCSS(t *testing.T) {
	out := filepath.Join(t.TempDir(), "modern.pdf")
	convert(t, "-q", fixture(t, "modern.html"), out)

	if n := pageCount(t, out); n != 2 {
		t.Errorf("page count = %d, want 2", n)
	}
	p1 := pageText(t, out, 1)
	// pdftotext can swallow a hyphen at a wrap point, so match prefixes.
	wantContains(t, "page 1", p1, "LAYER-CASCADE", "CONTAINER-QUERY", "GRID-SUPPORTED", "grid 1", "grid 3")
	if strings.Contains(p1, "LAYER-BASE") {
		t.Errorf("page 1: @layer cascade order not honored (base layer won):\n%s", p1)
	}
	if strings.Contains(p1, "GRID-NOT-SUPPORTED") {
		t.Errorf("page 1: @supports (display: grid) not detected:\n%s", p1)
	}
	wantContains(t, "page 2", pageText(t, out, 2), "PAGE2-MARKER")
}

// TestGeneratedHeaders asserts --header-left/--header-center with
// [page]/[topage] and --footer-line appear on every page.
func TestGeneratedHeaders(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hdr.pdf")
	convert(t, "-q",
		"--header-left", "ACME-LEFT",
		"--header-center", "[page]/[topage]",
		"--footer-line",
		fixture(t, "multipage.html"), out)

	if n := pageCount(t, out); n != 3 {
		t.Fatalf("page count = %d, want 3", n)
	}
	for p := 1; p <= 3; p++ {
		wantContains(t, fmt.Sprintf("page %d", p), pageText(t, out, p),
			"ACME-LEFT", fmt.Sprintf("%d/3", p))
	}
}

// TestHeaderHTML asserts a --header-html file renders with its local
// image inlined and [page]/[topage] substituted on every page.
func TestHeaderHTML(t *testing.T) {
	out := filepath.Join(t.TempDir(), "hdrhtml.pdf")
	convert(t, "-q", "--header-html", fixture(t, "header.html"),
		fixture(t, "multipage.html"), out)

	for p := 1; p <= 3; p++ {
		wantContains(t, fmt.Sprintf("page %d", p), pageText(t, out, p),
			"HDRHTML-MARKER", fmt.Sprintf("page %d of 3", p))
	}
	if _, err := exec.LookPath("pdfimages"); err != nil {
		t.Skip("pdfimages not installed; skipping embedded-image assertion")
	}
	list, err := exec.Command("pdfimages", "-list", out).Output()
	if err != nil {
		t.Fatalf("pdfimages -list: %v", err)
	}
	for p := 1; p <= 3; p++ {
		re := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*%d\s+\d+\s+image`, p))
		if !re.Match(list) {
			t.Errorf("no embedded image on page %d:\n%s", p, list)
		}
	}
}

// TestMultiInputTOC asserts cover + toc + two chapters merge in order,
// the TOC shows real final page numbers, and the output carries an
// outline (bookmark) tree.
func TestMultiInputTOC(t *testing.T) {
	out := filepath.Join(t.TempDir(), "multi.pdf")
	convert(t, "-q",
		"cover", fixture(t, "cover.html"),
		"toc",
		"page", fixture(t, "ch1.html"),
		"page", fixture(t, "ch2.html"),
		out)

	if n := pageCount(t, out); n != 4 {
		t.Fatalf("page count = %d, want 4", n)
	}
	wantContains(t, "page 1 (cover)", pageText(t, out, 1), "COVER-MARKER")
	tocText := pageText(t, out, 2)
	wantContains(t, "page 2 (toc)", tocText, "Table of Contents")
	for _, want := range []*regexp.Regexp{
		regexp.MustCompile(`Chapter One\s+3`),
		regexp.MustCompile(`Section One Point One\s+3`),
		regexp.MustCompile(`Chapter Two\s+4`),
	} {
		if !want.MatchString(tocText) {
			t.Errorf("toc page: no match for %v in:\n%s", want, tocText)
		}
	}
	wantContains(t, "page 3", pageText(t, out, 3), "CH1-MARKER")
	wantContains(t, "page 4", pageText(t, out, 4), "CH2-MARKER")

	pdf, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("reading output: %v", err)
	}
	entries, err := pdfops.ReadOutline(pdf)
	if err != nil {
		t.Fatalf("reading outline: %v", err)
	}
	byTitle := map[string]pdfops.OutlineEntry{}
	for _, e := range entries {
		byTitle[e.Title] = e
	}
	for title, page := range map[string]int{"Chapter One": 3, "Chapter Two": 4} {
		e, ok := byTitle[title]
		if !ok {
			t.Errorf("outline: missing bookmark %q in %+v", title, entries)
			continue
		}
		if e.Page != page {
			t.Errorf("outline %q: page %d, want %d", title, e.Page, page)
		}
	}
}

// TestStdoutOutput asserts output path "-" writes the PDF to stdout.
func TestStdoutOutput(t *testing.T) {
	code, stdout, stderr := runBin(t, "-q", fixture(t, "cover.html"), "-")
	if code != 0 {
		t.Fatalf("exit %d\nstderr:\n%s", code, stderr)
	}
	if !strings.HasPrefix(stdout, "%PDF-") {
		t.Fatalf("stdout does not start with %%PDF- (got %q...)", stdout[:min(16, len(stdout))])
	}
	out := filepath.Join(t.TempDir(), "stdout.pdf")
	if err := os.WriteFile(out, []byte(stdout), 0o644); err != nil {
		t.Fatal(err)
	}
	if n := pageCount(t, out); n != 1 {
		t.Errorf("piped PDF page count = %d, want 1", n)
	}
}

// TestUnsupportedFlag asserts an unsupported legacy flag warns but
// succeeds, that -q mutes the warning, and that --strict fails the run.
func TestUnsupportedFlag(t *testing.T) {
	out := filepath.Join(t.TempDir(), "warn.pdf")
	code, _, stderr := runBin(t, "--disable-smart-shrinking", fixture(t, "cover.html"), out)
	if code != 0 {
		t.Errorf("non-strict: exit %d, want 0\nstderr:\n%s", code, stderr)
	}
	wantContains(t, "non-strict stderr", stderr, "Warning:", "--disable-smart-shrinking")
	if _, err := os.Stat(out); err != nil {
		t.Errorf("non-strict: output not written: %v", err)
	}

	// -q is wkhtmltopdf's --log-level none: warnings are muted too.
	outq := filepath.Join(t.TempDir(), "quiet.pdf")
	code, _, stderr = runBin(t, "-q", "--disable-smart-shrinking", fixture(t, "cover.html"), outq)
	if code != 0 {
		t.Errorf("quiet: exit %d, want 0\nstderr:\n%s", code, stderr)
	}
	if stderr != "" {
		t.Errorf("quiet: stderr must be empty, got:\n%s", stderr)
	}

	out2 := filepath.Join(t.TempDir(), "strict.pdf")
	code, _, stderr = runBin(t, "-q", "--strict", "--disable-smart-shrinking", fixture(t, "cover.html"), out2)
	if code != 1 {
		t.Errorf("strict: exit %d, want 1\nstderr:\n%s", code, stderr)
	}
	wantContains(t, "strict stderr", stderr, "--disable-smart-shrinking")
	if _, err := os.Stat(out2); err == nil {
		t.Errorf("strict: output written despite fatal warning")
	}
}

// TestUnknownFlag asserts a typo'd switch exits 1 like wkhtmltopdf
// instead of silently treating its value as an input page.
func TestUnknownFlag(t *testing.T) {
	out := filepath.Join(t.TempDir(), "unknown.pdf")
	code, _, stderr := runBin(t, "--frobnicate", "value", fixture(t, "cover.html"), out)
	if code != 1 {
		t.Errorf("exit %d, want 1\nstderr:\n%s", code, stderr)
	}
	wantContains(t, "stderr", stderr, "unknown switch --frobnicate")
	if _, err := os.Stat(out); err == nil {
		t.Error("output written despite unknown switch")
	}
}

// TestLocalFileAccessBlocking asserts local subresources are blocked by
// default (wkhtmltopdf 0.12.6 behavior) and load again with
// --enable-local-file-access or a matching --allow.
func TestLocalFileAccessBlocking(t *testing.T) {
	if _, err := exec.LookPath("pdfimages"); err != nil {
		t.Skip("pdfimages not installed")
	}

	// logo.png is 1x1; a blocked load leaves Chromium's broken-image
	// placeholder instead, so match the intrinsic size.
	logoRe := regexp.MustCompile(`(?m)^\s*1\s+\d+\s+image\s+1\s+1\b`)

	// header.html embeds logo.png; rendered as a page it exercises a
	// local file loading a sibling local file.
	blocked := filepath.Join(t.TempDir(), "blocked.pdf")
	stderr := convert(t, fixture(t, "header.html"), blocked)
	wantContains(t, "blocked stderr", stderr, "Blocked access to file", "logo.png")
	list, err := exec.Command("pdfimages", "-list", blocked).Output()
	if err != nil {
		t.Fatalf("pdfimages -list: %v", err)
	}
	if logoRe.Match(list) {
		t.Errorf("blocked render still embeds the image:\n%s", list)
	}

	allowed := filepath.Join(t.TempDir(), "allowed.pdf")
	stderr = convert(t, "--enable-local-file-access", fixture(t, "header.html"), allowed)
	if strings.Contains(stderr, "Blocked access") {
		t.Errorf("--enable-local-file-access still blocked:\n%s", stderr)
	}
	list, err = exec.Command("pdfimages", "-list", allowed).Output()
	if err != nil {
		t.Fatalf("pdfimages -list: %v", err)
	}
	if !logoRe.Match(list) {
		t.Errorf("--enable-local-file-access did not load the image:\n%s", list)
	}

	viaAllow := filepath.Join(t.TempDir(), "via-allow.pdf")
	stderr = convert(t, "--allow", filepath.Dir(fixture(t, "logo.png")), fixture(t, "header.html"), viaAllow)
	if strings.Contains(stderr, "Blocked access") {
		t.Errorf("--allow did not exempt the fixture dir:\n%s", stderr)
	}
}

// TestMultiObjectContinuousNumbering asserts header/footer page numbers run
// continuously across multiple input documents (as wkhtmltopdf did), since
// headers/footers are composited onto the merged document rather than
// stamped per object.
func TestMultiObjectContinuousNumbering(t *testing.T) {
	out := filepath.Join(t.TempDir(), "multi.pdf")
	stderr := convert(t, "-q", "--footer-center", "[page]/[topage]",
		fixture(t, "ch1.html"), fixture(t, "ch2.html"), out)
	if strings.Contains(stderr, "restart at 1") {
		t.Errorf("composited numbering must not warn about restarting:\n%s", stderr)
	}
	n := pageCount(t, out)
	if n < 2 {
		t.Fatalf("expected at least 2 pages across two inputs, got %d", n)
	}
	// Last page must read "N/N" — proof numbering is continuous and the
	// total spans both documents.
	last := pageText(t, out, n)
	want := fmt.Sprintf("%d/%d", n, n)
	if !strings.Contains(last, want) {
		t.Errorf("last page footer = %q, want %q", last, want)
	}
	// First page must read "1/N", not "1/1".
	if first := pageText(t, out, 1); !strings.Contains(first, fmt.Sprintf("1/%d", n)) {
		t.Errorf("first page footer = %q, want 1/%d", first, n)
	}
}

// TestJavascriptDelay asserts --javascript-delay waits long enough for a
// 500ms deferred DOM write, and that a zero delay does not.
func TestJavascriptDelay(t *testing.T) {
	out := filepath.Join(t.TempDir(), "delay.pdf")
	convert(t, "-q", "--javascript-delay", "1200", fixture(t, "delayed.html"), out)
	wantContains(t, "delayed output", pageText(t, out, 1), "STATIC-MARKER", "DELAYED-CONTENT-MARKER")

	out2 := filepath.Join(t.TempDir(), "nodelay.pdf")
	convert(t, "-q", "--javascript-delay", "0", fixture(t, "delayed.html"), out2)
	if txt := pageText(t, out2, 1); strings.Contains(txt, "DELAYED-CONTENT-MARKER") {
		t.Errorf("zero delay: deferred content unexpectedly present:\n%s", txt)
	}
}
