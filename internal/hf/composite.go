package hf

import (
	"fmt"
	stdhtml "html"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// PageVars are the per-page values wkhtmltopdf substitutes into a header
// or footer: the current page, the document total, and the object's first
// page. They resolve to literal numbers because each page is composited
// individually, exactly as wkhtmltopdf re-rendered the header/footer for
// every page.
type PageVars struct {
	Page     int
	Topage   int
	Frompage int
}

// Geometry describes the paper and margins the composite header/footer
// page is laid out against (all millimeters), so generated bars align
// horizontally with the content body and sit in the margin band.
type Geometry struct {
	PaperWmm, PaperHmm   float64
	MarginLmm, MarginRmm float64
	MarginTmm, MarginBmm float64
}

// litSubster substitutes wkhtmltopdf [variables] with literal values for
// one specific page, accumulating warnings for unsupported variables. When
// escape is set, substituted values are HTML-escaped — used for generated
// bars where the values are plain text; the --header-html path leaves them
// raw because the surrounding document is already HTML.
type litSubster struct {
	vars     PageVars
	title    string
	repl     map[string]string
	escape   bool
	warnings []string
}

// esc HTML-escapes v when the substituter is in escaping mode.
func (s *litSubster) esc(v string) string {
	if s.escape {
		return stdhtml.EscapeString(v)
	}
	return v
}

func (s *litSubster) warn(msg string) {
	for _, w := range s.warnings {
		if w == msg {
			return
		}
	}
	s.warnings = append(s.warnings, msg)
}

// substitute replaces every wkhtmltopdf variable in text with its literal
// value for this page. text may be plain footer text or a full HTML file.
func (s *litSubster) substitute(text string) string {
	now := time.Now()
	title := s.esc(s.title)
	values := map[string]string{
		"page":     strconv.Itoa(s.vars.Page),
		"topage":   strconv.Itoa(s.vars.Topage),
		"frompage": strconv.Itoa(s.vars.Frompage),
		"date":     now.Format("2006-01-02"),
		"isodate":  now.Format("2006-01-02"),
		"time":     now.Format("15:04:05"),
		"title":    title,
		"doctitle": title,
	}
	out := text
	// --replace values first; they are user text, never markup.
	for name, val := range s.repl {
		out = varRe(name).ReplaceAllLiteralString(out, s.esc(val))
	}
	for name, val := range values {
		out = varRe(name).ReplaceAllLiteralString(out, val)
	}
	for _, name := range unsupportedVars {
		re := varRe(name)
		if re.MatchString(out) {
			s.warn(fmt.Sprintf("header/footer variable [%s] is not supported; substituted with empty text", name))
			out = re.ReplaceAllLiteralString(out, "")
		}
	}
	// [webpage] has no meaningful value in a composited footer; blank it.
	out = varRe("webpage").ReplaceAllLiteralString(out, "")
	return out
}

// SubstituteHTML applies per-page variable substitution to a raw
// --header-html/--footer-html document, returning the ready-to-render HTML
// and any warnings. The document is rendered as a full page by Chromium,
// so its own stylesheets (including base-href ones) load normally — no
// translation, inlining, or template shimming.
func SubstituteHTML(htmlText string, vars PageVars, title string, repl map[string]string) (string, []string) {
	s := &litSubster{vars: vars, title: title, repl: repl}
	return s.substitute(htmlText), s.warnings
}

// hasPageVars reports whether text references any per-page variable, so
// callers can render a header/footer once and reuse it across pages when
// it does not.
func hasPageVars(text string) bool {
	return pageVarLitRe.MatchString(text)
}

// HasPageVars reports whether a header/footer text or HTML references a
// per-page variable ([page]/[topage]/[frompage]).
func HasPageVars(text string) bool { return hasPageVars(text) }

var pageVarLitRe = regexp.MustCompile(`(?i)\[(page|topage|frompage)\]`)

// BuildPage renders a full-page HTML document for a generated
// (--header-left/center/right) header or footer, positioned in the top or
// bottom margin band and aligned horizontally with the content body. It is
// rendered at full paper size with no margins and composited over each
// content page, replacing Chromium's cramped native template mechanism.
func BuildPage(opts HFOptions, vars PageVars, geo Geometry) (string, []string) {
	h := opts.HF
	s := &litSubster{vars: vars, title: opts.Title, repl: opts.Replacements, escape: true}
	left := s.substitute(stdhtml.EscapeString(h.Left))
	center := s.substitute(stdhtml.EscapeString(h.Center))
	right := s.substitute(stdhtml.EscapeString(h.Right))

	font := h.FontName
	if font == "" {
		font = "Arial"
	}
	font = strings.ReplaceAll(font, "'", "")
	size := h.FontSize
	if size <= 0 {
		size = 12
	}

	// The bar occupies the margin band and pads to the content edges.
	var vpos, line string
	if opts.IsHeader {
		vpos = fmt.Sprintf("top:%.3fmm;", h.Spacing)
		if h.Line {
			line = "border-bottom:1px solid #000;padding-bottom:2px;"
		}
	} else {
		vpos = fmt.Sprintf("bottom:%.3fmm;", h.Spacing)
		if h.Line {
			line = "border-top:1px solid #000;padding-top:2px;"
		}
	}

	page := fmt.Sprintf(`<!doctype html><html><head><meta charset="utf-8">`+
		`<style>html,body{margin:0;padding:0;}`+
		`.__hf_bar{position:absolute;left:%.3fmm;right:%.3fmm;%s`+
		`font-family:'%s',sans-serif;font-size:%dpt;display:flex;`+
		`justify-content:space-between;align-items:baseline;%s}`+
		`.__hf_bar>span{flex:1;}.__hf_c{text-align:center;}.__hf_r{text-align:right;}`+
		`</style></head><body>`+
		`<div class="__hf_bar"><span>%s</span><span class="__hf_c">%s</span>`+
		`<span class="__hf_r">%s</span></div></body></html>`,
		geo.MarginLmm, geo.MarginRmm, vpos, font, size, line, left, center, right)

	return page, s.warnings
}
