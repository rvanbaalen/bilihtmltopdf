// Command wkhtmltopdf is a drop-in replacement for the wkhtmltopdf CLI,
// rendering HTML to PDF through headless Chromium (CDP) and pdfcpu.
// Unsupported legacy switches warn to stderr; --strict makes them fatal.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/rvanbaalen/bilihtmltopdf/internal/args"
	"github.com/rvanbaalen/bilihtmltopdf/internal/chrome"
	"github.com/rvanbaalen/bilihtmltopdf/internal/hf"
	"github.com/rvanbaalen/bilihtmltopdf/internal/pdfops"
	"github.com/rvanbaalen/bilihtmltopdf/internal/toc"
)

// producerName is stamped into the PDF /Producer field.
const producerName = "bilihtmltopdf"

// tocSizeIterations bounds the render/measure loop that stabilizes TOC
// page numbers against the TOC's own page count.
const tocSizeIterations = 3

func main() {
	os.Exit(run(os.Args[1:]))
}

// run parses argv and executes the full pipeline, returning the process
// exit code (0 ok, 1 error). Help/version text exits 0 via ExitText.
func run(argv []string) int {
	cmd, err := args.Parse(argv)
	if err != nil {
		var et *args.ExitText
		if errors.As(err, &et) {
			fmt.Print(et.Text)
			return 0
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	rep := &reporter{quiet: cmd.Global.Quiet, strict: cmd.Global.Strict}
	for _, w := range cmd.Warnings {
		if err := rep.warn(w); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return 1
		}
	}
	if err := pipeline(context.Background(), cmd, rep); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return 1
	}
	return 0
}

// reporter prints warnings and progress lines to stderr, turning
// warnings into errors under --strict and muting both under -q
// (wkhtmltopdf -q is documented as --log-level none).
type reporter struct {
	quiet  bool
	strict bool
}

// warn prints a compatibility warning unless --quiet; under --strict it
// returns the warning as a fatal error instead.
func (r *reporter) warn(msg string) error {
	if r.strict {
		return fmt.Errorf("treating warning as error (--strict): %s", msg)
	}
	if !r.quiet {
		fmt.Fprintln(os.Stderr, "Warning: "+msg)
	}
	return nil
}

// notice prints a runtime notice (e.g. a blocked file load) unless
// --quiet. Notices mirror wkhtmltopdf load warnings and are never
// fatal, even under --strict.
func (r *reporter) notice(msg string) {
	if !r.quiet {
		fmt.Fprintln(os.Stderr, "Warning: "+msg)
	}
}

// progress prints a wkhtmltopdf-style stage line unless --quiet is set.
func (r *reporter) progress(format string, a ...any) {
	if !r.quiet {
		fmt.Fprintf(os.Stderr, format+"\n", a...)
	}
}

// layout carries the resolved CDP print geometry shared by all objects:
// paper size, base margins (inches), and orientation.
type layout struct {
	paperW, paperH   float64
	marginT, marginB float64
	marginL, marginR float64
	landscape        bool
}

