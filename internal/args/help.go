package args

// helpText is printed for --help / -h.
var helpText = `Name:
  ` + versionString + `

Synopsis:
  wkhtmltopdf [GLOBAL OPTION]... [OBJECT]... <output file>

Document objects:
  wkhtmltopdf is able to put several objects into the output file, an
  object is either a single webpage, a cover webpage or a table of
  contents. The objects are put into the output document in the order
  they are specified on the command line, options can be specified on a
  per object basis or in the global options area.

Options:
  An object is a bare <input> (URL, file path, or "-" for stdin), or
  "page <input>", "cover <input>", or "toc". The output file may be "-"
  for stdout.

Global Options:
  -q, --quiet                     Be less verbose
      --title <text>              The title of the generated pdf file
  -s, --page-size <Size>          Set paper size to: A4, Letter, etc. (default A4)
      --page-width <unitreal>     Page width
      --page-height <unitreal>    Page height
  -O, --orientation <orientation> Set orientation to Landscape or Portrait
                                  (default Portrait)
  -B, --margin-bottom <unitreal>  Set the page bottom margin (default 10mm)
  -L, --margin-left <unitreal>    Set the page left margin (default 10mm)
  -R, --margin-right <unitreal>   Set the page right margin (default 10mm)
  -T, --margin-top <unitreal>     Set the page top margin (default 10mm)
      --outline                   Put an outline into the pdf (default)
      --no-outline                Do not put an outline into the pdf
      --outline-depth <level>     Set the depth of the outline (default 4)
      --strict                    Fail on unsupported options instead of warning
      --owner-password <password> Encrypt the pdf with this owner password
      --user-password <password>  Encrypt the pdf with this user password
  -h, --help                      Display help
  -H, --extended-help             Display more extensive help
  -V, --version                   Output version information and exit

Page Options (global defaults or per object):
      --background                Do print background (default)
      --no-background             Do not print background
      --cookie <name> <value>     Set an additional cookie (repeatable),
                                  value should be url encoded
      --custom-header <name> <value> Set an additional HTTP header (repeatable)
      --custom-header-propagation Add HTTP headers specified by
                                  --custom-header for each resource request
      --no-custom-header-propagation Do not add HTTP headers specified by
                                  --custom-header for each resource request
                                  (default)
      --javascript-delay <msec>   Wait some milliseconds for javascript finish
                                  (default 200)
  -n, --disable-javascript        Do not allow web pages to run javascript
      --enable-javascript         Do allow web pages to run javascript (default)
      --print-media-type          Use print media-type instead of screen
      --run-script <js>           Run this javascript after the page is done
                                  loading (repeatable)
      --user-style-sheet <path>   Specify a user style sheet to load with
                                  every page
      --viewport-size <size>      Set viewport size, e.g. 1280x1024
      --window-status <string>    Wait until window.status equals this string
                                  before rendering
      --zoom <float>              Use this zoom factor (default 1.0)
      --username <username>       HTTP Authentication username
      --password <password>       HTTP Authentication password
      --enable-local-file-access  Allow local files to read other local files
      --disable-local-file-access Do not allow local files to read other
                                  local files, unless explicitly allowed
                                  with --allow (default)
      --allow <path>              Allow the file or files from the specified
                                  folder to be loaded (repeatable)
      --exclude-from-outline      Do not include the page in outline and toc

Headers And Footer Options:
      --header-left/center/right <text>  Aligned header text
      --header-font-name <name>   Set header font name (default Arial)
      --header-font-size <size>   Set header font size (default 12)
      --header-line               Display line below the header
      --header-spacing <real>     Spacing between header and content in mm
      --header-html <url>         Adds a html header
      --footer-* variants of all of the above
      --replace <name> <value>    Replace [name] with value in header and
                                  footer (repeatable)
      --default-header            Shorthand for --header-left '[webpage]'
                                  --header-right '[page]/[toPage]'
                                  --margin-top 2cm --header-line

TOC Options (after "toc"):
      --toc-header-text <text>    The header text of the toc
                                  (default Table of Contents)
      --toc-level-indentation <width> Indent per heading level (default 1em)
      --toc-text-size-shrink <real> Font scale per heading level (default 0.8)
      --disable-dotted-lines      Do not use dotted lines in the toc
      --disable-toc-links         Do not link from toc to sections
      --xsl-style-sheet <file>    Use the supplied xsl style sheet

Header and footer texts substitute: [page] [topage] [webpage] [section]
[subsection] [date] [time] [title] [doctitle] [sitepage] [sitepages]

Use --extended-help for a listing of the remaining, compatibility-only
switches.
`

