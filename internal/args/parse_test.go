package args

import (
	"errors"
	"strings"
	"testing"
)

// mustParse parses argv and fails the test on error.
func mustParse(t *testing.T, argv ...string) *Command {
	t.Helper()
	cmd, err := Parse(argv)
	if err != nil {
		t.Fatalf("Parse(%v) error: %v", argv, err)
	}
	return cmd
}

// page returns object i asserted to be a *PageObject.
func page(t *testing.T, cmd *Command, i int) *PageOptions {
	t.Helper()
	o, ok := cmd.Objects[i].(*PageObject)
	if !ok {
		t.Fatalf("object %d: got %T, want *PageObject", i, cmd.Objects[i])
	}
	return &o.Page
}

func TestParseSimple(t *testing.T) {
	cmd := mustParse(t, "in.html", "out.pdf")
	if got := len(cmd.Objects); got != 1 {
		t.Fatalf("objects = %d, want 1", got)
	}
	p := page(t, cmd, 0)
	if p.Input != "in.html" {
		t.Errorf("Input = %q", p.Input)
	}
	if cmd.Global.Output != "out.pdf" {
		t.Errorf("Output = %q", cmd.Global.Output)
	}
	if len(cmd.Warnings) != 0 {
		t.Errorf("Warnings = %v", cmd.Warnings)
	}
	// Spot-check defaults.
	g := cmd.Global
	if g.PageSize != "A4" || g.Orientation != Portrait || g.MarginTop != "10mm" ||
		!g.Outline || g.OutlineDepth != 4 || g.Copies != 1 || !g.Collate || g.DPI != 96 {
		t.Errorf("bad global defaults: %+v", g)
	}
	if p.Zoom != 1.0 || !p.Background || !p.LoadImages || !p.EnableJavascript ||
		p.JavascriptDelay != 200 || !p.IncludeInOutline || p.Header.FontName != "Arial" ||
		p.Header.FontSize != 12 {
		t.Errorf("bad page defaults: %+v", p)
	}
}

func TestParseStdinStdout(t *testing.T) {
	cmd := mustParse(t, "-", "-")
	if page(t, cmd, 0).Input != "-" {
		t.Error("stdin input not recognized")
	}
	if cmd.Global.Output != "-" {
		t.Error("stdout output not recognized")
	}
}

func TestParseGlobalOptions(t *testing.T) {
	cmd := mustParse(t,
		"-q", "--orientation", "landscape", "-s", "Letter",
		"-B", "5mm", "-T", "0", "-L", "1cm", "-R", "1cm",
		"--title", "My Doc", "--no-outline", "--outline-depth", "2",
		"--page-offset", "3", "--owner-password", "op", "--user-password", "up",
		"in.html", "out.pdf")
	g := cmd.Global
	if !g.Quiet {
		t.Error("-q not applied")
	}
	if g.Orientation != Landscape {
		t.Errorf("Orientation = %q", g.Orientation)
	}
	if g.PageSize != "Letter" {
		t.Errorf("PageSize = %q", g.PageSize)
	}
	if g.MarginBottom != "5mm" || g.MarginTop != "0" || g.MarginLeft != "1cm" || g.MarginRight != "1cm" {
		t.Errorf("margins = %q %q %q %q", g.MarginTop, g.MarginBottom, g.MarginLeft, g.MarginRight)
	}
	if g.Title != "My Doc" || g.Outline || g.OutlineDepth != 2 || g.PageOffset != 3 {
		t.Errorf("global = %+v", g)
	}
	if g.OwnerPassword != "op" || g.UserPassword != "up" {
		t.Error("passwords not applied")
	}
}

func TestParseEqualsAndShortAttachedValues(t *testing.T) {
	cmd := mustParse(t, "--zoom=1.5", "-sA4", "--javascript-delay=500", "in.html", "out.pdf")
	p := page(t, cmd, 0)
	if p.Zoom != 1.5 {
		t.Errorf("Zoom = %v", p.Zoom)
	}
	if cmd.Global.PageSize != "A4" {
		t.Errorf("PageSize = %q", cmd.Global.PageSize)
	}
	if p.JavascriptDelay != 500 {
		t.Errorf("JavascriptDelay = %d", p.JavascriptDelay)
	}
}

