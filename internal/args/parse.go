package args

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Version is the release version, injected at build time via
// -ldflags "-X .../internal/args.Version=x.y.z"; "dev" for local builds.
var Version = "dev"

// versionString is printed for --version / -V.
var versionString = "bilihtmltopdf " + Version

// errUsage is returned when the input/output positionals are missing.
var errUsage = fmt.Errorf("you need to specify at least one input file, and exactly one output file")

// ExitText is returned as the error from Parse when the invocation only
// requests informational output (--help, --extended-help, --version).
// Callers should print Text to stdout and exit 0.
type ExitText struct {
	// Text is the full text to print.
	Text string
}

// Error returns the first line of the informational text.
func (e *ExitText) Error() string {
	text, _, _ := strings.Cut(e.Text, "\n")
	return text
}

// item is one lexed argv element: either a flag with its values or a
// positional token.
type item struct {
	name string   // canonical long flag name; empty for a positional
	vals []string // consumed flag arguments
	pos  string   // positional token when name is empty
}

// parser accumulates parse state across the argv walk.
type parser struct {
	cmd      *Command
	defPage  PageOptions // page-option defaults set before the first object
	defTOC   TOCOptions  // toc-option defaults set before the first toc object
	cur      Object      // object receiving page options; nil in global mode
	awaiting string      // "page" or "cover" while the keyword awaits its input
}

// pg returns the PageOptions that page-scoped flags currently target.
func (p *parser) pg() *PageOptions {
	if p.cur != nil {
		return p.cur.Options()
	}
	return &p.defPage
}

// tc returns the TOCOptions that toc-scoped flags currently target, or nil
// (with a warning) when the current object is not a toc.
func (p *parser) tc(name string) *TOCOptions {
	if p.cur == nil {
		return &p.defTOC
	}
	if t, ok := p.cur.(*TOCObject); ok {
		return &t.TOC
	}
	p.cmd.Warn("--" + name + " ignored: only valid for toc objects")
	return nil
}

// unsupported records the standard warn-only notice for a flag.
func (p *parser) unsupported(name string) {
	p.cmd.Warn("--" + name + " is not supported and will be ignored")
}

// newPage clones the current page-option defaults, deep-copying maps and
// slices so objects never share storage.
func (p *parser) newPage() PageOptions {
	np := p.defPage
	np.Cookies = maps.Clone(p.defPage.Cookies)
	np.CustomHeaders = maps.Clone(p.defPage.CustomHeaders)
	np.Replacements = maps.Clone(p.defPage.Replacements)
	np.RunScripts = slices.Clone(p.defPage.RunScripts)
	np.AllowedPaths = slices.Clone(p.defPage.AllowedPaths)
	return np
}

// applyFunc applies one flag occurrence to the parser state.
type applyFunc func(p *parser, name string, vals []string) error

// flagSpec describes one long option: how many value arguments it consumes
// and how to apply it.
type flagSpec struct {
	nargs int
	apply applyFunc
}

// atoi parses an integer flag value with a flag-named error.
func atoi(name, s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q for --%s: integer expected", s, name)
	}
	return n, nil
}

// atof parses a float flag value with a flag-named error.
func atof(name, s string) (float64, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid value %q for --%s: number expected", s, name)
	}
	return f, nil
}

// gStr builds a spec storing one value into GlobalOptions.
func gStr(set func(*GlobalOptions, string)) flagSpec {
	return flagSpec{nargs: 1, apply: func(p *parser, _ string, v []string) error {
		set(&p.cmd.Global, v[0])
		return nil
	}}
}

// gBool builds a zero-arg spec storing a fixed bool into GlobalOptions.
func gBool(set func(*GlobalOptions, bool), val bool) flagSpec {
	return flagSpec{apply: func(p *parser, _ string, _ []string) error {
		set(&p.cmd.Global, val)
		return nil
	}}
}

// gInt builds a spec storing one integer value into GlobalOptions.
func gInt(set func(*GlobalOptions, int)) flagSpec {
	return flagSpec{nargs: 1, apply: func(p *parser, name string, v []string) error {
		n, err := atoi(name, v[0])
		if err != nil {
			return err
		}
		set(&p.cmd.Global, n)
		return nil
	}}
}

// pgStr builds a spec storing one value into the current PageOptions.
func pgStr(set func(*PageOptions, string)) flagSpec {
	return flagSpec{nargs: 1, apply: func(p *parser, _ string, v []string) error {
		set(p.pg(), v[0])
		return nil
	}}
}

