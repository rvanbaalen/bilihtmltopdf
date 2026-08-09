// Package pdfops post-processes rendered PDFs with pdfcpu: merging
// multi-object output, reading/writing outlines, metadata, encryption,
// and page counting.
package pdfops

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// OutlineEntry is one bookmark in a PDF outline tree, flattened
// depth-first.
type OutlineEntry struct {
	// Title is the bookmark text.
	Title string
	// Page is the 1-based destination page number.
	Page int
	// Level is the nesting depth, 1 for top-level entries.
	Level int
}

var confOnce sync.Once

// newConf returns a fresh pdfcpu configuration that never touches the
// user's pdfcpu config directory.
func newConf() *model.Configuration {
	confOnce.Do(api.DisableConfigDir)
	return model.NewDefaultConfiguration()
}

// Merge concatenates the given PDFs in order into one document. When
// fixBookmarkOffsets is true, each input's bookmarks are preserved with
// their page destinations shifted by the preceding inputs' page counts.
func Merge(inputs [][]byte, fixBookmarkOffsets bool) ([]byte, error) {
	if len(inputs) == 0 {
		return nil, errors.New("pdfops: merge: no inputs")
	}
	if len(inputs) == 1 {
		return inputs[0], nil
	}

	// pdfcpu's raw merge drops source outlines, so collect them (with
	// page offsets applied) up front and re-attach them afterwards.
	var combined []pdfcpu.Bookmark
	if fixBookmarkOffsets {
		offset := 0
		for i, in := range inputs {
			n, err := api.PageCount(bytes.NewReader(in), newConf())
			if err != nil {
				return nil, fmt.Errorf("pdfops: merge: input %d: page count: %w", i, err)
			}
			bms, err := api.Bookmarks(bytes.NewReader(in), newConf())
			if err != nil {
				return nil, fmt.Errorf("pdfops: merge: input %d: read bookmarks: %w", i, err)
			}
			combined = append(combined, offsetBookmarks(bms, offset)...)
			offset += n
		}
	}

	readers := make([]io.ReadSeeker, len(inputs))
	for i, in := range inputs {
		readers[i] = bytes.NewReader(in)
	}
	var buf bytes.Buffer
	if err := api.MergeRaw(readers, &buf, false, newConf()); err != nil {
		return nil, fmt.Errorf("pdfops: merge: %w", err)
	}
	merged := buf.Bytes()

	if len(combined) > 0 {
		merged, err := replaceOutline(merged, combined)
		if err != nil {
			return nil, fmt.Errorf("pdfops: merge: reattach bookmarks: %w", err)
		}
		return merged, nil
	}
	return merged, nil
}

// replaceOutline rewrites pdf with bms as its complete outline tree,
// using direct /Dest arrays. pdfcpu's AddBookmarks keys named
// destinations by title, which breaks on duplicates and merged
// name trees, so the outline dictionary is built by hand.
func replaceOutline(pdf []byte, bms []pdfcpu.Bookmark) ([]byte, error) {
	ctx, err := api.ReadAndValidate(bytes.NewReader(pdf), newConf())
	if err != nil {
		return nil, fmt.Errorf("read: %w", err)
	}

	rootDict, err := ctx.Catalog()
	if err != nil {
		return nil, fmt.Errorf("catalog: %w", err)
	}

	outlinesDict := types.Dict(map[string]types.Object{"Type": types.Name("Outlines")})
	outlinesIR, err := ctx.IndRefForNewObject(outlinesDict)
	if err != nil {
		return nil, fmt.Errorf("create outlines dict: %w", err)
	}

	first, last, count, err := createOutlineItems(ctx, bms, outlinesIR)
	if err != nil {
		return nil, err
	}
	outlinesDict["First"] = *first
	outlinesDict["Last"] = *last
	outlinesDict["Count"] = types.Integer(count)
	rootDict["Outlines"] = *outlinesIR

	var buf bytes.Buffer
	if err := api.WriteContext(ctx, &buf); err != nil {
		return nil, fmt.Errorf("write: %w", err)
	}
	return buf.Bytes(), nil
}

