// Package args defines the shared option types mirroring wkhtmltopdf's
// CLI settings and parses argv into them. Field defaults follow the
// fork's src/lib/pdfsettings.cc constructors.
package args

// Orientation is the page orientation, matching wkhtmltopdf --orientation.
type Orientation string

// Orientation values accepted by --orientation (case-insensitive on parse).
const (
	Portrait  Orientation = "Portrait"
	Landscape Orientation = "Landscape"
)

// HeaderFooter holds the settings for one generated or HTML-file header or
// footer, mirroring wkhtmltopdf's HeaderFooter settings struct.
type HeaderFooter struct {
	// Left is the left-aligned text (--header-left / --footer-left).
	Left string
	// Center is the centered text (--header-center / --footer-center).
	Center string
	// Right is the right-aligned text (--header-right / --footer-right).
	Right string
	// FontName is the font family (--header-font-name). Default "Arial".
	FontName string
	// FontSize is the font size in points (--header-font-size). Default 12.
	FontSize int
	// Line draws a rule between header/footer and content (--header-line).
	Line bool
	// Spacing is the gap between header/footer and content in mm
	// (--header-spacing / --footer-spacing). Default 0.
	Spacing float64
	// HTMLPath is a user-supplied HTML file (--header-html / --footer-html);
	// when set it replaces the generated Left/Center/Right template.
	HTMLPath string
}

// NewHeaderFooter returns a HeaderFooter with wkhtmltopdf defaults
// (Arial 12pt, no line, no spacing, empty texts).
func NewHeaderFooter() HeaderFooter {
	return HeaderFooter{FontName: "Arial", FontSize: 12}
}

// GlobalOptions holds the once-per-invocation settings, mirroring
// wkhtmltopdf's PdfGlobal. Unsupported switches are kept so the
// parser can warn (or fail under Strict) with their real names.
type GlobalOptions struct {
	// Output is the output PDF path; "-" means stdout.
	Output string
	// PageSize is the named paper size (--page-size), e.g. "A4", "Letter".
	// Default "A4". Ignored when PageWidth/PageHeight are set.
	PageSize string
	// PageWidth is a custom page width as a CSS unit string (--page-width),
	// e.g. "210mm". Empty means: use PageSize.
	PageWidth string
	// PageHeight is a custom page height as a CSS unit string (--page-height).
	PageHeight string
	// Orientation is Portrait or Landscape (--orientation). Default Portrait.
	Orientation Orientation
	// MarginTop is the top page margin as a CSS unit string (--margin-top).
	// wkhtmltopdf default 10mm.
	MarginTop string
	// MarginBottom is the bottom page margin (--margin-bottom). Default 10mm.
	MarginBottom string
	// MarginLeft is the left page margin (--margin-left). Default 10mm.
	MarginLeft string
	// MarginRight is the right page margin (--margin-right). Default 10mm.
	MarginRight string
	// Title is the PDF document title (--title); the first document's
	// title is used when empty.
	Title string
	// Copies is the number of copies printed into the PDF (--copies).
	// Default 1. Values > 1 are a warn-only no-op under Chromium.
	Copies int
	// Collate mirrors --collate/--no-collate. Default true. Warn-only no-op.
	Collate bool
	// Outline embeds a PDF outline/bookmark tree (--outline/--no-outline).
	// Default true.
	Outline bool
	// OutlineDepth caps the outline depth (--outline-depth). Default 4.
	OutlineDepth int
	// OwnerPassword encrypts the output with this owner password
	// (--owner-password, bilihtmltopdf extension via pdfcpu).
	OwnerPassword string
	// UserPassword encrypts the output with this user password
	// (--user-password, bilihtmltopdf extension via pdfcpu).
	UserPassword string
	// Quiet suppresses progress output (--quiet / -q).
	Quiet bool
	// Strict turns unsupported-flag warnings into fatal errors (--strict,
	// bilihtmltopdf extension).
	Strict bool
	// Grayscale mirrors --grayscale. Warn-only no-op.
	Grayscale bool
	// LowQuality mirrors --lowquality. Warn-only no-op.
	LowQuality bool
	// DPI mirrors --dpi. Default 96. Warn-only no-op.
	DPI int
	// PageOffset is the starting page number (--page-offset). Default 0.
	PageOffset int
	// CookieJar is the cookie jar file path (--cookie-jar). Warn-only no-op.
	CookieJar string
}

