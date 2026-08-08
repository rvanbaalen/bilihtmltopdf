package toc

import (
	"strings"
	"testing"

	"github.com/rvanbaalen/bilihtmltopdf/internal/args"
	"github.com/rvanbaalen/bilihtmltopdf/internal/pdfops"
)

// sampleEntries is a small two-level outline shared across tests.
func sampleEntries() []pdfops.OutlineEntry {
	return []pdfops.OutlineEntry{
		{Title: "Intro", Page: 3, Level: 1},
		{Title: "Chapter 1", Page: 4, Level: 1},
		{Title: "Section 1.1", Page: 5, Level: 2},
		{Title: "Chapter 2", Page: 7, Level: 1},
	}
}

const goldenDefault = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8"/>
<title>Table of Contents</title>
<style>
h1 { text-align: center; font-size: 20px; font-family: arial; }
div { border-bottom: 1px dashed rgb(200,200,200); }
span { float: right; }
li { list-style: none; }
ul { font-size: 20px; font-family: arial; padding-left: 0em; }
ul ul { font-size: 80%; padding-left: 1em; }
a { text-decoration: none; color: black; }
</style>
</head>
<body>
<h1>Table of Contents</h1>
<ul>
<li><div><a href="#dest-p3">Intro</a><span>3</span></div></li>
<li><div><a href="#dest-p4">Chapter 1</a><span>4</span></div>
<ul>
<li><div><a href="#dest-p5">Section 1.1</a><span>5</span></div></li>
</ul>
</li>
<li><div><a href="#dest-p7">Chapter 2</a><span>7</span></div></li>
</ul>
</body>
</html>
`

func TestGenerateHTMLDefaultGolden(t *testing.T) {
	got, err := GenerateHTML(sampleEntries(), args.NewTOCOptions())
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if got != goldenDefault {
		t.Errorf("golden mismatch\n--- got ---\n%s\n--- want ---\n%s", got, goldenDefault)
	}
}

func TestGenerateHTMLDisableDottedLines(t *testing.T) {
	opts := args.NewTOCOptions()
	opts.UseDottedLines = false
	got, err := GenerateHTML(sampleEntries(), opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if strings.Contains(got, "border-bottom") {
		t.Errorf("dotted-lines rule present despite UseDottedLines=false:\n%s", got)
	}
}

func TestGenerateHTMLNoForwardLinks(t *testing.T) {
	opts := args.NewTOCOptions()
	opts.ForwardLinks = false
	got, err := GenerateHTML(sampleEntries(), opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if strings.Contains(got, "href=") {
		t.Errorf("href present despite ForwardLinks=false:\n%s", got)
	}
	if !strings.Contains(got, "<a>Intro</a>") {
		t.Errorf("bare anchor missing:\n%s", got)
	}
}

func TestGenerateHTMLHeaderTextEscaped(t *testing.T) {
	opts := args.NewTOCOptions()
	opts.HeaderText = `R&D <"Guide">`
	got, err := GenerateHTML(sampleEntries(), opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	want := "<h1>R&amp;D &lt;&#34;Guide&#34;&gt;</h1>"
	if !strings.Contains(got, want) {
		t.Errorf("escaped caption %q missing:\n%s", want, got)
	}
	if !strings.Contains(got, "<title>R&amp;D &lt;&#34;Guide&#34;&gt;</title>") {
		t.Errorf("escaped <title> missing:\n%s", got)
	}
}

func TestGenerateHTMLEntryTitleEscaped(t *testing.T) {
	entries := []pdfops.OutlineEntry{{Title: "A <b>&</b>", Page: 1, Level: 1}}
	got, err := GenerateHTML(entries, args.NewTOCOptions())
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if !strings.Contains(got, ">A &lt;b&gt;&amp;&lt;/b&gt;</a>") {
		t.Errorf("escaped entry title missing:\n%s", got)
	}
}

func TestGenerateHTMLIndentationAndFontScale(t *testing.T) {
	opts := args.NewTOCOptions()
	opts.Indentation = "2em"
	opts.FontScale = 0.65
	got, err := GenerateHTML(sampleEntries(), opts)
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if !strings.Contains(got, "ul ul { font-size: 65%; padding-left: 2em; }") {
		t.Errorf("custom indent/scale rule missing:\n%s", got)
	}
}

func TestGenerateHTMLLevelJumpClamped(t *testing.T) {
	// Level jumps 1 -> 3: entry must nest at depth 2, not vanish.
	entries := []pdfops.OutlineEntry{
		{Title: "Top", Page: 1, Level: 1},
		{Title: "Deep", Page: 2, Level: 3},
	}
	got, err := GenerateHTML(entries, args.NewTOCOptions())
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	want := "<li><div><a href=\"#dest-p1\">Top</a><span>1</span></div>\n<ul>\n<li><div><a href=\"#dest-p2\">Deep</a><span>2</span></div></li>\n</ul>\n</li>"
	if !strings.Contains(got, want) {
		t.Errorf("clamped nesting missing\n--- want fragment ---\n%s\n--- got ---\n%s", want, got)
	}
}

func TestGenerateHTMLEmptyEntries(t *testing.T) {
	got, err := GenerateHTML(nil, args.NewTOCOptions())
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if !strings.Contains(got, "<h1>Table of Contents</h1>\n<ul>\n</ul>") {
		t.Errorf("empty TOC body malformed:\n%s", got)
	}
}

func TestGenerateHTMLXSLStyleSheetWarns(t *testing.T) {
	opts := args.NewTOCOptions()
	opts.XSLStyleSheet = "custom.xsl"
	got, err := GenerateHTML(sampleEntries(), opts)
	if err == nil {
		t.Fatal("want warning error for XSLStyleSheet, got nil")
	}
	if !strings.Contains(err.Error(), "--xsl-style-sheet") || !strings.Contains(err.Error(), "custom.xsl") {
		t.Errorf("warning error lacks flag/path: %v", err)
	}
	// Built-in HTML must still be usable so callers can warn and continue.
	if got != goldenDefault {
		t.Errorf("HTML alongside warning differs from built-in output")
	}
}

func TestGenerateHTMLZeroValueOptsGuarded(t *testing.T) {
	got, err := GenerateHTML(sampleEntries(), args.TOCOptions{})
	if err != nil {
		t.Fatalf("GenerateHTML: %v", err)
	}
	if !strings.Contains(got, "ul ul { font-size: 80%; padding-left: 1em; }") {
		t.Errorf("zero-value opts not defaulted:\n%s", got)
	}
}