// pgBool builds a zero-arg spec storing a fixed bool into the current PageOptions.
func pgBool(set func(*PageOptions, bool), val bool) flagSpec {
	return flagSpec{apply: func(p *parser, _ string, _ []string) error {
		set(p.pg(), val)
		return nil
	}}
}

// pgInt builds a spec storing one integer value into the current PageOptions.
func pgInt(set func(*PageOptions, int)) flagSpec {
	return flagSpec{nargs: 1, apply: func(p *parser, name string, v []string) error {
		n, err := atoi(name, v[0])
		if err != nil {
			return err
		}
		set(p.pg(), n)
		return nil
	}}
}

// pgFloat builds a spec storing one float value into the current PageOptions.
func pgFloat(set func(*PageOptions, float64)) flagSpec {
	return flagSpec{nargs: 1, apply: func(p *parser, name string, v []string) error {
		f, err := atof(name, v[0])
		if err != nil {
			return err
		}
		set(p.pg(), f)
		return nil
	}}
}

// pgMap builds a two-arg spec storing name/value into a PageOptions map.
func pgMap(get func(*PageOptions) map[string]string) flagSpec {
	return flagSpec{nargs: 2, apply: func(p *parser, _ string, v []string) error {
		get(p.pg())[v[0]] = v[1]
		return nil
	}}
}

// tocStr builds a spec storing one value into the current TOCOptions.
func tocStr(set func(*TOCOptions, string)) flagSpec {
	return flagSpec{nargs: 1, apply: func(p *parser, name string, v []string) error {
		if t := p.tc(name); t != nil {
			set(t, v[0])
		}
		return nil
	}}
}

// tocBool builds a zero-arg spec storing a fixed bool into the current TOCOptions.
func tocBool(set func(*TOCOptions, bool), val bool) flagSpec {
	return flagSpec{apply: func(p *parser, name string, _ []string) error {
		if t := p.tc(name); t != nil {
			set(t, val)
		}
		return nil
	}}
}

// warnOnly builds a spec that consumes nargs values and only records the
// standard unsupported-flag warning.
func warnOnly(nargs int) flagSpec {
	return flagSpec{nargs: nargs, apply: func(p *parser, name string, _ []string) error {
		p.unsupported(name)
		return nil
	}}
}

// shortFlags maps wkhtmltopdf short options to their long names.
var shortFlags = map[byte]string{
	'h': "help",
	'H': "extended-help",
	'V': "version",
	'q': "quiet",
	'O': "orientation",
	's': "page-size",
	'g': "grayscale",
	'l': "lowquality",
	'B': "margin-bottom",
	'L': "margin-left",
	'R': "margin-right",
	'T': "margin-top",
	'd': "dpi",
	'n': "disable-javascript",
	'p': "proxy",
}