func TestParseShortCluster(t *testing.T) {
	cmd := mustParse(t, "-qg", "in.html", "out.pdf")
	if !cmd.Global.Quiet || !cmd.Global.Grayscale {
		t.Errorf("cluster -qg: %+v", cmd.Global)
	}
	if len(cmd.Warnings) != 1 { // grayscale warns
		t.Errorf("Warnings = %v", cmd.Warnings)
	}
}

func TestParseGlobalPageDefaultsApplyToAllObjects(t *testing.T) {
	cmd := mustParse(t, "--javascript-delay", "500", "--print-media-type",
		"a.html", "b.html", "out.pdf")
	for i := 0; i < 2; i++ {
		p := page(t, cmd, i)
		if p.JavascriptDelay != 500 || !p.PrintMediaType {
			t.Errorf("object %d did not inherit global page defaults: %+v", i, p)
		}
	}
}

func TestParsePerObjectOptions(t *testing.T) {
	cmd := mustParse(t, "a.html", "--javascript-delay", "500", "b.html", "out.pdf")
	if p := page(t, cmd, 0); p.JavascriptDelay != 500 {
		t.Errorf("a.html delay = %d, want 500", p.JavascriptDelay)
	}
	if p := page(t, cmd, 1); p.JavascriptDelay != 200 {
		t.Errorf("b.html delay = %d, want default 200", p.JavascriptDelay)
	}
}

func TestParseMultiObjectCoverAndTOC(t *testing.T) {
	cmd := mustParse(t,
		"cover", "cover.html",
		"toc", "--toc-header-text", "Contents", "--disable-dotted-lines",
		"a.html", "--zoom", "2",
		"page", "b.html",
		"out.pdf")
	if got := len(cmd.Objects); got != 4 {
		t.Fatalf("objects = %d, want 4", got)
	}

	cov, ok := cmd.Objects[0].(*CoverObject)
	if !ok || cov.Kind() != "cover" {
		t.Fatalf("object 0: %T", cmd.Objects[0])
	}
	if cov.Page.Input != "cover.html" {
		t.Errorf("cover input = %q", cov.Page.Input)
	}
	if cov.Page.IncludeInOutline {
		t.Error("cover should default to excluded from outline")
	}

	toc, ok := cmd.Objects[1].(*TOCObject)
	if !ok || toc.Kind() != "toc" {
		t.Fatalf("object 1: %T", cmd.Objects[1])
	}
	if toc.TOC.HeaderText != "Contents" {
		t.Errorf("toc header = %q", toc.TOC.HeaderText)
	}
	if toc.TOC.UseDottedLines {
		t.Error("dotted lines should be disabled")
	}
	if !toc.TOC.ForwardLinks || toc.TOC.Indentation != "1em" || toc.TOC.FontScale != 0.8 {
		t.Errorf("toc defaults clobbered: %+v", toc.TOC)
	}

	if p := page(t, cmd, 2); p.Input != "a.html" || p.Zoom != 2 {
		t.Errorf("object 2: %+v", p)
	}
	if p := page(t, cmd, 3); p.Input != "b.html" || p.Zoom != 1 {
		t.Errorf("object 3: %+v", p)
	}
	if cmd.Global.Output != "out.pdf" {
		t.Errorf("Output = %q", cmd.Global.Output)
	}
}

func TestParseHeaderFooterFlags(t *testing.T) {
	cmd := mustParse(t,
		"--header-left", "[title]", "--header-center", "mid", "--header-right", "[page]/[topage]",
		"--header-line", "--header-font-name", "Times", "--header-font-size", "9",
		"--header-spacing", "2.5",
		"--footer-center", "[page]", "--footer-html", "foot.html", "--no-footer-line",
		"in.html", "out.pdf")
	p := page(t, cmd, 0)
	h := p.Header
	if h.Left != "[title]" || h.Center != "mid" || h.Right != "[page]/[topage]" {
		t.Errorf("header texts: %+v", h)
	}
	if !h.Line || h.FontName != "Times" || h.FontSize != 9 || h.Spacing != 2.5 {
		t.Errorf("header opts: %+v", h)
	}
	if p.Footer.Center != "[page]" || p.Footer.HTMLPath != "foot.html" || p.Footer.Line {
		t.Errorf("footer opts: %+v", p.Footer)
	}
}

