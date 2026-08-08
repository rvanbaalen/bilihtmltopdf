// Package toc generates the table-of-contents HTML from a rendered
// document's outline, approximating wkhtmltopdf's default TOC style.
package toc

import (
	"fmt"
	"html"
	"strconv"
	"strings"

	"github.com/rvanbaalen/bilihtmltopdf/internal/args"
	"github.com/rvanbaalen/bilihtmltopdf/internal/pdfops"
)

// node is one outline entry with its nested children, rebuilt from the
// flattened depth-first entry list.
type node struct {
	entry pdfops.OutlineEntry
	kids  []*node
}

// GenerateHTML renders the TOC page HTML for entries, mirroring wkhtmltopdf's
// default XSLT output. opts.XSLStyleSheet is unsupported (no XSLT engine):
// built-in HTML is still returned alongside a warning error.
//
// Depth filtering (--toc-depth / --outline-depth) is the caller's job: pass
// only the entries that should appear. When opts.ForwardLinks is set, each
// entry links to "#dest-p<page>"; turning those into cross-document PDF
// link annotations is up to the merge post-processing step.
func GenerateHTML(entries []pdfops.OutlineEntry, opts args.TOCOptions) (string, error) {
	// Guard zero-value opts; contract callers use args.NewTOCOptions.
	if opts.Indentation == "" {
		opts.Indentation = "1em"
	}
	if opts.FontScale <= 0 {
		opts.FontScale = 0.8
	}

	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html>\n<head>\n")
	b.WriteString("<meta charset=\"utf-8\"/>\n")
	fmt.Fprintf(&b, "<title>%s</title>\n", html.EscapeString(opts.HeaderText))
	b.WriteString("<style>\n")
	b.WriteString("h1 { text-align: center; font-size: 20px; font-family: arial; }\n")
	if opts.UseDottedLines {
		b.WriteString("div { border-bottom: 1px dashed rgb(200,200,200); }\n")
	}
	b.WriteString("span { float: right; }\n")
	b.WriteString("li { list-style: none; }\n")
	b.WriteString("ul { font-size: 20px; font-family: arial; padding-left: 0em; }\n")
	fmt.Fprintf(&b, "ul ul { font-size: %s%%; padding-left: %s; }\n",
		formatScale(opts.FontScale), opts.Indentation)
	b.WriteString("a { text-decoration: none; color: black; }\n")
	b.WriteString("</style>\n</head>\n<body>\n")
	fmt.Fprintf(&b, "<h1>%s</h1>\n", html.EscapeString(opts.HeaderText))
	writeList(&b, buildTree(entries), opts.ForwardLinks)
	b.WriteString("</body>\n</html>\n")

	if opts.XSLStyleSheet != "" {
		return b.String(), fmt.Errorf(
			"toc: --xsl-style-sheet %q is not supported (no XSLT engine); using built-in TOC style",
			opts.XSLStyleSheet)
	}
	return b.String(), nil
}

// buildTree nests the flattened depth-first entries by Level. Level jumps
// deeper than parent+1 are clamped so orphaned entries still render.
func buildTree(entries []pdfops.OutlineEntry) []*node {
	var roots []*node
	// stack[i] points at the children slice receiving entries of level i+1.
	stack := []*[]*node{&roots}
	for _, e := range entries {
		l := e.Level
		if l < 1 {
			l = 1
		}
		if l > len(stack) {
			l = len(stack)
		}
		stack = stack[:l]
		n := &node{entry: e}
		*stack[l-1] = append(*stack[l-1], n)
		stack = append(stack, &n.kids)
	}
	return roots
}

// writeList emits one <ul> level of TOC entries and recurses into children,
// matching the li>div>a+span structure of wkhtmltopdf's default XSLT.
func writeList(b *strings.Builder, nodes []*node, links bool) {
	b.WriteString("<ul>\n")
	for _, n := range nodes {
		b.WriteString("<li><div>")
		if links {
			fmt.Fprintf(b, "<a href=\"#dest-p%d\">", n.entry.Page)
		} else {
			b.WriteString("<a>")
		}
		b.WriteString(html.EscapeString(n.entry.Title))
		b.WriteString("</a>")
		fmt.Fprintf(b, "<span>%d</span></div>", n.entry.Page)
		if len(n.kids) > 0 {
			b.WriteString("\n")
			writeList(b, n.kids, links)
		}
		b.WriteString("</li>\n")
	}
	b.WriteString("</ul>\n")
}

// formatScale renders FontScale as a CSS percentage number the way Qt
// printed it (6 significant digits, no trailing zeros): 0.8 -> "80".
func formatScale(scale float64) string {
	return strconv.FormatFloat(scale*100, 'g', 6, 64)
}
