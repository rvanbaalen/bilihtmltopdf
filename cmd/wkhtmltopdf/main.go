// Command wkhtmltopdf is a drop-in replacement for the wkhtmltopdf CLI,
// rendering HTML to PDF through headless Chromium (CDP) and pdfcpu.
// Unsupported legacy switches warn to stderr; --strict makes them fatal.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

	// Every object's page count is needed to map object-local pages to
	// final document page numbers for continuous header/footer numbering.
	for i := range cmd.Objects {
		if counts[i] == 0 {
			if counts[i], err = pdfops.PageCount(pdfs[i]); err != nil {
				return fmt.Errorf("counting pages of %s: %w", cmd.Objects[i].Options().Input, err)
			}
		}
	}

	out, err := pdfops.Merge(pdfs, true)
	if err != nil {
		return fmt.Errorf("merging objects: %w", err)
	}

	rep.progress("Loading headers and footers (5/6)")
	if out, err = compositeHeadersFooters(ctx, chromePath, cmd, lay, out, counts, rep); err != nil {
		return fmt.Errorf("compositing headers and footers: %w", err)
	}

	rep.progress("Printing pages (6/6)")
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

// renderObject prints one page or cover object to content-only PDF bytes.
// Headers and footers are composited separately (compositeObject), matching
// wkhtmltopdf: they are full pages rendered by the same engine and laid over
// the content, not Chromium's cramped native template mechanism.
func renderObject(ctx context.Context, chromePath string, g args.GlobalOptions, lay layout, obj args.Object, hasTOC bool, rep *reporter) ([]byte, error) {
	p := obj.Options()
	req := printRequest(chromePath, g, lay, *p)
	req.GenerateOutline = (g.Outline || hasTOC) && p.IncludeInOutline
	req.Warn = rep.notice
	return chrome.PrintPDF(ctx, req)
}

// compositeHeadersFooters overlays each object's header and footer onto
// the merged document. Following wkhtmltopdf, every header/footer is a full
// page rendered by the same engine and laid over the content; page numbers
// ([page]/[topage]/[frompage]) resolve to literal values against the final,
// continuously-numbered document. Covers carry no header/footer.
func compositeHeadersFooters(ctx context.Context, chromePath string, cmd *args.Command, lay layout, merged []byte, counts []int, rep *reporter) ([]byte, error) {
	g := cmd.Global
	total, err := pdfops.PageCount(merged)
	if err != nil {
		return nil, fmt.Errorf("counting merged pages: %w", err)
	}
	headerStamps := map[int][]byte{}
	footerStamps := map[int][]byte{}
	cache := map[string][]byte{} // substituted HTML -> rendered stamp PDF

	start := 1
	for i, obj := range cmd.Objects {
		count := counts[i]
		if obj.Kind() == "cover" { // covers never carry headers/footers
			start += count
			continue
		}
		p := obj.Options()
		for local := 0; local < count; local++ {
			finalPage := start + local
			vars := hf.PageVars{
				Page:     finalPage + g.PageOffset,
				Topage:   total,
				Frompage: start + g.PageOffset,
			}
			if err := stampSide(ctx, chromePath, g, lay, p, p.Header, true, vars, finalPage, headerStamps, cache, rep); err != nil {
				return nil, err
			}
			if err := stampSide(ctx, chromePath, g, lay, p, p.Footer, false, vars, finalPage, footerStamps, cache, rep); err != nil {
				return nil, err
			}
		}
		start += count
	}

	if merged, err = pdfops.OverlayFullPage(merged, headerStamps); err != nil {
		return nil, err
	}
	return pdfops.OverlayFullPage(merged, footerStamps)
}

// stampSide renders one page's header or footer (if configured) and records
// the stamp for finalPage, reusing a cached render for identical content.
func stampSide(ctx context.Context, chromePath string, g args.GlobalOptions, lay layout, p *args.PageOptions, set args.HeaderFooter, isHeader bool, vars hf.PageVars, finalPage int, stamps map[int][]byte, cache map[string][]byte, rep *reporter) error {
	html, warns, err := hfHTML(set, isHeader, g, p, vars)
	if err != nil {
		return err
	}
	for _, w := range warns {
		if err := rep.warn(w); err != nil {
			return err
		}
	}
	if html == "" {
		return nil
	}
	if stamp, ok := cache[html]; ok {
		stamps[finalPage] = stamp
		return nil
	}
	stamp, err := renderHFPage(ctx, chromePath, lay, p, set.HTMLPath, html, rep)
	if err != nil {
		return err
	}
	cache[html] = stamp
	stamps[finalPage] = stamp
	return nil
}