// createOutlineItems writes bms as a linked list of outline item dicts
// under parent, recursing into kids. All items are written open.
// Returns first/last item refs and the visible descendant count.
func createOutlineItems(ctx *model.Context, bms []pdfcpu.Bookmark, parent *types.IndirectRef) (*types.IndirectRef, *types.IndirectRef, int, error) {
	var (
		first, prevIR *types.IndirectRef
		prevD         types.Dict
		count         int
	)

	for _, bm := range bms {
		_, pageIR, _, err := ctx.PageDict(bm.PageFrom, false)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("bookmark %q page %d: %w", bm.Title, bm.PageFrom, err)
		}
		title, err := types.EscapedUTF16String(bm.Title)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("bookmark %q: encode title: %w", bm.Title, err)
		}

		d := types.Dict(map[string]types.Object{
			"Title":  types.StringLiteral(*title),
			"Parent": *parent,
			"Dest":   types.Array{*pageIR, types.Name("Fit")},
		})
		ir, err := ctx.IndRefForNewObject(d)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("bookmark %q: create outline item: %w", bm.Title, err)
		}
		count++

		if len(bm.Kids) > 0 {
			kidFirst, kidLast, kidCount, err := createOutlineItems(ctx, bm.Kids, ir)
			if err != nil {
				return nil, nil, 0, err
			}
			d["First"] = *kidFirst
			d["Last"] = *kidLast
			d["Count"] = types.Integer(kidCount)
			count += kidCount
		}

		if first == nil {
			first = ir
		}
		if prevIR != nil {
			d["Prev"] = *prevIR
			prevD["Next"] = *ir
		}
		prevIR, prevD = ir, d
	}
	return first, prevIR, count, nil
}

// offsetBookmarks returns a deep copy of bms with every page destination
// shifted by off pages, ready for re-insertion into a merged document.
func offsetBookmarks(bms []pdfcpu.Bookmark, off int) []pdfcpu.Bookmark {
	out := make([]pdfcpu.Bookmark, len(bms))
	for i, b := range bms {
		b.PageFrom += off
		if b.PageThru > 0 {
			b.PageThru += off
		}
		b.Parent = nil
		b.Kids = offsetBookmarks(b.Kids, off)
		out[i] = b
	}
	return out
}

// ReadOutline returns the PDF's outline (bookmark tree) flattened
// depth-first, as produced by CDP generateDocumentOutline.
func ReadOutline(pdf []byte) ([]OutlineEntry, error) {
	bms, err := api.Bookmarks(bytes.NewReader(pdf), newConf())
	if err != nil {
		return nil, fmt.Errorf("pdfops: read outline: %w", err)
	}
	var out []OutlineEntry
	flattenBookmarks(bms, 1, &out)
	return out, nil
}

// flattenBookmarks appends bms and their kids to out depth-first,
// tagging each entry with its 1-based nesting level.
func flattenBookmarks(bms []pdfcpu.Bookmark, level int, out *[]OutlineEntry) {
	for _, b := range bms {
		*out = append(*out, OutlineEntry{Title: b.Title, Page: b.PageFrom, Level: level})
		flattenBookmarks(b.Kids, level+1, out)
	}
}

// SetMetadata sets the document title (--title) and producer on the PDF
// and returns the updated bytes. Empty values leave the field untouched.
func SetMetadata(pdf []byte, title, producer string) ([]byte, error) {
	if title == "" && producer == "" {
		return pdf, nil
	}
	// AddProperties writes Title into the Info dict; the (possibly
	// empty) rewrite also stamps a fresh /Producer literal to patch.
	props := map[string]string{}
	if title != "" {
		props["Title"] = title
	}
	var buf bytes.Buffer
	if err := api.AddProperties(bytes.NewReader(pdf), &buf, props, newConf()); err != nil {
		return nil, fmt.Errorf("pdfops: set metadata: %w", err)
	}
	out := buf.Bytes()
	if producer != "" {
		out = patchProducer(out, producer)
	}
	return out, nil
}

