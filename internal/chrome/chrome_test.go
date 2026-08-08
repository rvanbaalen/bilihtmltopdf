package chrome

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rvanbaalen/bilihtmltopdf/internal/args"
)

func TestNavigationTarget(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		isStdin bool
	}{
		{"http URL", "http://example.com/a", "http://example.com/a", false},
		{"https URL", "https://example.com/a?x=1", "https://example.com/a?x=1", false},
		{"file URL", "file:///tmp/x.html", "file:///tmp/x.html", false},
		{"stdin", "-", "", true},
		{"absolute path", "/tmp/in.html", "file:///tmp/in.html", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, isStdin, err := navigationTarget(tt.input)
			if err != nil {
				t.Fatalf("navigationTarget(%q): %v", tt.input, err)
			}
			if got != tt.want || isStdin != tt.isStdin {
				t.Errorf("navigationTarget(%q) = (%q, %v), want (%q, %v)",
					tt.input, got, isStdin, tt.want, tt.isStdin)
			}
		})
	}
}

func TestNavigationTargetRelativePath(t *testing.T) {
	got, isStdin, err := navigationTarget("testdata/in.html")
	if err != nil || isStdin {
		t.Fatalf("navigationTarget: err=%v isStdin=%v", err, isStdin)
	}
	if !strings.HasPrefix(got, "file:///") || !strings.HasSuffix(got, "/testdata/in.html") {
		t.Errorf("relative path mapped to %q, want absolute file:// URL", got)
	}
}

func TestBuildPrintParams(t *testing.T) {
	req := PrintRequest{
		Landscape:       true,
		PaperWidth:      8.27,
		PaperHeight:     11.69,
		MarginTop:       0.5,
		MarginBottom:    0.6,
		MarginLeft:      0.7,
		MarginRight:     0.8,
		Scale:           1.3,
		FooterTemplate:  `<div class="pageNumber"></div>`,
		PrintBackground: true,
		GenerateOutline: true,
		PageRanges:      "2-4",
	}
	p := buildPrintParams(req)

	if !p.Landscape || !p.PrintBackground || !p.GenerateDocumentOutline {
		t.Errorf("bool flags not mapped: %+v", p)
	}
	if p.PaperWidth != 8.27 || p.PaperHeight != 11.69 {
		t.Errorf("paper = %vx%v, want 8.27x11.69", p.PaperWidth, p.PaperHeight)
	}
	if p.MarginTop != 0.5 || p.MarginBottom != 0.6 || p.MarginLeft != 0.7 || p.MarginRight != 0.8 {
		t.Errorf("margins not mapped: %+v", p)
	}
	if p.Scale != 1.3 {
		t.Errorf("scale = %v, want 1.3", p.Scale)
	}
	if p.PageRanges != "2-4" {
		t.Errorf("pageRanges = %q, want 2-4", p.PageRanges)
	}
	if !p.DisplayHeaderFooter {
		t.Error("footer set but DisplayHeaderFooter false")
	}
	if p.FooterTemplate != req.FooterTemplate {
		t.Errorf("footer = %q", p.FooterTemplate)
	}
	if p.HeaderTemplate != "<span></span>" {
		t.Errorf("empty header should be blanked, got %q", p.HeaderTemplate)
	}
}

func TestBuildPrintParamsNoHeaderFooter(t *testing.T) {
	p := buildPrintParams(PrintRequest{PaperWidth: 8.5, PaperHeight: 11})
	if p.DisplayHeaderFooter {
		t.Error("DisplayHeaderFooter true without templates")
	}
	if p.HeaderTemplate != "" || p.FooterTemplate != "" {
		t.Errorf("templates set without input: %q / %q", p.HeaderTemplate, p.FooterTemplate)
	}
}

func TestParseViewport(t *testing.T) {
	w, h, err := parseViewport("1280x1024")
	if err != nil || w != 1280 || h != 1024 {
		t.Errorf("parseViewport(1280x1024) = (%d, %d, %v)", w, h, err)
	}
	for _, bad := range []string{"", "1280", "x1024", "1280x", "axb", "-1x100"} {
		if _, _, err := parseViewport(bad); err == nil {
			t.Errorf("parseViewport(%q) accepted invalid input", bad)
		}
	}
}

func TestBasicAuthHeader(t *testing.T) {
	// RFC 7617 example credentials.
	got := basicAuthHeader("Aladdin", "open sesame")
	want := "Basic QWxhZGRpbjpvcGVuIHNlc2FtZQ=="
	if got != want {
		t.Errorf("basicAuthHeader = %q, want %q", got, want)
	}
}

func TestExtraHeaders(t *testing.T) {
	// Without --custom-header-propagation nothing is applied
	// connection-wide; headers ride the main request via interception.
	p := args.NewPageOptions()
	p.CustomHeaders["X-Test"] = "1"
	p.Username = "u"
	p.Password = "p"
	if h := extraHeaders(p); len(h) != 0 {
		t.Errorf("expected no connection-wide headers without propagation, got %v", h)
	}
	p.CustomHeaderPropagation = true
	if h := extraHeaders(p); h["X-Test"] != "1" {
		t.Errorf("propagated custom header missing: %v", h)
	}
	// Credentials answer auth challenges; never a preemptive global header.
	if h := extraHeaders(p); h["Authorization"] != nil {
		t.Errorf("Authorization must not be connection-wide: %v", h)
	}
}