// pipeline renders every object, resolves TOC page numbers, merges the
// parts in command-line order, applies metadata/encryption, and writes
// the output.
func pipeline(ctx context.Context, cmd *args.Command, rep *reporter) error {
	g := cmd.Global
	chromePath, err := chrome.FindChrome()
	if err != nil {
		return err
	}
	lay, err := resolveLayout(g)
	if err != nil {
		return err
	}

	// CDP evaluates [page]/[topage] per printToPDF call, so numbering
	// restarts for every object; wkhtmltopdf numbered continuously.
	if len(cmd.Objects) > 1 && usesPageNumbering(cmd.Objects) {
		if err := rep.warn("with multiple input documents, header/footer page numbers " +
			"([page]/[topage]/[frompage]) restart at 1 for each document; " +
			"wkhtmltopdf numbered pages continuously across documents"); err != nil {
			return err
		}
	}

	hasTOC := false
	for _, obj := range cmd.Objects {
		if obj.Kind() == "toc" {
			hasTOC = true
		}
	}

	n := len(cmd.Objects)
	pdfs := make([][]byte, n)
	counts := make([]int, n)
	entries := make([][]pdfops.OutlineEntry, n)

	rep.progress("Loading pages (1/6)")
	for i, obj := range cmd.Objects {
		if obj.Kind() == "toc" {
			counts[i] = 1 // placeholder until the TOC is rendered
			continue
		}
		pdf, err := renderObject(ctx, chromePath, g, lay, obj, hasTOC, rep)
		if err != nil {
			return err
		}
		pdfs[i] = pdf
	}

	rep.progress("Counting pages (2/6)")
	if hasTOC {
		for i, obj := range cmd.Objects {
			if obj.Kind() == "toc" {
				continue
			}
			if counts[i], err = pdfops.PageCount(pdfs[i]); err != nil {
				return fmt.Errorf("counting pages of %s: %w", obj.Options().Input, err)
			}
			if obj.Options().IncludeInOutline {
				if entries[i], err = pdfops.ReadOutline(pdfs[i]); err != nil {
					return fmt.Errorf("reading outline of %s: %w", obj.Options().Input, err)
				}
			}
		}
		rep.progress("Generating table of contents (3/6)")
		if err := renderTOCs(ctx, chromePath, cmd, lay, pdfs, counts, entries, rep); err != nil {
			return err
		}
	}

	rep.progress("Resolving links (4/6)")
	rep.progress("Loading headers and footers (5/6)")
	rep.progress("Printing pages (6/6)")

	inputs := make([][]byte, 0, n)
	for _, pdf := range pdfs {
		inputs = append(inputs, pdf)
	}
	out, err := pdfops.Merge(inputs, true)
	if err != nil {
		return fmt.Errorf("merging objects: %w", err)
	}

	if out, err = pdfops.SetMetadata(out, g.Title, producerName); err != nil {
		return fmt.Errorf("setting metadata: %w", err)
	}
	if g.OwnerPassword != "" || g.UserPassword != "" {
		if err := rep.warn("encryption uses AES-256; wkhtmltopdf produced RC4-40"); err != nil {
			return err
		}
		if out, err = pdfops.Encrypt(out, g.OwnerPassword, g.UserPassword); err != nil {
			return fmt.Errorf("encrypting output: %w", err)
		}
	}

	if err := writeOutput(g.Output, out); err != nil {
		return err
	}
	rep.progress("Done")
	return nil
}

// renderObject prints one page or cover object to PDF bytes with its
// header/footer templates and spacing-adjusted margins.
func renderObject(ctx context.Context, chromePath string, g args.GlobalOptions, lay layout, obj args.Object, hasTOC bool, rep *reporter) ([]byte, error) {
	p := obj.Options()
	var header, footer string
	var err error
	if obj.Kind() != "cover" { // covers never carry headers/footers
		if header, err = buildHF(p.Header, true, g, p, rep); err != nil {
			return nil, err
		}
		if footer, err = buildHF(p.Footer, false, g, p, rep); err != nil {
			return nil, err
		}
	}
	req := printRequest(chromePath, g, lay, *p, header, footer)
	req.GenerateOutline = (g.Outline || hasTOC) && p.IncludeInOutline
	req.Warn = rep.notice
	pdf, err := chrome.PrintPDF(ctx, req)
	if err != nil {
		return nil, err
	}
	return pdf, nil
}

// pageVarRe detects per-object page numbering in header/footer settings:
// the [page]-family variables or their substitution classes in HTML files.
var pageVarRe = regexp.MustCompile(`(?i)\[(page|topage|frompage)\]|class\s*=\s*['"][^'"]*\b(page|topage|frompage|pagenumber|totalpages)\b`)

// usesPageNumbering reports whether any non-cover object's header or
// footer references page numbers ([page]/[topage]/[frompage]).
func usesPageNumbering(objs []args.Object) bool {
	for _, obj := range objs {
		if obj.Kind() == "cover" { // covers carry no headers/footers
			continue
		}
		p := obj.Options()
		for _, hf := range []args.HeaderFooter{p.Header, p.Footer} {
			if pageVarRe.MatchString(hf.Left + " " + hf.Center + " " + hf.Right) {
				return true
			}
			if hf.HTMLPath == "" {
				continue
			}
			// Unreadable files fail loudly later; skip them here.
			if raw, err := os.ReadFile(hf.HTMLPath); err == nil && pageVarRe.Match(raw) {
				return true
			}
		}
	}
	return false
}