// NewGlobalOptions returns GlobalOptions with wkhtmltopdf defaults:
// A4 portrait, 10mm margins, outline on at depth 4, 1 collated copy.
func NewGlobalOptions() GlobalOptions {
	return GlobalOptions{
		PageSize:     "A4",
		Orientation:  Portrait,
		MarginTop:    "10mm",
		MarginBottom: "10mm",
		MarginLeft:   "10mm",
		MarginRight:  "10mm",
		Copies:       1,
		Collate:      true,
		Outline:      true,
		OutlineDepth: 4,
		DPI:          96,
	}
}

// TOCOptions holds the settings of a toc object, mirroring
// wkhtmltopdf's TableOfContent settings struct.
type TOCOptions struct {
	// HeaderText is the TOC caption (--toc-header-text).
	// Default "Table of Contents".
	HeaderText string
	// UseDottedLines fills entries with dot leaders
	// (--disable-dotted-lines clears it). Default true.
	UseDottedLines bool
	// ForwardLinks links TOC entries to their sections
	// (--disable-toc-links clears it). Default true.
	ForwardLinks bool
	// BackLinks links section headers back to the TOC
	// (--enable-toc-back-links). Default false. Warn-only no-op.
	BackLinks bool
	// Indentation is the per-level indent as a CSS unit string
	// (--toc-level-indentation). Default "1em".
	Indentation string
	// FontScale scales the font per heading level
	// (--toc-text-size-shrink). Default 0.8.
	FontScale float64
	// XSLStyleSheet is a user XSL stylesheet applied to the generated
	// outline XML (--xsl-style-sheet). Empty means built-in style.
	XSLStyleSheet string
}

// NewTOCOptions returns TOCOptions with wkhtmltopdf defaults
// (dotted lines, forward links, 1em indent, 0.8 font scale).
func NewTOCOptions() TOCOptions {
	return TOCOptions{
		HeaderText:     "Table of Contents",
		UseDottedLines: true,
		ForwardLinks:   true,
		Indentation:    "1em",
		FontScale:      0.8,
	}
}

// PageOptions holds the per-object settings, mirroring wkhtmltopdf's
// PdfObject plus its Web and LoadPage sub-structs.
type PageOptions struct {
	// Input is the source to render: URL, file path, or "-" for stdin.
	Input string
	// Zoom is the zoom factor mapped to CDP scale (--zoom). Default 1.0.
	Zoom float64
	// Background prints CSS backgrounds (--background/--no-background).
	// Default true.
	Background bool
	// LoadImages loads and prints images (--images/--no-images).
	// Default true.
	LoadImages bool
	// EnableJavascript allows pages to run JS
	// (--enable-javascript / --disable-javascript). Default true.
	EnableJavascript bool
	// JavascriptDelay waits this many ms for JS to finish
	// (--javascript-delay). Default 200.
	JavascriptDelay int
	// WindowStatus waits until window.status equals this string before
	// rendering (--window-status).
	WindowStatus string
	// RunScripts are JS snippets evaluated after load (--run-script,
	// repeatable), in order.
	RunScripts []string
	// PrintMediaType emulates print media instead of screen
	// (--print-media-type/--no-print-media-type). Default false.
	PrintMediaType bool
	// UserStyleSheet is a stylesheet injected into every page
	// (--user-style-sheet).
	UserStyleSheet string
	// DefaultEncoding is the fallback text encoding (--encoding).
	// Warn-only no-op: Chromium offers no fallback-encoding override.
	DefaultEncoding string
	// Cookies are additional cookies (--cookie name value, repeatable);
	// values are URL-encoded per wkhtmltopdf convention.
	Cookies map[string]string
	// CustomHeaders are extra HTTP headers (--custom-header, repeatable).
	CustomHeaders map[string]string
	// CustomHeaderPropagation repeats CustomHeaders on every resource
	// request (--custom-header-propagation). Default false: headers are
	// sent on main-document requests only, matching wkhtmltopdf.
	CustomHeaderPropagation bool
	// Username is the HTTP basic-auth username (--username).
	Username string
	// Password is the HTTP basic-auth password (--password).
	Password string
	// SSLKeyPath mirrors --ssl-key-path. Warn-only no-op.
	SSLKeyPath string
	// SSLKeyPassword mirrors --ssl-key-password. Warn-only no-op.
	SSLKeyPassword string
	// SSLCrtPath mirrors --ssl-crt-path. Warn-only no-op.
	SSLCrtPath string
	// ViewportSize mirrors --viewport-size, e.g. "1280x1024".
	ViewportSize string
	// IncludeInOutline includes this object in outline and TOC
	// (--include-in-outline/--exclude-from-outline). Default true;
	// cover objects default false.
	IncludeInOutline bool
	// EnableLocalFileAccess lets local files read other local files
	// (--enable-local-file-access/--disable-local-file-access).
	// Default false: file:// subresource loads are blocked, matching
	// wkhtmltopdf 0.12.6; enforced via CDP Fetch interception.
	EnableLocalFileAccess bool
	// AllowedPaths are files or directories exempt from local-file
	// blocking (--allow, repeatable).
	AllowedPaths []string
	// Replacements substitute [name] with value in headers and footers
	// (--replace name value, repeatable).
	Replacements map[string]string
	// Header is this object's header configuration.
	Header HeaderFooter
	// Footer is this object's footer configuration.
	Footer HeaderFooter
}

