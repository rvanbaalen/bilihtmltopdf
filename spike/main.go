// Spike: validate that chromedp + Chrome can produce a wkhtmltopdf-style PDF —
// header/footer templates with page numbers, document outline, modern CSS.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: spike <input.html> <output.pdf>")
	}
	in, err := filepath.Abs(os.Args[1])
	if err != nil {
		log.Fatal(err)
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", "new"),
	)
	allocCtx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer cancel()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancel = context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	header := `<div style="font-size:9px; width:100%; padding:0 1cm; display:flex; justify-content:space-between;">
		<span>bilihtmltopdf spike</span><span class="title"></span>
		<span>Page <span class="pageNumber"></span> of <span class="totalPages"></span></span></div>`
	footer := `<div style="font-size:9px; width:100%; text-align:center;">[page] equivalent: <span class="pageNumber"></span></div>`

	var pdf []byte
	err = chromedp.Run(ctx,
		chromedp.Navigate("file://"+in),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			pdf, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				WithPaperWidth(8.27).WithPaperHeight(11.69). // A4 in inches
				WithMarginTop(0.6).WithMarginBottom(0.6).
				WithDisplayHeaderFooter(true).
				WithHeaderTemplate(header).
				WithFooterTemplate(footer).
				WithGenerateDocumentOutline(true).
				Do(ctx)
			return err
		}),
	)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(os.Args[2], pdf, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("wrote %s (%d bytes)\n", os.Args[2], len(pdf))
}