// printRequest assembles the chrome.PrintRequest for one object,
// folding header/footer spacing into the top/bottom margins.
func printRequest(chromePath string, g args.GlobalOptions, lay layout, p args.PageOptions, header, footer string) chrome.PrintRequest {
	mt, mb := lay.marginT, lay.marginB
	if header != "" {
		mt += p.Header.Spacing / 25.4
	}
	if footer != "" {
		mb += p.Footer.Spacing / 25.4
	}
	return chrome.PrintRequest{
		ChromePath:      chromePath,
		Page:            p,
		Landscape:       lay.landscape,
		PaperWidth:      lay.paperW,
		PaperHeight:     lay.paperH,
		MarginTop:       mt,
		MarginBottom:    mb,
		MarginLeft:      lay.marginL,
		MarginRight:     lay.marginR,
		Scale:           p.Zoom,
		HeaderTemplate:  header,
		FooterTemplate:  footer,
		PrintBackground: p.Background,
	}
}

// buildHF returns the CDP template for one header or footer: either the
// translated --header-html file or the generated left/center/right one.
func buildHF(set args.HeaderFooter, isHeader bool, g args.GlobalOptions, p *args.PageOptions, rep *reporter) (string, error) {
	if set.HTMLPath != "" {
		tpl, warns, err := hf.TranslateHTMLFile(set.HTMLPath)
		if err != nil {
			return "", err
		}
		for _, w := range warns {
			if err := rep.warn(w); err != nil {
				return "", err
			}
		}
		return tpl, nil
	}
	tpl, warns, err := hf.BuildTemplate(hf.HFOptions{
		HF:           set,
		IsHeader:     isHeader,
		Title:        g.Title,
		PageOffset:   g.PageOffset,
		Replacements: p.Replacements,
	})
	if err != nil {
		return "", err
	}
	for _, w := range warns {
		if err := rep.warn(w); err != nil {
			return "", err
		}
	}
	return tpl, nil
}

// renderTOCs renders every toc object, iterating until each TOC's own
// page count stops shifting the page numbers it displays.
func renderTOCs(ctx context.Context, chromePath string, cmd *args.Command, lay layout, pdfs [][]byte, counts []int, entries [][]pdfops.OutlineEntry, rep *reporter) error {
	g := cmd.Global
	for iter := 0; iter < tocSizeIterations; iter++ {
		changed := false
		for i, obj := range cmd.Objects {
			tocObj, ok := obj.(*args.TOCObject)
			if !ok {
				continue
			}
			html, err := toc.GenerateHTML(tocEntries(cmd, counts, entries), tocObj.TOC)
			if err != nil {
				// GenerateHTML degrades gracefully: usable HTML plus a
				// warning-grade error (e.g. --xsl-style-sheet).
				if iter == 0 {
					if werr := rep.warn(err.Error()); werr != nil {
						return werr
					}
				}
			}
			pdf, err := renderTOCHTML(ctx, chromePath, g, lay, tocObj, html, rep)
			if err != nil {
				return err
			}
			pages, err := pdfops.PageCount(pdf)
			if err != nil {
				return fmt.Errorf("counting toc pages: %w", err)
			}
			pdfs[i] = pdf
			if pages != counts[i] {
				counts[i] = pages
				changed = true
			}
		}
		if !changed {
			return nil
		}
	}
	return nil
}

// tocEntries flattens the outlines of all included objects into final-
// document page numbers, capped at the configured outline depth.
func tocEntries(cmd *args.Command, counts []int, entries [][]pdfops.OutlineEntry) []pdfops.OutlineEntry {
	depth := cmd.Global.OutlineDepth
	var out []pdfops.OutlineEntry
	start := 1
	for i := range cmd.Objects {
		for _, e := range entries[i] {
			if depth > 0 && e.Level > depth {
				continue
			}
			out = append(out, pdfops.OutlineEntry{
				Title: e.Title,
				Page:  e.Page + start - 1,
				Level: e.Level,
			})
		}
		start += counts[i]
	}
	return out
}

