package pdfops

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
)

// makePDF hand-writes a minimal valid PDF with n empty A4 pages, giving
// tests deterministic fixtures without external files.
func makePDF(t *testing.T, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	var offsets []int
	writeObj := func(s string) {
		offsets = append(offsets, buf.Len())
		buf.WriteString(s)
	}

	buf.WriteString("%PDF-1.4\n")
	kids := make([]string, n)
	for i := range kids {
		kids[i] = fmt.Sprintf("%d 0 R", 3+i)
	}
	writeObj("1 0 obj\n<</Type/Catalog/Pages 2 0 R>>\nendobj\n")
	writeObj(fmt.Sprintf("2 0 obj\n<</Type/Pages/Kids[%s]/Count %d>>\nendobj\n", strings.Join(kids, " "), n))
	for i := 0; i < n; i++ {
		writeObj(fmt.Sprintf("%d 0 obj\n<</Type/Page/Parent 2 0 R/MediaBox[0 0 595 842]>>\nendobj\n", 3+i))
	}

	xref := buf.Len()
	buf.WriteString(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1))
	for _, off := range offsets {
		buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	buf.WriteString(fmt.Sprintf("trailer\n<</Size %d/Root 1 0 R>>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, xref))
	return buf.Bytes()
}

// withBookmarks returns pdf with the given bookmark tree attached.
func withBookmarks(t *testing.T, pdf []byte, bms []pdfcpu.Bookmark) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := api.AddBookmarks(bytes.NewReader(pdf), &out, bms, true, newConf()); err != nil {
		t.Fatalf("add bookmarks: %v", err)
	}
	return out.Bytes()
}

func TestPageCount(t *testing.T) {
	n, err := PageCount(makePDF(t, 3))
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 3 {
		t.Fatalf("PageCount = %d, want 3", n)
	}
}

func TestMergePageCounts(t *testing.T) {
	merged, err := Merge([][]byte{makePDF(t, 2), makePDF(t, 3), makePDF(t, 1)}, false)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	n, err := PageCount(merged)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	if n != 6 {
		t.Fatalf("merged page count = %d, want 6", n)
	}
}

func TestMergeSingleInputPassthrough(t *testing.T) {
	in := makePDF(t, 2)
	out, err := Merge([][]byte{in}, true)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if !bytes.Equal(in, out) {
		t.Fatal("single-input merge must return input unchanged")
	}
}

func TestMergeNoInputs(t *testing.T) {
	if _, err := Merge(nil, false); err == nil {
		t.Fatal("Merge(nil) must error")
	}
}

func TestOutlineRoundTrip(t *testing.T) {
	pdf := withBookmarks(t, makePDF(t, 4), []pdfcpu.Bookmark{
		{Title: "One", PageFrom: 1, Kids: []pdfcpu.Bookmark{
			{Title: "One.A", PageFrom: 2},
			{Title: "One.B", PageFrom: 3},
		}},
		{Title: "Two", PageFrom: 4},
	})

	got, err := ReadOutline(pdf)
	if err != nil {
		t.Fatalf("ReadOutline: %v", err)
	}
	want := []OutlineEntry{
		{Title: "One", Page: 1, Level: 1},
		{Title: "One.A", Page: 2, Level: 2},
		{Title: "One.B", Page: 3, Level: 2},
		{Title: "Two", Page: 4, Level: 1},
	}
	assertOutline(t, got, want)
}

func TestReadOutlineEmpty(t *testing.T) {
	got, err := ReadOutline(makePDF(t, 1))
	if err != nil {
		t.Fatalf("ReadOutline: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("ReadOutline = %v, want empty", got)
	}
}

func TestMergeOffsetsBookmarks(t *testing.T) {
	in1 := withBookmarks(t, makePDF(t, 2), []pdfcpu.Bookmark{
		{Title: "A", PageFrom: 1, Kids: []pdfcpu.Bookmark{{Title: "A.1", PageFrom: 2}}},
	})
	in2 := makePDF(t, 3) // no bookmarks, still shifts what follows
	in3 := withBookmarks(t, makePDF(t, 2), []pdfcpu.Bookmark{
		{Title: "B", PageFrom: 1},
		{Title: "C", PageFrom: 2},
	})

	merged, err := Merge([][]byte{in1, in2, in3}, true)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if n, err := PageCount(merged); err != nil || n != 7 {
		t.Fatalf("merged page count = %d (%v), want 7", n, err)
	}

	got, err := ReadOutline(merged)
	if err != nil {
		t.Fatalf("ReadOutline: %v", err)
	}
	want := []OutlineEntry{
		{Title: "A", Page: 1, Level: 1},
		{Title: "A.1", Page: 2, Level: 2},
		{Title: "B", Page: 6, Level: 1},
		{Title: "C", Page: 7, Level: 1},
	}
	assertOutline(t, got, want)
}

// TestMergeDuplicateTitles guards the hand-built outline writer:
// Chrome outlines repeat heading titles, which pdfcpu's title-keyed
// named destinations cannot represent.
func TestMergeDuplicateTitles(t *testing.T) {
	in1 := withBookmarks(t, makePDF(t, 2), []pdfcpu.Bookmark{
		{Title: "Intro", PageFrom: 1, Kids: []pdfcpu.Bookmark{{Title: "Details", PageFrom: 2}}},
	})
	in2 := withBookmarks(t, makePDF(t, 2), []pdfcpu.Bookmark{
		{Title: "Intro", PageFrom: 1, Kids: []pdfcpu.Bookmark{{Title: "Details", PageFrom: 2}}},
	})

	merged, err := Merge([][]byte{in1, in2}, true)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	got, err := ReadOutline(merged)
	if err != nil {
		t.Fatalf("ReadOutline: %v", err)
	}
	want := []OutlineEntry{
		{Title: "Intro", Page: 1, Level: 1},
		{Title: "Details", Page: 2, Level: 2},
		{Title: "Intro", Page: 3, Level: 1},
		{Title: "Details", Page: 4, Level: 2},
	}
	assertOutline(t, got, want)
}

func TestSetMetadata(t *testing.T) {
	out, err := SetMetadata(makePDF(t, 1), "My Title", "bilihtmltopdf")
	if err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}

	info, err := api.PDFInfo(bytes.NewReader(out), "", nil, false, newConf())
	if err != nil {
		t.Fatalf("PDFInfo: %v", err)
	}
	if info.Title != "My Title" {
		t.Errorf("Title = %q, want %q", info.Title, "My Title")
	}
	if !strings.HasPrefix(strings.TrimSpace(info.Producer), "bilihtmltopdf") {
		t.Errorf("Producer = %q, want prefix %q", info.Producer, "bilihtmltopdf")
	}
}

func TestSetMetadataNoop(t *testing.T) {
	in := makePDF(t, 1)
	out, err := SetMetadata(in, "", "")
	if err != nil {
		t.Fatalf("SetMetadata: %v", err)
	}
	if !bytes.Equal(in, out) {
		t.Fatal("empty metadata must return input unchanged")
	}
}

func TestEncrypt(t *testing.T) {
	enc, err := Encrypt(makePDF(t, 2), "owner-secret", "user-secret")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}

	if _, err := api.PageCount(bytes.NewReader(enc), newConf()); err == nil {
		t.Fatal("reading encrypted PDF without password must fail")
	}

	conf := newConf()
	conf.UserPW = "user-secret"
	n, err := api.PageCount(bytes.NewReader(enc), conf)
	if err != nil {
		t.Fatalf("PageCount with password: %v", err)
	}
	if n != 2 {
		t.Fatalf("page count = %d, want 2", n)
	}
}

func TestEncryptNoop(t *testing.T) {
	in := makePDF(t, 1)
	out, err := Encrypt(in, "", "")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if !bytes.Equal(in, out) {
		t.Fatal("empty passwords must return input unchanged")
	}
}

// TestChromeFixtureOutline exercises ReadOutline against a real
// Chrome-generated PDF when the spike fixture is present.
func TestChromeFixtureOutline(t *testing.T) {
	pdf, err := os.ReadFile("../../spike/out.pdf")
	if err != nil {
		t.Skip("spike fixture not present")
	}
	if _, err := ReadOutline(pdf); err != nil {
		t.Fatalf("ReadOutline(chrome pdf): %v", err)
	}
	if n, err := PageCount(pdf); err != nil || n < 1 {
		t.Fatalf("PageCount(chrome pdf) = %d (%v)", n, err)
	}
}

// assertOutline fails the test unless got matches want exactly.
func assertOutline(t *testing.T, got, want []OutlineEntry) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("outline = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("outline[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}