func TestParseDefaultHeader(t *testing.T) {
	cmd := mustParse(t, "--default-header", "in.html", "out.pdf")
	h := page(t, cmd, 0).Header
	if h.Left != "[webpage]" || h.Right != "[page]/[toPage]" || !h.Line {
		t.Errorf("default header: %+v", h)
	}
	if cmd.Global.MarginTop != "2cm" {
		t.Errorf("MarginTop = %q, want 2cm", cmd.Global.MarginTop)
	}
}

func TestParseRepeatableMapFlags(t *testing.T) {
	cmd := mustParse(t,
		"--cookie", "sid", "abc123", "--cookie", "lang", "nl",
		"--custom-header", "X-Token", "t1",
		"--replace", "client", "ACME",
		"in.html", "out.pdf")
	p := page(t, cmd, 0)
	if p.Cookies["sid"] != "abc123" || p.Cookies["lang"] != "nl" {
		t.Errorf("Cookies = %v", p.Cookies)
	}
	if p.CustomHeaders["X-Token"] != "t1" {
		t.Errorf("CustomHeaders = %v", p.CustomHeaders)
	}
	if p.Replacements["client"] != "ACME" {
		t.Errorf("Replacements = %v", p.Replacements)
	}
}

func TestParseObjectsDoNotShareMaps(t *testing.T) {
	cmd := mustParse(t, "--cookie", "sid", "s", "a.html", "--cookie", "extra", "e", "b.html", "out.pdf")
	a, b := page(t, cmd, 0), page(t, cmd, 1)
	if a.Cookies["sid"] != "s" || b.Cookies["sid"] != "s" {
		t.Fatal("global cookie default not inherited")
	}
	if _, leaked := b.Cookies["extra"]; leaked {
		t.Error("per-object cookie leaked into sibling object")
	}
	if _, leaked := a.Cookies["extra"]; !leaked {
		t.Error("per-object cookie not applied to its own object")
	}
}

func TestParseRunScriptsRepeatable(t *testing.T) {
	cmd := mustParse(t, "--run-script", "a()", "--run-script", "b()", "in.html", "out.pdf")
	p := page(t, cmd, 0)
	if len(p.RunScripts) != 2 || p.RunScripts[0] != "a()" || p.RunScripts[1] != "b()" {
		t.Errorf("RunScripts = %v", p.RunScripts)
	}
}

func TestParseUnknownFlagErrors(t *testing.T) {
	// Unknown switches are fatal like wkhtmltopdf: warn-and-continue
	// would lex the flag's value as an extra input page.
	for _, argv := range [][]string{
		{"--frobnicate", "in.html", "out.pdf"},
		{"-Z", "in.html", "out.pdf"},
	} {
		_, err := Parse(argv)
		if err == nil || !strings.Contains(err.Error(), "unknown switch") {
			t.Errorf("Parse(%v) = %v, want unknown switch error", argv, err)
		}
	}
}

func TestParseEncodingStoredWithWarning(t *testing.T) {
	cmd := mustParse(t, "--encoding", "ISO-8859-1", "in.html", "out.pdf")
	if p := page(t, cmd, 0); p.DefaultEncoding != "ISO-8859-1" {
		t.Errorf("DefaultEncoding = %q", p.DefaultEncoding)
	}
	if len(cmd.Warnings) != 1 || !strings.Contains(cmd.Warnings[0], "--encoding") {
		t.Errorf("Warnings = %v", cmd.Warnings)
	}
}

func TestParseAllowSupportedWithoutWarning(t *testing.T) {
	cmd := mustParse(t, "--allow", "/srv/assets", "--allow", "/srv/img", "in.html", "out.pdf")
	p := page(t, cmd, 0)
	if len(p.AllowedPaths) != 2 || p.AllowedPaths[0] != "/srv/assets" {
		t.Errorf("AllowedPaths = %v", p.AllowedPaths)
	}
	if len(cmd.Warnings) != 0 {
		t.Errorf("Warnings = %v", cmd.Warnings)
	}
}

func TestParseInformationalFlagsExit(t *testing.T) {
	tests := []struct {
		flag string
		want string // substring of the printed text
	}{
		{"--license", "License"},
		{"--readme", "Synopsis"},
		{"--manpage", "Synopsis"},
		{"--htmldoc", "Synopsis"},
		{"--dump-default-toc-xsl", "xsl:stylesheet"},
	}
	for _, tt := range tests {
		_, err := Parse([]string{tt.flag})
		var et *ExitText
		if !errors.As(err, &et) {
			t.Errorf("Parse(%s): want *ExitText, got %v", tt.flag, err)
			continue
		}
		if !strings.Contains(et.Text, tt.want) {
			t.Errorf("%s output lacks %q:\n%.200s", tt.flag, tt.want, et.Text)
		}
	}
}

