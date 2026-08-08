// Package hf builds CDP header/footer templates. It generates 3-column
// inline-styled templates from --header-left/center/right settings and
// rewrites --header-html files into CDP's isolated template context.
package hf

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	stdhtml "html"
	"mime"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/rvanbaalen/bilihtmltopdf/internal/args"
)

// HFOptions describes one header or footer template to generate.
type HFOptions struct {
	// HF holds the wkhtmltopdf header/footer settings to render.
	HF args.HeaderFooter
	// IsHeader distinguishes header (line below) from footer (line above).
	IsHeader bool
	// Title substitutes [title]/[doctitle] in the template texts.
	Title string
	// PageOffset is added to CDP's pageNumber to honor --page-offset.
	PageOffset int
	// Replacements substitute [name] with value per --replace.
	Replacements map[string]string
}

// wrapperClass marks the div that replaces <body> in translated header/footer
// HTML; CSS "body" selectors are rewritten to target it.
const wrapperClass = "__hf_body"

// cdpClasses maps wkhtmltopdf substitution-class names to the CDP template
// classes Chromium fills at print time. Names absent here are either baked
// as static text or unsupported.
var cdpClasses = map[string]string{
	"page":     "pageNumber",
	"topage":   "totalPages",
	"webpage":  "url",
	"doctitle": "title",
	// "title" and "date" already match CDP's class names verbatim.
}

// unsupportedVars are wkhtmltopdf variables with no Chromium counterpart;
// they substitute to empty text and produce a warning.
var unsupportedVars = []string{"section", "subsection", "sitepage", "sitepages"}

// subster performs wkhtmltopdf [variable] substitution on escaped text,
// accumulating warnings for variables that cannot be translated.
type subster struct {
	title        string
	replacements map[string]string
	warnings     []string
}

// warn records a translation warning once per distinct message.
func (s *subster) warn(msg string) {
	for _, w := range s.warnings {
		if w == msg {
			return
		}
	}
	s.warnings = append(s.warnings, msg)
}

// apply HTML-escapes plain text and then substitutes wkhtmltopdf variables.
func (s *subster) apply(text string) string {
	if text == "" {
		return ""
	}
	return s.substitute(stdhtml.EscapeString(text))
}

// varRe returns a case-insensitive matcher for the literal [name] token;
// the fork's hfreplace substitutes with Qt::CaseInsensitive.
func varRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + regexp.QuoteMeta("["+name+"]"))
}

// substitute replaces wkhtmltopdf variables in already-escaped or serialized
// HTML with CDP template spans, baked values, or --replace values.
func (s *subster) substitute(out string) string {
	// --replace values are user text, never markup.
	for name, val := range s.replacements {
		v := stdhtml.EscapeString(val)
		out = varRe(name).ReplaceAllLiteralString(out, v)
		out = regexp.MustCompile(`(?i)`+regexp.QuoteMeta(stdhtml.EscapeString("["+name+"]"))).ReplaceAllLiteralString(out, v)
	}
	now := time.Now()
	title := `<span class="title"></span>`
	if s.title != "" {
		title = stdhtml.EscapeString(s.title)
	}
	builtins := map[string]string{
		"page":     `<span class="pageNumber"></span>`,
		"topage":   `<span class="totalPages"></span>`,
		"webpage":  `<span class="url"></span>`,
		"date":     `<span class="date"></span>`,
		"frompage": "1", // whole document is always printed
		"isodate":  now.Format("2006-01-02"),
		"time":     now.Format("15:04:05"),
		"title":    title,
		"doctitle": title,
	}
	for name, repl := range builtins {
		out = varRe(name).ReplaceAllLiteralString(out, repl)
	}
	for _, name := range unsupportedVars {
		re := varRe(name)
		if re.MatchString(out) {
			s.warn(fmt.Sprintf("header/footer variable [%s] is not supported by the Chromium backend; substituted with empty text", name))
			out = re.ReplaceAllLiteralString(out, "")
		}
	}
	return out
}