// specs is the full wkhtmltopdf pdf-mode option table (plus -ng extensions).
// Flags absent here are unknown and warn-only.
var specs = map[string]flagSpec{
	// ── global, supported ──
	"quiet":  gBool(func(g *GlobalOptions, v bool) { g.Quiet = v }, true),
	"strict": gBool(func(g *GlobalOptions, v bool) { g.Strict = v }, true),
	"title":  gStr(func(g *GlobalOptions, v string) { g.Title = v }),
	"orientation": {nargs: 1, apply: func(p *parser, _ string, v []string) error {
		switch {
		case strings.EqualFold(v[0], string(Portrait)):
			p.cmd.Global.Orientation = Portrait
		case strings.EqualFold(v[0], string(Landscape)):
			p.cmd.Global.Orientation = Landscape
		default:
			return fmt.Errorf("invalid orientation %q, must be Portrait or Landscape", v[0])
		}
		return nil
	}},
	"page-size":      gStr(func(g *GlobalOptions, v string) { g.PageSize = v }),
	"page-width":     gStr(func(g *GlobalOptions, v string) { g.PageWidth = v }),
	"page-height":    gStr(func(g *GlobalOptions, v string) { g.PageHeight = v }),
	"margin-top":     gStr(func(g *GlobalOptions, v string) { g.MarginTop = v }),
	"margin-bottom":  gStr(func(g *GlobalOptions, v string) { g.MarginBottom = v }),
	"margin-left":    gStr(func(g *GlobalOptions, v string) { g.MarginLeft = v }),
	"margin-right":   gStr(func(g *GlobalOptions, v string) { g.MarginRight = v }),
	"outline":        gBool(func(g *GlobalOptions, v bool) { g.Outline = v }, true),
	"no-outline":     gBool(func(g *GlobalOptions, v bool) { g.Outline = v }, false),
	"outline-depth":  gInt(func(g *GlobalOptions, v int) { g.OutlineDepth = v }),
	"page-offset":    gInt(func(g *GlobalOptions, v int) { g.PageOffset = v }),
	"owner-password": gStr(func(g *GlobalOptions, v string) { g.OwnerPassword = v }),
	"user-password":  gStr(func(g *GlobalOptions, v string) { g.UserPassword = v }),
	"collate":        gBool(func(g *GlobalOptions, v bool) { g.Collate = v }, true),
	"log-level": {nargs: 1, apply: func(p *parser, _ string, v []string) error {
		// Only "none" has an equivalent; other levels are the default behavior.
		if strings.EqualFold(v[0], "none") {
			p.cmd.Global.Quiet = true
		}
		return nil
	}},

	// ── global, stored but warn-only no-ops ──
	"copies": {nargs: 1, apply: func(p *parser, name string, v []string) error {
		n, err := atoi(name, v[0])
		if err != nil {
			return err
		}
		p.cmd.Global.Copies = n
		if n > 1 {
			p.cmd.Warn("--copies > 1 is not supported; producing a single copy")
		}
		return nil
	}},
	"no-collate": {apply: func(p *parser, name string, _ []string) error {
		p.cmd.Global.Collate = false
		p.unsupported(name)
		return nil
	}},
	"dpi": {nargs: 1, apply: func(p *parser, name string, v []string) error {
		n, err := atoi(name, v[0])
		if err != nil {
			return err
		}
		p.cmd.Global.DPI = n
		p.unsupported(name)
		return nil
	}},
	"grayscale": {apply: func(p *parser, name string, _ []string) error {
		p.cmd.Global.Grayscale = true
		p.unsupported(name)
		return nil
	}},
	"lowquality": {apply: func(p *parser, name string, _ []string) error {
		p.cmd.Global.LowQuality = true
		p.unsupported(name)
		return nil
	}},
	"cookie-jar": {nargs: 1, apply: func(p *parser, name string, v []string) error {
		p.cmd.Global.CookieJar = v[0]
		p.unsupported(name)
		return nil
	}},

	// ── global, warn-only no-ops ──
	"read-args-from-stdin": warnOnly(0),
	"use-xserver":          warnOnly(0),
	"no-pdf-compression":   warnOnly(0),
	"image-quality":        warnOnly(1),
	"image-dpi":            warnOnly(1),
	"dump-outline":         warnOnly(1),

	// ── page, supported ──
	"background":                   pgBool(func(o *PageOptions, v bool) { o.Background = v }, true),
	"no-background":                pgBool(func(o *PageOptions, v bool) { o.Background = v }, false),
	"images":                       pgBool(func(o *PageOptions, v bool) { o.LoadImages = v }, true),
	"no-images":                    pgBool(func(o *PageOptions, v bool) { o.LoadImages = v }, false),
	"enable-javascript":            pgBool(func(o *PageOptions, v bool) { o.EnableJavascript = v }, true),
	"disable-javascript":           pgBool(func(o *PageOptions, v bool) { o.EnableJavascript = v }, false),
	"javascript-delay":             pgInt(func(o *PageOptions, v int) { o.JavascriptDelay = v }),
	"window-status":                pgStr(func(o *PageOptions, v string) { o.WindowStatus = v }),
	"print-media-type":             pgBool(func(o *PageOptions, v bool) { o.PrintMediaType = v }, true),
	"no-print-media-type":          pgBool(func(o *PageOptions, v bool) { o.PrintMediaType = v }, false),
	"user-style-sheet":             pgStr(func(o *PageOptions, v string) { o.UserStyleSheet = v }),
	"username":                     pgStr(func(o *PageOptions, v string) { o.Username = v }),
	"password":                     pgStr(func(o *PageOptions, v string) { o.Password = v }),
	"viewport-size":                pgStr(func(o *PageOptions, v string) { o.ViewportSize = v }),
	"zoom":                         pgFloat(func(o *PageOptions, v float64) { o.Zoom = v }),
	"cookie":                       pgMap(func(o *PageOptions) map[string]string { return o.Cookies }),
	"custom-header":                pgMap(func(o *PageOptions) map[string]string { return o.CustomHeaders }),
	"replace":                      pgMap(func(o *PageOptions) map[string]string { return o.Replacements }),
	"custom-header-propagation":    pgBool(func(o *PageOptions, v bool) { o.CustomHeaderPropagation = v }, true),
	"no-custom-header-propagation": pgBool(func(o *PageOptions, v bool) { o.CustomHeaderPropagation = v }, false),
	"include-in-outline":           pgBool(func(o *PageOptions, v bool) { o.IncludeInOutline = v }, true),
	"exclude-from-outline":         pgBool(func(o *PageOptions, v bool) { o.IncludeInOutline = v }, false),
	"enable-local-file-access":     pgBool(func(o *PageOptions, v bool) { o.EnableLocalFileAccess = v }, true),
	"disable-local-file-access":    pgBool(func(o *PageOptions, v bool) { o.EnableLocalFileAccess = v }, false),
	"run-script": {nargs: 1, apply: func(p *parser, _ string, v []string) error {
		o := p.pg()
		o.RunScripts = append(o.RunScripts, v[0])
		return nil
	}},

	// allow feeds the local-file-access allowlist enforced in internal/chrome.
	"allow": {nargs: 1, apply: func(p *parser, _ string, v []string) error {
		o := p.pg()
		o.AllowedPaths = append(o.AllowedPaths, v[0])
		return nil
	}},

	// ── page, stored but warn-only no-ops ──
	"encoding": {nargs: 1, apply: func(p *parser, name string, v []string) error {
		p.pg().DefaultEncoding = v[0]
		p.cmd.Warn("--" + name + " is not supported by the Chromium backend; pages without a declared charset are decoded as UTF-8")
		return nil
	}},
	"ssl-key-path": {nargs: 1, apply: func(p *parser, name string, v []string) error {
		p.pg().SSLKeyPath = v[0]
		p.unsupported(name)
		return nil
	}},
	"ssl-key-password": {nargs: 1, apply: func(p *parser, name string, v []string) error {
		p.pg().SSLKeyPassword = v[0]
		p.unsupported(name)
		return nil
	}},
	"ssl-crt-path": {nargs: 1, apply: func(p *parser, name string, v []string) error {
		p.pg().SSLCrtPath = v[0]
		p.unsupported(name)
		return nil
	}},

	// ── page, warn-only no-ops ──
	"enable-forms":              warnOnly(0),
	"disable-forms":             warnOnly(0),
	"enable-internal-links":     warnOnly(0),
	"disable-internal-links":    warnOnly(0),
	"enable-external-links":     warnOnly(0),
	"disable-external-links":    warnOnly(0),
	"keep-relative-links":       warnOnly(0),
	"resolve-relative-links":    warnOnly(0),
	"enable-smart-shrinking":    warnOnly(0),
	"disable-smart-shrinking":   warnOnly(0),
	"enable-plugins":            warnOnly(0),
	"disable-plugins":           warnOnly(0),
	"minimum-font-size":         warnOnly(1),
	"proxy":                     warnOnly(1),
	"proxy-hostname-lookup":     warnOnly(0),
	"bypass-proxy-for":          warnOnly(1),
	"load-error-handling":       warnOnly(1),
	"load-media-error-handling": warnOnly(1),
	"post":                      warnOnly(2),
	"post-file":                 warnOnly(2),
	"cache-dir":                 warnOnly(1),
	"debug-javascript":          warnOnly(0),
	"no-debug-javascript":       warnOnly(0),
	"stop-slow-scripts":         warnOnly(0),
	"no-stop-slow-scripts":      warnOnly(0),
	"checkbox-svg":              warnOnly(1),
	"checkbox-checked-svg":      warnOnly(1),
	"radiobutton-svg":           warnOnly(1),
	"radiobutton-checked-svg":   warnOnly(1),

	// ── header/footer ──
	"header-left":      pgStr(func(o *PageOptions, v string) { o.Header.Left = v }),
	"header-center":    pgStr(func(o *PageOptions, v string) { o.Header.Center = v }),
	"header-right":     pgStr(func(o *PageOptions, v string) { o.Header.Right = v }),
	"header-font-name": pgStr(func(o *PageOptions, v string) { o.Header.FontName = v }),
	"header-font-size": pgInt(func(o *PageOptions, v int) { o.Header.FontSize = v }),
	"header-line":      pgBool(func(o *PageOptions, v bool) { o.Header.Line = v }, true),
	"no-header-line":   pgBool(func(o *PageOptions, v bool) { o.Header.Line = v }, false),
	"header-html":      pgStr(func(o *PageOptions, v string) { o.Header.HTMLPath = v }),
	"footer-left":      pgStr(func(o *PageOptions, v string) { o.Footer.Left = v }),
	"footer-center":    pgStr(func(o *PageOptions, v string) { o.Footer.Center = v }),
	"footer-right":     pgStr(func(o *PageOptions, v string) { o.Footer.Right = v }),
	"footer-font-name": pgStr(func(o *PageOptions, v string) { o.Footer.FontName = v }),
	"footer-font-size": pgInt(func(o *PageOptions, v int) { o.Footer.FontSize = v }),
	"footer-line":      pgBool(func(o *PageOptions, v bool) { o.Footer.Line = v }, true),
	"no-footer-line":   pgBool(func(o *PageOptions, v bool) { o.Footer.Line = v }, false),
	"footer-html":      pgStr(func(o *PageOptions, v string) { o.Footer.HTMLPath = v }),
	"header-spacing":   pgFloat(func(o *PageOptions, v float64) { o.Header.Spacing = v }),
	"footer-spacing":   pgFloat(func(o *PageOptions, v float64) { o.Footer.Spacing = v }),
	"default-header": {apply: func(p *parser, _ string, _ []string) error {
		// Documented shorthand: --header-left '[webpage]'
		// --header-right '[page]/[toPage]' --margin-top 2cm --header-line.
		o := p.pg()
		o.Header.Left = "[webpage]"
		o.Header.Right = "[page]/[toPage]"
		o.Header.Line = true
		p.cmd.Global.MarginTop = "2cm"
		return nil
	}},

	// ── toc ──
	"toc-header-text":       tocStr(func(t *TOCOptions, v string) { t.HeaderText = v }),
	"toc-level-indentation": tocStr(func(t *TOCOptions, v string) { t.Indentation = v }),
	"xsl-style-sheet":       tocStr(func(t *TOCOptions, v string) { t.XSLStyleSheet = v }),
	"disable-dotted-lines":  tocBool(func(t *TOCOptions, v bool) { t.UseDottedLines = v }, false),
	"disable-toc-links":     tocBool(func(t *TOCOptions, v bool) { t.ForwardLinks = v }, false),
	"toc-text-size-shrink": {nargs: 1, apply: func(p *parser, name string, v []string) error {
		f, err := atof(name, v[0])
		if err != nil {
			return err
		}
		if t := p.tc(name); t != nil {
			t.FontScale = f
		}
		return nil
	}},
	"enable-toc-back-links": {apply: func(p *parser, name string, _ []string) error {
		if t := p.tc(name); t != nil {
			t.BackLinks = true
		}
		p.unsupported(name)
		return nil
	}},
	"disable-toc-back-links": {apply: func(p *parser, name string, _ []string) error {
		if t := p.tc(name); t != nil {
			t.BackLinks = false
		}
		return nil
	}},
}