// hfHTML returns the full-page HTML for one header/footer on one page: the
// substituted --header-html file, or a generated left/center/right bar.
// Returns "" when nothing is configured.
func hfHTML(set args.HeaderFooter, isHeader bool, g args.GlobalOptions, p *args.PageOptions, vars hf.PageVars) (string, []string, error) {
	if set.HTMLPath != "" {
		raw, err := os.ReadFile(set.HTMLPath)
		if err != nil {
			return "", nil, fmt.Errorf("reading %s: %w", set.HTMLPath, err)
		}
		html, warns := hf.SubstituteHTML(string(raw), vars, g.Title, p.Replacements)
		return html, warns, nil
	}
	if set.Left == "" && set.Center == "" && set.Right == "" && !set.Line {
		return "", nil, nil
	}
	html, warns := hf.BuildPage(hf.HFOptions{
		HF:           set,
		IsHeader:     isHeader,
		Title:        g.Title,
		PageOffset:   g.PageOffset,
		Replacements: p.Replacements,
	}, vars)
	return html, warns, nil
}

// renderHFPage renders a header/footer HTML document to a full-page,
// zero-margin PDF for compositing. When the content came from a
// --header-html/--footer-html file, the temp file is written beside the
// original so its relative resources still resolve; generated bars carry no
// relative resources and go to the system temp dir.
func renderHFPage(ctx context.Context, chromePath string, lay layout, p *args.PageOptions, srcPath, html string, rep *reporter) ([]byte, error) {
	dir := ""
	if srcPath != "" {
		dir = filepath.Dir(srcPath)
	}
	tmp, err := os.CreateTemp(dir, "bilihtmltopdf-hf-*.html")
	if err != nil {
		return nil, fmt.Errorf("creating header/footer temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	// The header/footer page is composited over the content, so its own
	// page background must be transparent (a stylesheet like Bootstrap
	// paints body white, which would white out the content). Only the
	// header/footer's own elements should paint. Appended last so it wins.
	html += `<style>html,body{background:transparent !important;}</style>`
	if _, err := tmp.WriteString(html); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("writing header/footer temp file: %w", err)
	}
	tmp.Close()

	hp := *p // inherit cookies/headers/auth/media so footer CSS loads identically
	hp.Input = tmp.Name()
	req := chrome.PrintRequest{
		ChromePath:  chromePath,
		Page:        hp,
		Landscape:   lay.landscape,
		PaperWidth:  lay.paperW,
		PaperHeight: lay.paperH,
		// Render with the content's own margins so the header/footer's
		// absolute positioning (top:0 / bottom:0) lands at the edges of the
		// content area — where wkhtmltopdf placed its separately-rendered
		// header/footer, just outside the body text — rather than at the
		// paper edge.
		MarginTop:       lay.marginT,
		MarginBottom:    lay.marginB,
		MarginLeft:      lay.marginL,
		MarginRight:     lay.marginR,
		Scale:           p.Zoom,
		PrintBackground: p.Background,
		Warn:            rep.notice,
	}
	return chrome.PrintPDF(ctx, req)
}

// printRequest assembles the content-only chrome.PrintRequest for one
// object. Header/footer geometry is handled by the compositing pass; the
// configured margins reserve the band they occupy (as in wkhtmltopdf,
// where --margin-top/bottom make room for the header/footer).
func printRequest(chromePath string, g args.GlobalOptions, lay layout, p args.PageOptions) chrome.PrintRequest {
	return chrome.PrintRequest{
		ChromePath:      chromePath,
		Page:            p,
		Landscape:       lay.landscape,
		PaperWidth:      lay.paperW,
		PaperHeight:     lay.paperH,
		MarginTop:       lay.marginT,
		MarginBottom:    lay.marginB,
		MarginLeft:      lay.marginL,
		MarginRight:     lay.marginR,
		Scale:           p.Zoom,
		PrintBackground: p.Background,
	}
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
	req := printRequest(chromePath, g, lay, p)
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