func TestBuildInterception(t *testing.T) {
	p := args.NewPageOptions()
	p.Input = "/tmp/in.html"
	req := PrintRequest{Page: p}

	ic := buildInterception(req, "file:///tmp/in.html")
	if ic == nil || !ic.blockLocal {
		t.Fatal("local file access must be blocked by default")
	}
	if !ic.allowedPath("/tmp/in.html") {
		t.Error("the input file itself must be allowed")
	}
	if ic.allowedPath("/tmp/other.png") {
		t.Error("sibling files must not be allowed")
	}

	p.AllowedPaths = []string{"/tmp"}
	ic = buildInterception(PrintRequest{Page: p}, "file:///tmp/in.html")
	if !ic.allowedPath("/tmp/other.png") {
		t.Error("--allow of a directory must allow files under it")
	}

	p = args.NewPageOptions()
	p.EnableLocalFileAccess = true
	if ic := buildInterception(PrintRequest{Page: p}, "file:///tmp/in.html"); ic != nil {
		t.Errorf("no interception needed with local file access enabled, got %+v", ic)
	}

	p = args.NewPageOptions()
	p.EnableLocalFileAccess = true
	p.CustomHeaders["X-Token"] = "t"
	ic = buildInterception(PrintRequest{Page: p}, "https://example.com/")
	if ic == nil || ic.docHeaders["X-Token"] != "t" {
		t.Fatalf("non-propagated custom headers must intercept document requests: %+v", ic)
	}
	p.CustomHeaderPropagation = true
	if ic := buildInterception(PrintRequest{Page: p}, "https://example.com/"); ic != nil {
		t.Errorf("propagated headers need no interception, got %+v", ic)
	}

	p = args.NewPageOptions()
	p.EnableLocalFileAccess = true
	p.Username = "u"
	ic = buildInterception(PrintRequest{Page: p}, "https://example.com/")
	if ic == nil || !ic.handleAuth {
		t.Fatal("credentials must enable auth handling")
	}
	if pats := ic.patterns(); len(pats) != 1 || pats[0].URLPattern != "*" {
		t.Errorf("auth handling must pause all requests: %+v", pats)
	}
}

func TestCookieActions(t *testing.T) {
	cookies := map[string]string{"a": "1", "b": "2"}
	if got := cookieActions(cookies, "https://example.com/x"); len(got) != 2 {
		t.Errorf("expected 2 cookie actions for https target, got %d", len(got))
	}
	if got := cookieActions(cookies, "file:///tmp/x.html"); got != nil {
		t.Errorf("expected no cookie actions for file target, got %d", len(got))
	}
	if got := cookieActions(nil, "https://example.com"); got != nil {
		t.Error("expected no actions for empty cookie map")
	}
}

func TestBundledShellPaths(t *testing.T) {
	got := bundledShellPaths(filepath.FromSlash("/opt/bilihtmltopdf/bin"))
	want := []string{
		filepath.FromSlash("/opt/bilihtmltopdf/lib/chrome-headless-shell/chrome-headless-shell"),
		filepath.FromSlash("/opt/bilihtmltopdf/bin/lib/chrome-headless-shell/chrome-headless-shell"),
	}
	if len(got) != len(want) {
		t.Fatalf("bundledShellPaths returned %d paths, want %d", len(got), len(want))
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) { // prefix: windows appends .exe
			t.Errorf("bundledShellPaths[%d] = %q, want prefix %q", i, got[i], want[i])
		}
	}
}

func TestStyleInjectionScript(t *testing.T) {
	css := `body { color: "red"; }` + "\nnewline"
	script := styleInjectionScript(css)
	if !strings.Contains(script, `\"red\"`) || !strings.Contains(script, `\n`) {
		t.Errorf("css not JSON-escaped in script:\n%s", script)
	}
	if !strings.Contains(script, "createElement('style')") {
		t.Errorf("script does not create a style element:\n%s", script)
	}
}

// TestPrintPDFIntegration renders the proven fixture through an installed
// Chrome and checks for a plausible PDF. Skipped under -short.
func TestPrintPDFIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("integration test requires Chrome; skipped in -short mode")
	}
	chromePath, err := FindChrome()
	if err != nil {
		t.Fatalf("FindChrome: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	page := args.NewPageOptions()
	page.Input = filepath.Join("..", "..", "testdata", "modern-css.html")
	pdf, err := PrintPDF(ctx, PrintRequest{
		ChromePath:      chromePath,
		Page:            page,
		PaperWidth:      8.27,
		PaperHeight:     11.69,
		MarginTop:       0.4,
		MarginBottom:    0.4,
		MarginLeft:      0.4,
		MarginRight:     0.4,
		Scale:           1.0,
		HeaderTemplate:  `<div style="font-size:9px;width:100%;text-align:center;">Page <span class="pageNumber"></span> of <span class="totalPages"></span></div>`,
		PrintBackground: true,
		GenerateOutline: true,
	})
	if err != nil {
		t.Fatalf("PrintPDF: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Fatalf("output does not start with %%PDF- (got %q)", pdf[:min(8, len(pdf))])
	}
	if len(pdf) < 1024 {
		t.Fatalf("suspiciously small PDF: %d bytes", len(pdf))
	}
}