// extendedHelpText is printed for --extended-help / -H.
var extendedHelpText = helpText + `
Compatibility switches (accepted, warn-only no-ops):
  bilihtmltopdf renders with a Chromium engine; the following original
  wkhtmltopdf switches have no equivalent and are accepted for drop-in
  compatibility. They print a warning to stderr and are otherwise ignored
  (with --strict they fail instead):

      --collate, --no-collate, --copies, -d/--dpi, -g/--grayscale,
      -l/--lowquality, --cookie-jar, --read-args-from-stdin,
      --use-xserver, --no-pdf-compression, --image-quality, --image-dpi,
      --dump-outline, --encoding, --enable-forms, --disable-forms,
      --enable-internal-links, --disable-internal-links,
      --enable-external-links, --disable-external-links,
      --keep-relative-links, --resolve-relative-links,
      --enable-smart-shrinking, --disable-smart-shrinking,
      --enable-plugins, --disable-plugins, --minimum-font-size,
      -p/--proxy, --proxy-hostname-lookup, --bypass-proxy-for,
      --load-error-handling, --load-media-error-handling, --post,
      --post-file, --cache-dir, --debug-javascript, --no-debug-javascript,
      --stop-slow-scripts, --no-stop-slow-scripts, --checkbox-svg,
      --checkbox-checked-svg, --radiobutton-svg, --radiobutton-checked-svg,
      --ssl-key-path, --ssl-key-password, --ssl-crt-path,
      --enable-toc-back-links, --disable-toc-back-links

  Informational switches (print to stdout and exit 0): --license,
  --readme, --manpage, --htmldoc, --dump-default-toc-xsl.

  Unknown switches are an error, matching wkhtmltopdf.

bilihtmltopdf extensions:
      --strict                    Turn compatibility warnings into errors
      --owner-password <password> Encrypt output via pdfcpu (owner)
      --user-password <password>  Encrypt output via pdfcpu (user)
`

// licenseText is printed for --license.
var licenseText = versionString + `

bilihtmltopdf is distributed under the license shipped with its source
distribution: https://github.com/rvanbaalen/bilihtmltopdf

It reimplements the command-line interface of wkhtmltopdf,
Copyright 2010-2020 wkhtmltopdf authors, released under the
GNU Lesser General Public License version 3 or later
(https://www.gnu.org/licenses/lgpl-3.0.html). No wkhtmltopdf code is
included in this program.
`

// defaultTOCXSL is printed for --dump-default-toc-xsl. It mirrors the
// stylesheet the fork's dumpDefaultTOCStyleSheet emits for default TOC
// settings, so dumped-then-customized sheets keep working.
const defaultTOCXSL = `<?xml version="1.0" encoding="UTF-8"?>
<xsl:stylesheet version="2.0"
                xmlns:xsl="http://www.w3.org/1999/XSL/Transform"
                xmlns:outline="http://wkhtmltopdf.org/outline"
                xmlns="http://www.w3.org/1999/xhtml">
  <xsl:output doctype-public="-//W3C//DTD XHTML 1.0 Strict//EN"
              doctype-system="http://www.w3.org/TR/xhtml1/DTD/xhtml1-transitional.dtd"
              indent="yes" />
  <xsl:template match="outline:outline">
    <html>
      <head>
        <title>Table of Contents</title>
        <meta http-equiv="Content-Type" content="text/html; charset=utf-8" />
        <style>
          h1 {
            text-align: center;
            font-size: 20px;
            font-family: arial;
          }
          div {border-bottom: 1px dashed rgb(200,200,200);}
          span {float: right;}
          li {list-style: none;}
          ul {
            font-size: 20px;
            font-family: arial;
          }
          ul ul {font-size: 80%; }
          ul {padding-left: 0em;}
          ul ul {padding-left: 1em;}
          a {text-decoration:none; color: black;}
        </style>
      </head>
      <body>
        <h1>Table of Contents</h1>
        <ul><xsl:apply-templates select="outline:item/outline:item"/></ul>
      </body>
    </html>
  </xsl:template>
  <xsl:template match="outline:item">
    <li>
      <xsl:if test="@title!=''">
        <div>
          <a>
            <xsl:if test="@link">
              <xsl:attribute name="href"><xsl:value-of select="@link"/></xsl:attribute>
            </xsl:if>
            <xsl:if test="@backLink">
              <xsl:attribute name="name"><xsl:value-of select="@backLink"/></xsl:attribute>
            </xsl:if>
            <xsl:value-of select="@title" />
          </a>
          <span> <xsl:value-of select="@page" /> </span>
        </div>
      </xsl:if>
      <ul>
        <xsl:comment>added to prevent self-closing tags in QtXmlPatterns</xsl:comment>
        <xsl:apply-templates select="outline:item"/>
      </ul>
    </li>
  </xsl:template>
</xsl:stylesheet>
`