// BuildTemplate renders a CDP headerTemplate/footerTemplate HTML string
// from generated 3-column settings: inline CSS only, wkhtmltopdf
// substitutions ([page], [topage], [date], [title], ...) mapped to CDP's
// pageNumber/totalPages/date/title classes. Returns the template plus
// warnings for settings that cannot be translated.
func BuildTemplate(opts HFOptions) (string, []string, error) {
	h := opts.HF
	if h.HTMLPath != "" {
		return "", nil, errors.New("hf.BuildTemplate: HTMLPath is set; use TranslateHTMLFile")
	}
	if h.Left == "" && h.Center == "" && h.Right == "" && !h.Line {
		return "", nil, nil
	}

	s := &subster{title: opts.Title, replacements: opts.Replacements}
	if opts.PageOffset != 0 && varRe("page").MatchString(h.Left+h.Center+h.Right) {
		s.warn("--page-offset cannot shift Chromium page numbers; [page] starts at 1")
	}
	left, center, right := s.apply(h.Left), s.apply(h.Center), s.apply(h.Right)

	font := h.FontName
	if font == "" {
		font = "Arial"
	}
	size := h.FontSize
	if size <= 0 {
		size = 12
	}
	line := ""
	if h.Line {
		if opts.IsHeader {
			line = "border-bottom:1px solid #000;"
		} else {
			line = "border-top:1px solid #000;"
		}
	}

	// Padding matches wkhtmltopdf's default 10mm content margins so the
	// columns line up with the page body; the template spans full paper width.
	tmpl := fmt.Sprintf(
		`<div style="font-family:'%s',sans-serif;font-size:%dpt;width:100%%;margin:0;padding:0 10mm;display:flex;justify-content:space-between;align-items:baseline;box-sizing:border-box;%s">`+
			`<span style="flex:1;text-align:left;">%s</span>`+
			`<span style="flex:1;text-align:center;">%s</span>`+
			`<span style="flex:1;text-align:right;">%s</span></div>`,
		strings.ReplaceAll(stdhtml.EscapeString(font), "'", ""), size, line, left, center, right)

	return tmpl, s.warnings, nil
}

// cssURLRe matches CSS url(...) references, capturing the bare reference.
var cssURLRe = regexp.MustCompile(`url\(\s*['"]?([^'")]+)['"]?\s*\)`)

// bodySelRe matches a "body" type selector in CSS so it can be rewritten to
// the wrapper-div class; leading char guard avoids ".body" and "tbody".
var bodySelRe = regexp.MustCompile(`(^|[\s,{}>+~])body\b`)

// translator carries state for one TranslateHTMLFile pass: the base dir for
// resolving relative references, gathered CSS, and accumulated warnings.
type translator struct {
	baseDir string
	css     []string
	sub     *subster
}

// warnf records a formatted translation warning.
func (t *translator) warnf(format string, a ...any) {
	t.sub.warn(fmt.Sprintf(format, a...))
}

// resolve turns a relative or file:// reference into an absolute local path,
// or "" for references that cannot be inlined (remote, data:).
func (t *translator) resolve(ref string) string {
	switch {
	case strings.HasPrefix(ref, "data:"):
		return ""
	case strings.HasPrefix(ref, "http://"), strings.HasPrefix(ref, "https://"):
		t.warnf("remote resource %q in header/footer HTML cannot be inlined; Chromium templates cannot load external resources", ref)
		return ""
	case strings.HasPrefix(ref, "file://"):
		return strings.TrimPrefix(ref, "file://")
	case filepath.IsAbs(ref):
		return ref
	default:
		return filepath.Join(t.baseDir, ref)
	}
}

// dataURI reads a local file and encodes it as a data: URI, guessing the
// MIME type from the extension. Returns "" (with a warning) on failure.
func (t *translator) dataURI(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		t.warnf("cannot inline %q: %v", path, err)
		return ""
	}
	mt := mime.TypeByExtension(filepath.Ext(path))
	if mt == "" {
		mt = "application/octet-stream"
	}
	// Strip charset suffixes; data URIs want the bare type for binaries.
	if i := strings.IndexByte(mt, ';'); i >= 0 && !strings.HasPrefix(mt, "text/") {
		mt = mt[:i]
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

// inlineCSSURLs rewrites url(...) references in CSS to data: URIs so
// images and fonts survive CDP's no-network template context.
func (t *translator) inlineCSSURLs(css string) string {
	return cssURLRe.ReplaceAllStringFunc(css, func(m string) string {
		ref := cssURLRe.FindStringSubmatch(m)[1]
		path := t.resolve(ref)
		if path == "" {
			return m
		}
		if uri := t.dataURI(path); uri != "" {
			return "url(" + uri + ")"
		}
		return m
	})
}

// attr returns the value of the named attribute on n, or "".
func attr(n *xhtml.Node, name string) string {
	for _, a := range n.Attr {
		if a.Key == name {
			return a.Val
		}
	}
	return ""
}

// setAttr sets or adds the named attribute on n.
func setAttr(n *xhtml.Node, name, val string) {
	for i, a := range n.Attr {
		if a.Key == name {
			n.Attr[i].Val = val
			return
		}
	}
	n.Attr = append(n.Attr, xhtml.Attribute{Key: name, Val: val})
}

// setText replaces n's children with a single text node.
func setText(n *xhtml.Node, text string) {
	for n.FirstChild != nil {
		n.RemoveChild(n.FirstChild)
	}
	n.AppendChild(&xhtml.Node{Type: xhtml.TextNode, Data: text})
}

// textContent concatenates the text nodes under n.
func textContent(n *xhtml.Node) string {
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == xhtml.TextNode {
			b.WriteString(c.Data)
		}
	}
	return b.String()
}

