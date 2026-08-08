# bilihtmltopdf

A drop-in replacement for the archived [wkhtmltopdf](https://wkhtmltopdf.org) CLI.
Same command line, modern engine: pages are rendered by headless Chromium
(driven over CDP via [chromedp](https://github.com/chromedp/chromedp)) and
post-processed with [pdfcpu](https://github.com/pdfcpu/pdfcpu). One installable
binary — no containers, no services.

Because the engine is Chromium instead of WebKit 534.34 (2011), modern CSS
(`@layer`, `@container`, custom properties, grid, `@supports`) just works.

## Why this exists

wkhtmltopdf was archived in January 2023. Its rendering engine — a patched
Qt 4.8.7 bundling the WebKit build from roughly 2011 — had been frozen for a
decade: no CSS variables, no working `calc()`, only the 2009 prefixed
flexbox, no grid, and no chance of ever gaining `@layer` or `@container`
(those need a 2022+ engine). The upstream maintainer's own
[status page](https://wkhtmltopdf.org/status.html) explains why the WebKit
path had no future, and every revival route was investigated and came up
short: the "modern" QtWebKit fork stopped at a 2016 engine and died in 2020,
and nine years of requests to port wkhtmltopdf to QtWebEngine produced no
port, because Qt's PDF printing API still lacks the header/footer/outline
hooks wkhtmltopdf is built on.

Meanwhile, an enormous amount of production tooling still shells out to
`wkhtmltopdf` with its particular flag language — headers/footers with
`[page]/[topage]`, `cover` and `toc` objects, per-object options.

bilihtmltopdf keeps that contract and replaces only the engine: the same
command line, driving today's Chromium instead of 2011's WebKit. Existing
integrations keep working; documents get thirteen years of CSS and
JavaScript progress. It is built for old wkhtmltopdf users who want modern
browser rendering without rewriting what already works.

## Install

Every release on [GitHub Releases](https://github.com/rvanbaalen/bilihtmltopdf/releases)
bundles a matching [`chrome-headless-shell`](https://googlechromelabs.github.io/chrome-for-testing/)
so the tool works offline with no system browser — like the original
wkhtmltopdf packages bundled their patched Qt.

**deb/rpm (Linux):**

```sh
sudo dpkg -i bilihtmltopdf_<version>_amd64.deb     # Debian/Ubuntu
sudo rpm -i bilihtmltopdf-<version>.x86_64.rpm     # RHEL/Fedora
```

Installs `/usr/local/bin/wkhtmltopdf` and the renderer under
`/usr/local/lib/chrome-headless-shell/`.

**tar.gz / zip (macOS, Linux, Windows):** extract anywhere and put the
directory on `PATH`; the binary finds the bundled shell in the `lib/`
directory next to it.

**From source:**

```sh
go install github.com/rvanbaalen/bilihtmltopdf/cmd/wkhtmltopdf@latest
```

A source install has no bundled shell; a system Chrome/Chromium/Edge is
used instead (see discovery order below).

> **linux/arm64 and windows/arm64:** Google publishes no
> `chrome-headless-shell` for these platforms, so those archives contain the
> binary only and require a system Chromium (or `WKHTMLTOPDF_CHROME`).

## Usage

```sh
wkhtmltopdf [GLOBAL OPTION]... [OBJECT]... <output file>
```

Identical to wkhtmltopdf 0.12.6: multiple `page`/`cover`/`toc` objects,
per-object options, `-` for stdin/stdout, generated or HTML headers/footers
with `[page]`/`[topage]`/`[title]`/... substitution, TOC generation, outlines.
`wkhtmltopdf --help` documents the supported switches and
`--extended-help` the full compatibility surface.

`--version` prints `bilihtmltopdf <version>`; the tool targets command-line
compatibility with wkhtmltopdf **0.12.6**.

### Examples

```sh
# Single page, A4 (default), backgrounds on
wkhtmltopdf https://example.com out.pdf

# Local file with header/footer page numbers and a PDF outline
wkhtmltopdf --header-center "[page]/[topage]" --footer-line --outline report.html report.pdf

# Cover page + table of contents + chapters, merged into one document
wkhtmltopdf cover cover.html toc chapter1.html chapter2.html book.pdf

# HTML header template with placeholder substitution
wkhtmltopdf --header-html header.html --margin-top 25mm invoice.html invoice.pdf

# Pipe: read HTML from stdin, write PDF to stdout
cat page.html | wkhtmltopdf - - > out.pdf

# Authenticated page behind a login cookie
wkhtmltopdf --cookie session 0a1b2c3d --print-media-type app.internal/report out.pdf

# Fail hard instead of warning on legacy flags (CI-friendly)
wkhtmltopdf --strict --disable-smart-shrinking in.html out.pdf   # exits 1
```

## Chromium discovery

The renderer binary is located in this order; the first hit wins:

1. `WKHTMLTOPDF_CHROME` environment variable (must point at an existing file).
2. Bundled `chrome-headless-shell`, looked up relative to the `wkhtmltopdf`
   executable: `../lib/chrome-headless-shell/` (package layout,
   `/usr/local/bin` + `/usr/local/lib`) then `./lib/chrome-headless-shell/`
   (extracted-archive layout).
3. A system browser: Chrome, Chromium, or Edge at the well-known install
   locations per OS (`$PATH` lookup on Linux).

## Flag compatibility

Design rule: flags that can behave like wkhtmltopdf 0.12.6 do; legacy flags
with no Chromium equivalent are **accepted, warn to stderr, and are
ignored** so existing invocations keep working. The `--strict` extension
turns those warnings into fatal errors. Unknown (typo'd) switches are an
error, exit 1 — same as wkhtmltopdf.

### Supported (same semantics as 0.12.6)

| Area | Flags |
| --- | --- |
| Paper | `--page-size` `--page-width` `--page-height` `--orientation` `-B/-T/-L/-R` margins |
| Rendering | `--zoom` `--background`/`--no-background` (backgrounds on by default) `--images`/`--no-images` `--viewport-size` |
| Media type | `--print-media-type`/`--no-print-media-type` (default **screen**, matching wkhtmltopdf — see notes below) |
| JavaScript | `--enable-javascript`/`--disable-javascript` (`-n`) `--javascript-delay` (default 200 ms) `--window-status` `--run-script` |
| Styling | `--user-style-sheet` |
| Network | `--cookie` `--custom-header` `--custom-header-propagation` `--username` `--password` |
| Local files | `--enable-local-file-access`/`--disable-local-file-access` (blocked by default, like 0.12.6) `--allow` |
| Headers/footers | `--header-left/center/right` `--footer-*` `--*-font-name/size` `--*-line` `--*-spacing` `--*-html` `--default-header` `--replace`, with `[page]` `[topage]` `[title]` `[date]` `[webpage]` `[url]` substitution |
| Objects | multiple `page` inputs, `cover`, `toc`, stdin `-`, stdout `-` |
| Outline/TOC | `--outline`/`--no-outline` `--outline-depth` `--include-in-outline`/`--exclude-from-outline` `--toc-header-text` `--toc-level-indentation` `--toc-text-size-shrink` `--disable-dotted-lines` `--disable-toc-links` `--dump-default-toc-xsl` |
| Misc | `--title` `-q`/`--quiet` `--log-level none` `--help` `--extended-help` `--version` `--license` |

### Behavior differs

| Flag / feature | Difference |
| --- | --- |
| media type default | Defaults to **screen** emulation to match wkhtmltopdf. Chrome-era tooling (Puppeteer, `--headless=new --print-to-pdf`) defaults to *print* media, so templates written for those need an explicit `--print-media-type`. Conversely, wkhtmltopdf templates that carry `@media print` rules will now see them honored properly under `--print-media-type`. |
| `--xsl-style-sheet` | Ignored with a warning (no XSLT engine in Chromium). The built-in TOC style approximates wkhtmltopdf's default XSLT output; `--dump-default-toc-xsl` still prints the reference stylesheet. |
| `[page]`/`[topage]` across objects | With multiple input documents, page numbers restart at 1 per document (each object is a separate Chromium print job, merged afterwards). wkhtmltopdf numbered continuously. Warned when it applies; pre-merge into one HTML document when continuous numbering matters. |
| `[section]`/`[subsection]`/`[sitepage]`/`[sitepages]` | No Chromium counterpart; substitute to empty text (warned). |
| `--owner-password`/`--user-password` | Output is encrypted with AES-256 via pdfcpu instead of wkhtmltopdf's RC4-40 (strictly better; very old PDF viewers may not open it). |
| `--encoding` | Accepted but has no effect: Chromium offers no fallback-encoding override; undeclared pages decode as UTF-8. Declare the charset in the document or HTTP header. |
| `--page-offset` | Cannot shift Chromium page counters; `[page]` starts at 1 (warned). |
| `--log-level` | Only `none` (= `--quiet`) is distinguished; `error`/`warn`/`info`/`debug` behave like the default. |
| `--copies` | Stored in metadata terms only; `> 1` produces a single copy with a warning. |

### Accepted but warn-ignored (no Chromium equivalent)

`--collate`/`--no-collate`, `--dpi`, `--grayscale`, `--lowquality`,
`--cookie-jar`, `--read-args-from-stdin`, `--use-xserver`,
`--no-pdf-compression`, `--image-quality`, `--image-dpi`, `--dump-outline`,
`--enable-forms`/`--disable-forms`, internal/external link toggles,
`--keep-relative-links`/`--resolve-relative-links`,
`--enable-smart-shrinking`/`--disable-smart-shrinking`, plugin toggles,
`--minimum-font-size`, proxy flags (`-p`, `--proxy-hostname-lookup`,
`--bypass-proxy-for`), `--load-error-handling`,
`--load-media-error-handling`, `--post`/`--post-file`, `--cache-dir`,
`--debug-javascript`, `--stop-slow-scripts`, checkbox/radiobutton SVG
flags, `--ssl-key-path`/`--ssl-key-password`/`--ssl-crt-path`,
`--enable-toc-back-links`.

Each prints `Warning: --<flag> is not supported and will be ignored` to
stderr; `--strict` makes that fatal instead. `-q`/`--quiet` silences
progress *and* warnings (wkhtmltopdf documents `-q` as `--log-level none`).

### Behavior notes

- `--custom-header` values are sent on main-document requests only, matching
  wkhtmltopdf's default; add `--custom-header-propagation` to repeat them on
  every resource request (mind credential leakage to third-party hosts).
- `--username`/`--password` answer HTTP auth challenges instead of being sent
  preemptively.
- `--cookie` values are expected URL-encoded (percent-decoded before setting),
  as documented by wkhtmltopdf.

## Migrating from wkhtmltopdf 0.12.6: expect layout drift

Any Chromium-class engine renders differently from the 2011 WebKit build —
this is inherent to leaving the old engine, not specific to this tool.
**Re-verify production templates side by side before switching.** The usual
sources of drift:

- **Text metrics.** Blink's font selection, hinting, and line breaking
  differ; long documents can gain or lose page breaks, shifting page counts
  and TOC numbers.
- **No smart shrinking.** wkhtmltopdf auto-scaled content to fit the page
  width by default. Chromium does not; wide fixed-width layouts may clip or
  overflow. Compensate with `--zoom` or responsive CSS.
- **DPI model.** Chromium uses the CSS standard 96 px/inch everywhere;
  wkhtmltopdf's `--dpi`/`--image-dpi` knobs have no counterpart.
- **Modern CSS now applies.** Rules the 2011 engine ignored (flexbox
  refinements, grid, `@supports`, custom properties) now take effect —
  usually the goal, occasionally a surprise in old templates.
- **`@media print` is real.** Under `--print-media-type`, Chromium fully
  honors print stylesheets, `@page`, and `break-*` properties that the old
  engine only partially implemented.
- **JavaScript is current.** Pages run a modern engine (ES2023+); scripts
  written around 2011-era quirks may behave differently. The default
  200 ms `--javascript-delay` is preserved.
- **TLS is current.** Ancient protocol/cipher options are gone; hosts must
  speak modern TLS.

A practical check: render your corpus with 0.12.6 and bilihtmltopdf,
compare with `pdftotext`/`pdfinfo` (page counts, header text, bookmarks)
and rasterized image diffs, then re-approve templates.

## Development

```sh
make build         # build bin/wkhtmltopdf
make test          # unit tests (no browser needed)
make e2e           # end-to-end; needs a local Chrome and poppler (pdftotext)
```

## Releasing

Releases are automated with
[release-please](https://github.com/googleapis/release-please): commits to
`main` follow [Conventional Commits](https://www.conventionalcommits.org)
(`feat:`, `fix:`, …), release-please maintains a release PR with the
changelog and next semver, and merging that PR tags the release — at which
point CI runs [goreleaser](https://goreleaser.com) to attach the archives
and deb/rpm packages.

Local release tooling:

```sh
make fetch-shell   # download chrome-headless-shell bundles into third_party/
                   # (also runs automatically as a goreleaser before-hook)
make release-dry   # snapshot build: archives + deb/rpm into dist/, nothing published
make check         # validate .goreleaser.yaml
```

`scripts/fetch-headless-shell.sh` pulls the Stable-channel
`chrome-headless-shell` per platform (pin with `CHROME_VERSION=...`) into
`third_party/headless-shell/<goos>_<goarch>/lib/chrome-headless-shell/`,
which `.goreleaser.yaml` maps into the archives (`lib/` next to the binary)
and linux packages (`/usr/local/lib/chrome-headless-shell`).