// exitFlag returns an *ExitText error for the informational flags, nil
// otherwise. Like wkhtmltopdf, these print to stdout and exit 0.
func exitFlag(name string) error {
	switch name {
	case "help":
		return &ExitText{Text: helpText}
	case "extended-help", "manpage", "readme", "htmldoc":
		return &ExitText{Text: extendedHelpText}
	case "version":
		return &ExitText{Text: versionString + "\n"}
	case "license":
		return &ExitText{Text: licenseText}
	case "dump-default-toc-xsl":
		return &ExitText{Text: defaultTOCXSL}
	}
	return nil
}

// lex splits argv into flag and positional items, resolving short options
// and --flag=value forms and consuming each flag's value arguments.
func (p *parser) lex(argv []string) ([]item, error) {
	var items []item
	i := 0
	for i < len(argv) {
		t := argv[i]
		i++
		switch {
		case t == "-" || !strings.HasPrefix(t, "-"):
			items = append(items, item{pos: t})
		case strings.HasPrefix(t, "--"):
			body := t[2:]
			if body == "" {
				// "--" ends option parsing; the rest are positionals.
				for ; i < len(argv); i++ {
					items = append(items, item{pos: argv[i]})
				}
				continue
			}
			name, inline, hasInline := strings.Cut(body, "=")
			if err := exitFlag(name); err != nil {
				return nil, err
			}
			spec, ok := specs[name]
			if !ok {
				// A typo'd flag's value would otherwise be lexed as a
				// positional input; fail like wkhtmltopdf does.
				return nil, fmt.Errorf("unknown switch --%s", name)
			}
			var vals []string
			if hasInline {
				vals = append(vals, inline)
			}
			for len(vals) < spec.nargs {
				if i >= len(argv) {
					return nil, fmt.Errorf("option --%s requires %d argument(s)", name, spec.nargs)
				}
				vals = append(vals, argv[i])
				i++
			}
			items = append(items, item{name: name, vals: vals[:spec.nargs]})
		default:
			// Short option cluster, e.g. -q, -qg, -sA4.
			chars := t[1:]
			for j := 0; j < len(chars); j++ {
				name, ok := shortFlags[chars[j]]
				if !ok {
					return nil, fmt.Errorf("unknown switch -%c", chars[j])
				}
				if err := exitFlag(name); err != nil {
					return nil, err
				}
				spec := specs[name]
				if spec.nargs == 0 {
					items = append(items, item{name: name})
					continue
				}
				var vals []string
				if rest := chars[j+1:]; rest != "" {
					vals = append(vals, rest)
				}
				for len(vals) < spec.nargs {
					if i >= len(argv) {
						return nil, fmt.Errorf("option -%c requires an argument", chars[j])
					}
					vals = append(vals, argv[i])
					i++
				}
				items = append(items, item{name: name, vals: vals})
				break
			}
		}
	}
	return items, nil
}