// mapClasses translates wkhtmltopdf substitution classes on n to their CDP
// equivalents, bakes static values, and warns on unsupported names.
func (t *translator) mapClasses(n *xhtml.Node) {
	classes := strings.Fields(attr(n, "class"))
	if len(classes) == 0 {
		return
	}
	now := time.Now()
	changed := false
	for _, c := range classes {
		if cdp, ok := cdpClasses[c]; ok {
			classes = append(classes, cdp)
			changed = true
			continue
		}
		switch c {
		case "frompage":
			setText(n, "1")
		case "isodate":
			setText(n, now.Format("2006-01-02"))
		case "time":
			setText(n, now.Format("15:04:05"))
		case "section", "subsection", "sitepage", "sitepages":
			t.warnf("header/footer class %q is not supported by the Chromium backend; element left empty", c)
		}
	}
	if changed {
		setAttr(n, "class", strings.Join(classes, " "))
	}
}

// transform walks n's subtree depth-first: strips scripts, hoists styles
// and stylesheet links into t.css, inlines images, and maps classes.
func (t *translator) transform(n *xhtml.Node) {
	for c := n.FirstChild; c != nil; {
		next := c.NextSibling // c may be removed below
		if c.Type == xhtml.ElementNode {
			switch c.Data {
			case "script":
				t.warnf("header/footer HTML scripts are not executed by Chromium templates; stripped (wkhtmltopdf substitution classes are translated automatically)")
				n.RemoveChild(c)
				c = next
				continue
			case "style":
				t.css = append(t.css, t.inlineCSSURLs(textContent(c)))
				n.RemoveChild(c)
				c = next
				continue
			case "link":
				if strings.Contains(strings.ToLower(attr(c, "rel")), "stylesheet") {
					if path := t.resolve(attr(c, "href")); path != "" {
						if raw, err := os.ReadFile(path); err != nil {
							t.warnf("cannot inline stylesheet %q: %v", path, err)
						} else {
							t.css = append(t.css, t.inlineCSSURLs(string(raw)))
						}
					}
					n.RemoveChild(c)
					c = next
					continue
				}
			case "img":
				if src := attr(c, "src"); src != "" {
					if path := t.resolve(src); path != "" {
						if uri := t.dataURI(path); uri != "" {
							setAttr(c, "src", uri)
						}
					}
				}
			}
			if style := attr(c, "style"); strings.Contains(style, "url(") {
				setAttr(c, "style", t.inlineCSSURLs(style))
			}
			t.mapClasses(c)
		}
		t.transform(c)
		c = next
	}
}

// findElement returns the first element named tag under n, depth-first.
func findElement(n *xhtml.Node, tag string) *xhtml.Node {
	if n.Type == xhtml.ElementNode && n.Data == tag {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if found := findElement(c, tag); found != nil {
			return found
		}
	}
	return nil
}

// TranslateHTMLFile rewrites a --header-html/--footer-html file into a CDP
// template: inlines stylesheets, embeds images as data URIs, strips
// scripts, and maps wkhtmltopdf substitutions. Returns the template HTML
// and warnings for constructs that cannot be translated (e.g. [section]).
func TranslateHTMLFile(path string) (string, []string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", nil, fmt.Errorf("hf.TranslateHTMLFile: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", nil, fmt.Errorf("hf.TranslateHTMLFile: %w", err)
	}
	doc, err := xhtml.Parse(bytes.NewReader(raw))
	if err != nil {
		return "", nil, fmt.Errorf("hf.TranslateHTMLFile: parse %s: %w", path, err)
	}

	t := &translator{baseDir: filepath.Dir(abs), sub: &subster{}}
	t.transform(doc)

	body := findElement(doc, "body")
	if body == nil {
		return "", nil, fmt.Errorf("hf.TranslateHTMLFile: %s has no body", path)
	}

	var buf bytes.Buffer
	if len(t.css) > 0 {
		// "body" selectors must target the wrapper div that replaces <body>.
		css := bodySelRe.ReplaceAllString(strings.Join(t.css, "\n"), "${1}."+wrapperClass)
		buf.WriteString("<style>")
		buf.WriteString(css)
		buf.WriteString("</style>")
	}

	// CDP's template context defaults to a tiny font; 12pt restores the
	// browser default (16px) the file was authored against.
	style := "font-size:12pt;margin:0;width:100%;"
	class := wrapperClass
	for _, a := range body.Attr {
		switch a.Key {
		case "style":
			style += a.Val // body's own style wins via later declarations
		case "class":
			class += " " + a.Val
		}
	}
	fmt.Fprintf(&buf, `<div class="%s" style="%s">`, class, style)
	for c := body.FirstChild; c != nil; c = c.NextSibling {
		if err := xhtml.Render(&buf, c); err != nil {
			return "", nil, fmt.Errorf("hf.TranslateHTMLFile: render: %w", err)
		}
	}
	buf.WriteString("</div>")

	// [variable] substitution runs on the serialized output; escaped text
	// nodes cannot contain markup, so plain string replacement is safe.
	return t.sub.substitute(buf.String()), t.sub.warnings, nil
}