func TestParseWarnOnlyFlagsStoredAndWarned(t *testing.T) {
	cmd := mustParse(t, "--grayscale", "--dpi", "300", "--lowquality",
		"--cookie-jar", "jar.txt", "--copies", "3", "--no-collate",
		"--read-args-from-stdin", "--disable-smart-shrinking",
		"in.html", "out.pdf")
	g := cmd.Global
	if !g.Grayscale || g.DPI != 300 || !g.LowQuality || g.CookieJar != "jar.txt" ||
		g.Copies != 3 || g.Collate {
		t.Errorf("warn-only values not stored: %+v", g)
	}
	if len(cmd.Warnings) != 8 {
		t.Errorf("expected 8 warnings, got %d: %v", len(cmd.Warnings), cmd.Warnings)
	}
}

func TestParseStrictTurnsWarningsFatal(t *testing.T) {
	if _, err := Parse([]string{"--strict", "--grayscale", "in.html", "out.pdf"}); err == nil {
		t.Fatal("expected error under --strict")
	} else if !strings.Contains(err.Error(), "grayscale") {
		t.Errorf("error should name the offending flag: %v", err)
	}
	// Same invocation without --strict succeeds.
	mustParse(t, "--grayscale", "in.html", "out.pdf")
}

func TestParseTOCOptionOutsidePageObjectWarns(t *testing.T) {
	cmd := mustParse(t, "a.html", "--toc-header-text", "X", "out.pdf")
	if len(cmd.Warnings) != 1 || !strings.Contains(cmd.Warnings[0], "toc-header-text") {
		t.Errorf("Warnings = %v", cmd.Warnings)
	}
}

func TestParseGlobalTOCDefaults(t *testing.T) {
	// TOC options before any object become defaults for later toc objects.
	cmd := mustParse(t, "--toc-header-text", "Inhoud", "toc", "a.html", "out.pdf")
	toc := cmd.Objects[0].(*TOCObject)
	if toc.TOC.HeaderText != "Inhoud" {
		t.Errorf("HeaderText = %q", toc.TOC.HeaderText)
	}
}

func TestParseHelpVersion(t *testing.T) {
	for _, argv := range [][]string{{"--version"}, {"-V"}} {
		_, err := Parse(argv)
		var et *ExitText
		if !errors.As(err, &et) {
			t.Fatalf("Parse(%v): want *ExitText, got %v", argv, err)
		}
		if !strings.Contains(et.Text, "bilihtmltopdf dev") {
			t.Errorf("version text = %q", et.Text)
		}
	}
	for _, argv := range [][]string{{"--help"}, {"-h"}, {"--extended-help"}, {"-H"}} {
		_, err := Parse(argv)
		var et *ExitText
		if !errors.As(err, &et) {
			t.Fatalf("Parse(%v): want *ExitText, got %v", argv, err)
		}
		if !strings.Contains(et.Text, "Synopsis") {
			t.Errorf("help text for %v lacks Synopsis", argv)
		}
	}
	// Extended help includes the compatibility switch listing.
	_, err := Parse([]string{"--extended-help"})
	var et *ExitText
	errors.As(err, &et)
	if !strings.Contains(et.Text, "Compatibility switches") {
		t.Error("extended help lacks compatibility section")
	}
	// Help wins even with other args present.
	if _, err := Parse([]string{"in.html", "--help", "out.pdf"}); !errors.As(err, new(*ExitText)) {
		t.Error("--help mid-argv should still exit")
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string // substring of the error
	}{
		{"empty", nil, "output file"},
		{"output only", []string{"out.pdf"}, "output file"},
		{"only flags", []string{"--quiet"}, "output file"},
		{"cover missing input", []string{"cover", "out.pdf"}, "cover object is missing its input"},
		{"page missing input", []string{"page", "out.pdf"}, "page object is missing its input"},
		{"bad orientation", []string{"--orientation", "diagonal", "in.html", "out.pdf"}, "orientation"},
		{"bad int", []string{"--javascript-delay", "soon", "in.html", "out.pdf"}, "integer"},
		{"bad float", []string{"--zoom", "big", "in.html", "out.pdf"}, "number"},
		{"missing value", []string{"in.html", "out.pdf", "--zoom"}, "requires"},
		{"missing map value", []string{"--cookie", "sid"}, "requires"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.argv)
			if err == nil {
				t.Fatalf("Parse(%v): expected error", tt.argv)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error %q does not contain %q", err, tt.want)
			}
		})
	}
}

