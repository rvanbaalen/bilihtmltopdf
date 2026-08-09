// Package hf builds full-page headers and footers for compositing over
// rendered content, matching wkhtmltopdf: a header/footer is a full page
// laid over each sheet rather than Chromium's cramped native template.
// BuildPage generates a positioned bar from --header-left/center/right
// settings; SubstituteHTML fills [page]-family variables in a
// --header-html/--footer-html document for one page.
package hf

import (
	"regexp"

	"github.com/rvanbaalen/bilihtmltopdf/internal/args"
)

// HFOptions describes one generated header or footer to render.
type HFOptions struct {
	// HF holds the wkhtmltopdf header/footer settings to render.
	HF args.HeaderFooter
	// IsHeader distinguishes header (line below) from footer (line above).
	IsHeader bool
	// Title substitutes [title]/[doctitle] in the texts.
	Title string
	// PageOffset is added to page numbers to honor --page-offset.
	PageOffset int
	// Replacements substitute [name] with value per --replace.
	Replacements map[string]string
}

// unsupportedVars are wkhtmltopdf variables with no Chromium counterpart;
// they substitute to empty text and produce a warning.
var unsupportedVars = []string{"section", "subsection", "sitepage", "sitepages"}

// varRe returns a case-insensitive matcher for the literal [name] token;
// the fork's hfreplace substitutes with Qt::CaseInsensitive.
func varRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + regexp.QuoteMeta("["+name+"]"))
}