// NewPageOptions returns PageOptions with wkhtmltopdf defaults: zoom 1.0,
// backgrounds/images/JS on, 200ms JS delay, included in outline.
func NewPageOptions() PageOptions {
	return PageOptions{
		Zoom:             1.0,
		Background:       true,
		LoadImages:       true,
		EnableJavascript: true,
		JavascriptDelay:  200,
		Cookies:          map[string]string{},
		CustomHeaders:    map[string]string{},
		Replacements:     map[string]string{},
		IncludeInOutline: true,
		Header:           NewHeaderFooter(),
		Footer:           NewHeaderFooter(),
	}
}

// Object is one input object on the command line: a page, cover, or toc.
// Concrete types are PageObject, CoverObject, and TOCObject.
type Object interface {
	// Kind returns the object's CLI keyword: "page", "cover", or "toc".
	Kind() string
	// Options returns the object's page-level settings.
	Options() *PageOptions
}

// PageObject is a regular content page ("page <input>" or a bare input).
type PageObject struct {
	// Page holds the object's settings.
	Page PageOptions
}

// Kind returns "page".
func (o *PageObject) Kind() string { return "page" }

// Options returns the object's page-level settings.
func (o *PageObject) Options() *PageOptions { return &o.Page }

// CoverObject is a cover page ("cover <input>"): rendered like a page but
// excluded from outline, TOC, and page numbering by default.
type CoverObject struct {
	// Page holds the object's settings.
	Page PageOptions
}

// Kind returns "cover".
func (o *CoverObject) Kind() string { return "cover" }

// Options returns the object's page-level settings.
func (o *CoverObject) Options() *PageOptions { return &o.Page }

// TOCObject is a generated table of contents ("toc").
type TOCObject struct {
	// Page holds header/footer and rendering settings for the TOC pages.
	Page PageOptions
	// TOC holds the TOC-specific settings.
	TOC TOCOptions
}

// Kind returns "toc".
func (o *TOCObject) Kind() string { return "toc" }

// Options returns the object's page-level settings.
func (o *TOCObject) Options() *PageOptions { return &o.Page }

// Command is a fully parsed invocation: global settings plus the ordered
// input objects, with any compatibility warnings collected during parse.
type Command struct {
	// Global holds the once-per-invocation settings.
	Global GlobalOptions
	// Objects are the input objects in command-line order.
	Objects []Object
	// Warnings are wkhtmltopdf-compat notices (e.g. unsupported flags)
	// to print to stderr; under Strict they become fatal instead.
	Warnings []string
}

// Warn records a compatibility warning on the command.
func (c *Command) Warn(msg string) {
	c.Warnings = append(c.Warnings, msg)
}