// startObject begins a page or cover object whose input is still pending.
func (p *parser) startObject(kind string) {
	pg := p.newPage()
	var obj Object
	switch kind {
	case "cover":
		pg.IncludeInOutline = false // covers stay out of outline and TOC
		obj = &CoverObject{Page: pg}
	default:
		obj = &PageObject{Page: pg}
	}
	p.cmd.Objects = append(p.cmd.Objects, obj)
	p.cur = obj
	p.awaiting = kind
}

// Parse parses a full wkhtmltopdf argv (excluding argv[0]) into a Command.
//
// Grammar: [GLOBAL OPTION]... [OBJECT]... <output file>, where an object is
// "page <input>", "cover <input>", "toc", or a bare input. Page options
// before the first object become defaults for every object; after an object
// they apply to that object only. The last positional is the output ("-"
// for stdout).
//
// Unsupported and unknown flags append to Command.Warnings; under --strict
// they produce an error instead. --help/--extended-help/--version return a
// *ExitText error carrying the text to print.
func Parse(argv []string) (*Command, error) {
	p := &parser{
		cmd:     &Command{Global: NewGlobalOptions()},
		defPage: NewPageOptions(),
		defTOC:  NewTOCOptions(),
	}
	items, err := p.lex(argv)
	if err != nil {
		return nil, err
	}

	lastPos := -1
	for idx := range items {
		if items[idx].name == "" {
			lastPos = idx
		}
	}
	if lastPos == -1 {
		return nil, errUsage
	}

	for idx, it := range items {
		if it.name != "" {
			if err := specs[it.name].apply(p, it.name, it.vals); err != nil {
				return nil, err
			}
			continue
		}
		if idx == lastPos {
			if p.awaiting != "" {
				return nil, fmt.Errorf("%s object is missing its input file", p.awaiting)
			}
			p.cmd.Global.Output = it.pos
			continue
		}
		switch {
		case p.awaiting != "":
			p.cur.Options().Input = it.pos
			p.awaiting = ""
		case it.pos == "page" || it.pos == "cover":
			p.startObject(it.pos)
		case it.pos == "toc":
			t := &TOCObject{Page: p.newPage(), TOC: p.defTOC}
			p.cmd.Objects = append(p.cmd.Objects, t)
			p.cur = t
		default:
			o := &PageObject{Page: p.newPage()}
			o.Page.Input = it.pos
			p.cmd.Objects = append(p.cmd.Objects, o)
			p.cur = o
		}
	}

	if len(p.cmd.Objects) == 0 {
		return nil, errUsage
	}
	if p.cmd.Global.Strict && len(p.cmd.Warnings) > 0 {
		return nil, fmt.Errorf("strict mode: %s", strings.Join(p.cmd.Warnings, "; "))
	}
	return p.cmd, nil
}