// patchProducer overwrites every /Producer string literal in-place with
// producer, space-padded (or truncated) to the literal's byte length so
// xref offsets stay valid. pdfcpu stamps its own producer on each write
// and offers no API to override it.
func patchProducer(pdf []byte, producer string) []byte {
	repl := sanitizeLiteral(producer)
	for i := 0; i+len("/Producer") < len(pdf); {
		j := bytes.Index(pdf[i:], []byte("/Producer"))
		if j < 0 {
			break
		}
		i += j + len("/Producer")
		k := i
		for k < len(pdf) && (pdf[k] == ' ' || pdf[k] == '\r' || pdf[k] == '\n' || pdf[k] == '\t') {
			k++
		}
		if k >= len(pdf) || pdf[k] != '(' {
			continue
		}
		end, ok := literalEnd(pdf, k)
		if !ok {
			continue
		}
		content := pdf[k+1 : end]
		v := repl
		if len(v) > len(content) {
			v = v[:len(content)]
		}
		copy(content, v)
		for x := len(v); x < len(content); x++ {
			content[x] = ' '
		}
		i = end
	}
	return pdf
}

// literalEnd returns the index of the ')' closing the PDF string literal
// opening at pdf[start] == '(', honoring escapes and nested parens.
func literalEnd(pdf []byte, start int) (int, bool) {
	depth := 0
	for k := start; k < len(pdf); k++ {
		switch pdf[k] {
		case '\\':
			k++
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return k, true
			}
		}
	}
	return 0, false
}

// sanitizeLiteral maps s to bytes safe inside a PDF string literal
// without escaping: printable ASCII with delimiters replaced by '-'.
func sanitizeLiteral(s string) []byte {
	out := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7e || c == '(' || c == ')' || c == '\\' {
			c = '-'
		}
		out[i] = c
	}
	return out
}

// Encrypt encrypts the PDF with the given owner and user passwords
// (either may be empty) and returns the encrypted bytes. Uses AES-256
// where wkhtmltopdf used RC4; callers should warn about the upgrade.
func Encrypt(pdf []byte, ownerPassword, userPassword string) ([]byte, error) {
	if ownerPassword == "" && userPassword == "" {
		return pdf, nil
	}
	conf := newConf()
	conf.OwnerPW = ownerPassword
	conf.UserPW = userPassword
	var buf bytes.Buffer
	if err := api.Encrypt(bytes.NewReader(pdf), &buf, conf); err != nil {
		return nil, fmt.Errorf("pdfops: encrypt: %w", err)
	}
	return buf.Bytes(), nil
}

// PageCount returns the number of pages in the PDF.
func PageCount(pdf []byte) (int, error) {
	n, err := api.PageCount(bytes.NewReader(pdf), newConf())
	if err != nil {
		return 0, fmt.Errorf("pdfops: page count: %w", err)
	}
	return n, nil
}

// OverlayFullPage stamps each stamp PDF onto its content page at 1:1
// scale, centered, no rotation — reproducing wkhtmltopdf's header/footer
// compositing, where the header/footer is a full page rendered by the
// same engine and laid over the content. stampByPage maps a 1-based
// content page number to the single-page stamp PDF to overlay; pages
// absent from the map are left unchanged. The stamp pages must share the
// content's paper size, and be transparent outside their drawn content
// so the body shows through.
func OverlayFullPage(content []byte, stampByPage map[int][]byte) ([]byte, error) {
	if len(stampByPage) == 0 {
		return content, nil
	}
	wms := make(map[int]*model.Watermark, len(stampByPage))
	for pageNr, stamp := range stampByPage {
		// onTop=true stamps over the content (a footer/header layer);
		// scale:1 abs keeps the stamp page at its native size.
		wm, err := api.PDFWatermarkForReadSeeker(bytes.NewReader(stamp), 1,
			"scale:1 abs, pos:c, rot:0", true, false, types.POINTS)
		if err != nil {
			return nil, fmt.Errorf("pdfops: overlay: page %d: %w", pageNr, err)
		}
		wms[pageNr] = wm
	}
	var buf bytes.Buffer
	if err := api.AddWatermarksMap(bytes.NewReader(content), &buf, wms, newConf()); err != nil {
		return nil, fmt.Errorf("pdfops: overlay: %w", err)
	}
	return buf.Bytes(), nil
}