func TestParseRealWorldInvocation(t *testing.T) {
	// Kitchen-sink invocation modeled on production wkhtmltopdf usage.
	cmd := mustParse(t,
		"-q", "-s", "A4", "-O", "Landscape",
		"--margin-top", "20mm", "--margin-bottom", "15mm",
		"--print-media-type", "--enable-local-file-access",
		"--header-html", "header.html", "--footer-center", "[page] of [topage]",
		"--cookie", "session", "deadbeef", "--custom-header", "Authorization", "Bearer x",
		"cover", "cover.html",
		"toc", "--xsl-style-sheet", "toc.xsl",
		"chapter1.html", "--footer-center", "",
		"chapter2.html",
		"report.pdf")
	if len(cmd.Objects) != 4 {
		t.Fatalf("objects = %d, want 4", len(cmd.Objects))
	}
	g := cmd.Global
	if !g.Quiet || g.Orientation != Landscape || g.MarginTop != "20mm" || g.MarginBottom != "15mm" {
		t.Errorf("global: %+v", g)
	}
	if g.Output != "report.pdf" {
		t.Errorf("Output = %q", g.Output)
	}
	kinds := []string{"cover", "toc", "page", "page"}
	for i, k := range kinds {
		if cmd.Objects[i].Kind() != k {
			t.Errorf("object %d kind = %q, want %q", i, cmd.Objects[i].Kind(), k)
		}
	}
	// Global page defaults inherited everywhere.
	for i := range cmd.Objects {
		p := cmd.Objects[i].Options()
		if !p.PrintMediaType || !p.EnableLocalFileAccess || p.Header.HTMLPath != "header.html" {
			t.Errorf("object %d missing inherited defaults: %+v", i, p)
		}
		if p.Cookies["session"] != "deadbeef" || p.CustomHeaders["Authorization"] != "Bearer x" {
			t.Errorf("object %d missing inherited cookie/header", i)
		}
	}
	if toc := cmd.Objects[1].(*TOCObject); toc.TOC.XSLStyleSheet != "toc.xsl" {
		t.Errorf("toc xsl = %q", toc.TOC.XSLStyleSheet)
	}
	// chapter1 overrides its footer; chapter2 keeps the inherited one.
	if p := cmd.Objects[2].Options(); p.Footer.Center != "" {
		t.Errorf("chapter1 footer = %q, want cleared", p.Footer.Center)
	}
	if p := cmd.Objects[3].Options(); p.Footer.Center != "[page] of [topage]" {
		t.Errorf("chapter2 footer = %q", p.Footer.Center)
	}
	if len(cmd.Warnings) != 0 {
		t.Errorf("Warnings = %v", cmd.Warnings)
	}
}

func TestParseDoubleDashEndsOptions(t *testing.T) {
	cmd := mustParse(t, "--print-media-type", "--", "--weird-name.html", "out.pdf")
	if p := page(t, cmd, 0); p.Input != "--weird-name.html" || !p.PrintMediaType {
		t.Errorf("page = %+v", p)
	}
}

func TestParseSSLFlagsStoredWithWarning(t *testing.T) {
	cmd := mustParse(t, "--ssl-key-path", "k.pem", "--ssl-crt-path", "c.pem",
		"--ssl-key-password", "pw", "in.html", "out.pdf")
	p := page(t, cmd, 0)
	if p.SSLKeyPath != "k.pem" || p.SSLCrtPath != "c.pem" || p.SSLKeyPassword != "pw" {
		t.Errorf("ssl fields: %+v", p)
	}
	if len(cmd.Warnings) != 3 {
		t.Errorf("Warnings = %v", cmd.Warnings)
	}
}

func TestParseTwoArgFlagValuesMayLookLikeFlags(t *testing.T) {
	// Second value of a two-arg flag is consumed verbatim even if it
	// starts with a dash.
	cmd := mustParse(t, "--replace", "delta", "-5", "in.html", "out.pdf")
	if p := page(t, cmd, 0); p.Replacements["delta"] != "-5" {
		t.Errorf("Replacements = %v", p.Replacements)
	}
}
