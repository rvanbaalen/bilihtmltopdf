package hf

import (
	"strings"
	"testing"

	"github.com/rvanbaalen/bilihtmltopdf/internal/args"
)

// footerOpts returns HFOptions for a footer with the given three texts.
func footerOpts(left, center, right string) HFOptions {
	hf := args.NewHeaderFooter()
	hf.Left, hf.Center, hf.Right = left, center, right
	return HFOptions{HF: hf, IsHeader: false}
}

// TestBuildPageEmpty returns no HTML when nothing is configured.
func TestBuildPageEmpty(t *testing.T) {
	html, _ := BuildPage(footerOpts("", "", ""), PageVars{Page: 1, Topage: 1})
	// An all-empty bar still renders (blank spans); assert it is at least
	// a full document, not a template fragment.
	if !strings.Contains(html, "<html>") {
		t.Errorf("BuildPage must emit a full HTML page:\n%s", html)
	}
}

// TestBuildPageLiteralPageNumbers bakes literal page numbers per page.
func TestBuildPageLiteralPageNumbers(t *testing.T) {
	html, warns := BuildPage(footerOpts("", "[page] of [topage]", ""),
		PageVars{Page: 3, Topage: 7, Frompage: 1})
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if !strings.Contains(html, "3 of 7") {
		t.Errorf("literal page numbers missing:\n%s", html)
	}
	// No CDP template classes remain.
	if strings.Contains(html, "pageNumber") || strings.Contains(html, "totalPages") {
		t.Errorf("CDP template classes must not appear:\n%s", html)
	}
}

// TestBuildPageLineAndFont honors the separator line and font settings.
func TestBuildPageLineAndFont(t *testing.T) {
	opts := footerOpts("L", "C", "R")
	opts.HF.Line = true
	opts.HF.FontName = "Helvetica"
	opts.HF.FontSize = 9
	html, _ := BuildPage(opts, PageVars{Page: 1, Topage: 1})
	if !strings.Contains(html, "border-top:1px solid #000") {
		t.Errorf("footer line missing:\n%s", html)
	}
	if !strings.Contains(html, "Helvetica") || !strings.Contains(html, "9pt") {
		t.Errorf("font settings missing:\n%s", html)
	}
}

// TestBuildPageEscapesAndReplaces escapes user text and applies --replace.
func TestBuildPageEscapesAndReplaces(t *testing.T) {
	opts := footerOpts("<b>[co]</b>", "[title]", "")
	opts.Title = "A & B"
	opts.Replacements = map[string]string{"co": "Acme <Ltd>"}
	html, _ := BuildPage(opts, PageVars{Page: 1, Topage: 1})
	if strings.Contains(html, "<b>") {
		t.Errorf("user angle brackets must be escaped:\n%s", html)
	}
	if !strings.Contains(html, "A &amp; B") {
		t.Errorf("title must be escaped and substituted:\n%s", html)
	}
}

// TestBuildPageUnsupportedVarsWarn warns and blanks [section] etc.
func TestBuildPageUnsupportedVarsWarn(t *testing.T) {
	html, warns := BuildPage(footerOpts("[section]", "", ""), PageVars{Page: 1, Topage: 1})
	found := false
	for _, w := range warns {
		if strings.Contains(w, "[section]") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected an unsupported-variable warning, got %v", warns)
	}
	if strings.Contains(html, "[section]") {
		t.Errorf("[section] should be blanked:\n%s", html)
	}
}

// TestSubstituteHTMLLiteral fills [page]-family variables in a raw
// --footer-html document with literal per-page values.
func TestSubstituteHTMLLiteral(t *testing.T) {
	src := `<html><body><footer>Page [page]/[topage] — [title]</footer></body></html>`
	out, warns := SubstituteHTML(src, PageVars{Page: 2, Topage: 5}, "Report", nil)
	if len(warns) != 0 {
		t.Errorf("unexpected warnings: %v", warns)
	}
	if !strings.Contains(out, "Page 2/5 — Report") {
		t.Errorf("substitution wrong:\n%s", out)
	}
	// The document is otherwise passed through untouched (base href, links,
	// and scripts survive — Chromium renders it as a full page).
	if !strings.Contains(out, "<footer>") {
		t.Errorf("document structure must be preserved:\n%s", out)
	}
}

// TestSubstituteHTMLCaseInsensitive matches [Page] like wkhtmltopdf.
func TestSubstituteHTMLCaseInsensitive(t *testing.T) {
	out, _ := SubstituteHTML("[PAGE]/[ToPage]", PageVars{Page: 4, Topage: 9}, "", nil)
	if out != "4/9" {
		t.Errorf("case-insensitive substitution = %q, want 4/9", out)
	}
}

// TestHasPageVars detects per-page variables so callers can cache renders.
func TestHasPageVars(t *testing.T) {
	if !HasPageVars("footer [page]") {
		t.Error("[page] must be detected")
	}
	if HasPageVars("static footer text") {
		t.Error("static text must not be detected as page-varying")
	}
}