// renderTOCHTML writes the generated TOC HTML to a temp file and prints
// it with the toc object's header/footer settings.
func renderTOCHTML(ctx context.Context, chromePath string, g args.GlobalOptions, lay layout, tocObj *args.TOCObject, html string, rep *reporter) ([]byte, error) {
	tmp, err := os.CreateTemp("", "bilihtmltopdf-toc-*.html")
	if err != nil {
		return nil, fmt.Errorf("creating toc temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(html); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("writing toc temp file: %w", err)
	}
	tmp.Close()

	p := tocObj.Page
	p.Input = tmp.Name()
	header, err := buildHF(p.Header, true, g, &p, rep)
	if err != nil {
		return nil, err
	}
	footer, err := buildHF(p.Footer, false, g, &p, rep)
	if err != nil {
		return nil, err
	}
	req := printRequest(chromePath, g, lay, p, header, footer)
	req.GenerateOutline = g.Outline
	req.Warn = rep.notice
	return chrome.PrintPDF(ctx, req)
}

// resolveLayout converts the global page-size/margin settings into the
// inch-based geometry Page.printToPDF expects.
func resolveLayout(g args.GlobalOptions) (layout, error) {
	var lay layout
	var err error
	if g.PageWidth != "" || g.PageHeight != "" {
		if g.PageWidth == "" || g.PageHeight == "" {
			return lay, errors.New("--page-width and --page-height must be given together")
		}
		if lay.paperW, err = cssToInches(g.PageWidth); err != nil {
			return lay, fmt.Errorf("--page-width: %w", err)
		}
		if lay.paperH, err = cssToInches(g.PageHeight); err != nil {
			return lay, fmt.Errorf("--page-height: %w", err)
		}
	} else {
		w, h, ok := paperSize(g.PageSize)
		if !ok {
			// wkhtmltopdf rejects unknown sizes; a silent A4 fallback
			// would ship wrong-sized PDFs on a typo.
			return lay, fmt.Errorf("invalid --page-size %q", g.PageSize)
		}
		lay.paperW, lay.paperH = w, h
	}
	lay.landscape = g.Orientation == args.Landscape
	if lay.marginT, err = cssToInches(g.MarginTop); err != nil {
		return lay, fmt.Errorf("--margin-top: %w", err)
	}
	if lay.marginB, err = cssToInches(g.MarginBottom); err != nil {
		return lay, fmt.Errorf("--margin-bottom: %w", err)
	}
	if lay.marginL, err = cssToInches(g.MarginLeft); err != nil {
		return lay, fmt.Errorf("--margin-left: %w", err)
	}
	if lay.marginR, err = cssToInches(g.MarginRight); err != nil {
		return lay, fmt.Errorf("--margin-right: %w", err)
	}
	return lay, nil
}

// paperSizesMM maps lowercase QPrinter paper-size names to portrait
// width/height in millimeters.
var paperSizesMM = map[string][2]float64{
	"a0": {841, 1189}, "a1": {594, 841}, "a2": {420, 594}, "a3": {297, 420},
	"a4": {210, 297}, "a5": {148, 210}, "a6": {105, 148}, "a7": {74, 105},
	"a8": {52, 74}, "a9": {37, 52},
	"b0": {1000, 1414}, "b1": {707, 1000}, "b2": {500, 707}, "b3": {353, 500},
	"b4": {250, 353}, "b5": {176, 250}, "b6": {125, 176}, "b7": {88, 125},
	"b8": {62, 88}, "b9": {44, 62}, "b10": {31, 44},
	"c5e": {163, 229}, "comm10e": {105, 241}, "dle": {110, 220},
	"executive": {190.5, 254}, "folio": {210, 330},
	"ledger": {432, 279}, "legal": {215.9, 355.6},
	"letter": {215.9, 279.4}, "tabloid": {279, 432},
}

// paperSize resolves a --page-size name to portrait inches.
func paperSize(name string) (w, h float64, ok bool) {
	mm, ok := paperSizesMM[strings.ToLower(name)]
	if !ok {
		return 0, 0, false
	}
	return mm[0] / 25.4, mm[1] / 25.4, true
}

// cssToInches parses a wkhtmltopdf unit string ("10mm", "0.5in", "20")
// into inches; a bare number is millimeters, matching UnitReal.
func cssToInches(s string) (float64, error) {
	v := strings.TrimSpace(strings.ToLower(s))
	factor := 1.0 / 25.4 // default unit: mm
	for _, u := range []struct {
		suffix string
		f      float64
	}{
		{"mm", 1.0 / 25.4}, {"cm", 10.0 / 25.4}, {"in", 1},
		{"pt", 1.0 / 72}, {"px", 1.0 / 96}, {"pc", 12.0 / 72},
	} {
		if strings.HasSuffix(v, u.suffix) {
			v = strings.TrimSpace(strings.TrimSuffix(v, u.suffix))
			factor = u.f
			break
		}
	}
	n, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid unit value %q", s)
	}
	return n * factor, nil
}

// writeOutput writes the finished PDF to path, or to stdout for "-".
func writeOutput(path string, pdf []byte) error {
	if path == "-" {
		if _, err := os.Stdout.Write(pdf); err != nil {
			return fmt.Errorf("writing to stdout: %w", err)
		}
		return nil
	}
	if err := os.WriteFile(path, pdf, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
