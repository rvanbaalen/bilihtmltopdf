package hf

import (
	"strings"
	"testing"
	"time"

	"github.com/rvanbaalen/bilihtmltopdf/internal/args"
)

// buildOpts returns HFOptions for a header with the given three texts.
func buildOpts(left, center, right string) HFOptions {
	hf := args.NewHeaderFooter()
	hf.Left, hf.Center, hf.Right = left, center, right
	return HFOptions{HF: hf, IsHeader: true}
}

func TestBuildTemplateEmpty(t *testing.T) {
	got, _, err := BuildTemplate(buildOpts("", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Errorf("empty header/footer must yield empty template, got %q", got)
	}
}

func TestBuildTemplatePlaceholders(t *testing.T) {
	got, _, err := BuildTemplate(buildOpts("[title]", "[webpage]", "Page [page] of [topage]"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<span class="pageNumber"></span>`,
		`<span class="totalPages"></span>`,
		`<span class="url"></span>`,
		`<span class="title"></span>`,
		"font-family:'Arial',sans-serif",
		"font-size:12pt",
		"Page ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("template missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "border-bottom") {
		t.Error("no --header-line requested but border present")
	}
}

func TestBuildTemplateLineAndFont(t *testing.T) {
	hf := args.NewHeaderFooter()
	hf.Center = "x"
	hf.Line = true
	hf.FontName = "Times New Roman"
	hf.FontSize = 9

	header, _, err := BuildTemplate(HFOptions{HF: hf, IsHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(header, "border-bottom:1px solid") {
		t.Error("header line must render as border-bottom")
	}
	if !strings.Contains(header, "font-family:'Times New Roman'") || !strings.Contains(header, "font-size:9pt") {
		t.Errorf("font settings not applied:\n%s", header)
	}

	footer, _, err := BuildTemplate(HFOptions{HF: hf, IsHeader: false})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(footer, "border-top:1px solid") {
		t.Error("footer line must render as border-top")
	}
}

func TestBuildTemplateTitleAndReplace(t *testing.T) {
	got, _, err := BuildTemplate(HFOptions{
		HF: func() args.HeaderFooter {
			h := args.NewHeaderFooter()
			h.Left = "[title] — [env] & [isodate]"
			return h
		}(),
		IsHeader:     true,
		Title:        "My <Doc>",
		Replacements: map[string]string{"env": "prod<1>"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "My &lt;Doc&gt;") {
		t.Errorf("--title must substitute [title] escaped:\n%s", got)
	}
	if !strings.Contains(got, "prod&lt;1&gt;") {
		t.Errorf("--replace value must substitute escaped:\n%s", got)
	}
	if !strings.Contains(got, time.Now().Format("2006-01-02")) {
		t.Errorf("[isodate] must bake today's date:\n%s", got)
	}
}

func TestBuildTemplateUnsupportedVars(t *testing.T) {
	got, warns, err := BuildTemplate(buildOpts("[section]", "", "[sitepage]/[sitepages]"))
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"[section]", "[sitepage]", "[sitepages]"} {
		if strings.Contains(got, gone) {
			t.Errorf("unsupported variable %s must substitute to empty:\n%s", gone, got)
		}
	}
	if len(warns) == 0 || !strings.Contains(strings.Join(warns, " "), "[section]") {
		t.Errorf("unsupported variables must be returned as warnings, got %v", warns)
	}
}

func TestBuildTemplateCaseInsensitiveVars(t *testing.T) {
	// --default-header emits [toPage]; hfreplace substitutes case-insensitively.
	got, _, err := BuildTemplate(buildOpts("", "", "[page]/[toPage]"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `<span class="totalPages"></span>`) {
		t.Errorf("[toPage] must substitute case-insensitively:\n%s", got)
	}
}

func TestBuildTemplateEscapesText(t *testing.T) {
	got, _, err := BuildTemplate(buildOpts(`<b onmouseover="x">`, "", ""))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "<b ") {
		t.Errorf("literal text must be HTML-escaped:\n%s", got)
	}
}

func TestBuildTemplateRejectsHTMLPath(t *testing.T) {
	hf := args.NewHeaderFooter()
	hf.HTMLPath = "x.html"
	if _, _, err := BuildTemplate(HFOptions{HF: hf}); err == nil {
		t.Error("HTMLPath set must be rejected; caller should use TranslateHTMLFile")
	}
}

func TestTranslateHTMLFile(t *testing.T) {
	got, warnings, err := TranslateHTMLFile("testdata/header.html")
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(got, "<script") {
		t.Error("scripts must be stripped")
	}
	for _, want := range []string{
		`class="page pageNumber"`,          // wkhtmltopdf class → CDP class appended
		`class="topage totalPages"`,        // ditto
		`<span class="pageNumber"></span>`, // [page] text substitution
		`<span class="totalPages"></span>`, // [topage]
		`<span class="date"></span>`,       // [date]
		"data:image/png;base64,",           // <img> and CSS url() inlined
		"<style>",
		"td { padding: 2px; }",   // inline <style> hoisted
		"." + wrapperClass + " ", // body selector rewritten
		"font-size:12pt",         // wrapper sets explicit font size
		"border:0; margin:0;",    // body's own style carried onto wrapper
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, `url("dot.png")`) || strings.Contains(got, "url(dot.png)") {
		t.Error("CSS url(dot.png) must be inlined as data URI")
	}
	if !strings.Contains(got, "url(https://example.com/x.png)") {
		t.Error("remote CSS url must be left as-is")
	}
	if strings.Contains(got, "<body") {
		t.Error("body must be replaced by a wrapper div")
	}

	wantWarn := func(substr string) {
		t.Helper()
		for _, w := range warnings {
			if strings.Contains(w, substr) {
				return
			}
		}
		t.Errorf("missing warning containing %q, got %v", substr, warnings)
	}
	wantWarn("scripts")
	wantWarn(`"section"`)
	wantWarn("remote resource")
}

func TestTranslateHTMLFileMissing(t *testing.T) {
	if _, _, err := TranslateHTMLFile("testdata/nope.html"); err == nil {
		t.Error("missing file must return an error")
	}
}
