<div align="center">

# gomupdf

**The fast, idiomatic way to read, render, and write PDFs in Go.**

A focused [cgo](https://pkg.go.dev/cmd/cgo) binding over the battle-tested
[MuPDF](https://mupdf.com/core) C core — text extraction, table detection,
rendering, and full document assembly, behind a small, Go-shaped API.

[![CI](https://github.com/tredy-ai/gomupdf/actions/workflows/ci.yml/badge.svg)](https://github.com/tredy-ai/gomupdf/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/tredy-ai/gomupdf/graph/badge.svg)](https://codecov.io/gh/tredy-ai/gomupdf)
[![Go Reference](https://pkg.go.dev/badge/github.com/tredy-ai/gomupdf.svg)](https://pkg.go.dev/github.com/tredy-ai/gomupdf)
[![Go Report Card](https://goreportcard.com/badge/github.com/tredy-ai/gomupdf)](https://goreportcard.com/report/github.com/tredy-ai/gomupdf)
[![Built with Claude Code](https://img.shields.io/badge/Built%20with-Claude%20Code-D97757?logo=anthropic&logoColor=white)](https://claude.com/claude-code)

</div>

> Fork of [srijanmukherjee/gomupdf](https://github.com/srijanmukherjee/gomupdf), maintained by Tredy.
> See [FORK.md](FORK.md) for the base version, the patch list and the upstream policy.

```go
doc, _ := gomupdf.Open("invoice.pdf")
defer doc.Close()

text, _   := doc.Text()         // every page, in reading order
page, _   := doc.LoadPage(0)
tables, _ := page.FindTables()  // structured rows & columns

pm, _ := page.Pixmap()          // render the page…
_ = pm.SavePNG("page.png")      // …and save it as an image
```

---

## What this fork changes

This fork backs a PII-redaction service, so it differs from upstream where a
redaction can fail without telling the caller. In short:

- **Redaction is checked.** `ApplyRedactions(ApplyOptions)` removes text glyph
  by glyph (quads follow the quad shape, not its bounding box), strips declared
  text (`/ActualText`, `/Alt`, `/E`) from redacted pages, extracts the page
  again, and returns an error (`*RedactionCheckError`) if redacted text is still
  there or moved. After such an error the `Document` must be discarded.
- **`Scrub` and `HiddenContent`** remove, and report, the places outside page
  content where text survives: metadata, attachments, scripts, annotations,
  form fields, outlines, private data. **`ShowAllLayers`** reveals hidden
  optional-content layers so they can be extracted and redacted.
- **Extraction ignores `/ActualText` by default** and exposes per-character
  quads (`RawDictWithOptions`, `Char.Quad`).
- **Damaged files are repaired at open**, locked documents answer nothing until
  `Authenticate`, and `SaveWithOptions` streams to the file.
- Requires MuPDF 1.26 or newer; built and tested on Go 1.27, Alpine 3.23,
  MuPDF 1.26.11.

Several of these break upstream's API. [FORK.md](FORK.md) has the patch list,
the exact rules, migration notes, known limitations and how to test.

---

## Why gomupdf?

- **One dependency, many formats.** PDF, XPS, EPUB, MOBI, FB2, CBZ, SVG, plain
  text, and raster images — all open through the same `Document` API.
- **Extraction that keeps geometry.** Plain text *and* word/span boxes,
  reading-order reconstruction, and table detection with two strategies
  (word-alignment and vector-ruling).
- **Real writing, not just reading.** Create documents, insert text/images,
  merge, set metadata/XMP, page labels, page boxes, redact, and save with
  fine-grained options including AES-256 encryption and incremental updates
  (for documents opened from memory).
- **Idiomatic Go.** `iter.Seq2` page ranges, typed geometry, `error` returns,
  deterministic `Close()`, no hidden globals.
- **Thin and honest.** A small layer over MuPDF — the credit for the engine
  belongs upstream; gomupdf gives it a clean Go surface.

## Table of contents

- [Install](#install) · [Requirements](#requirements)
- [What this fork changes](#what-this-fork-changes) · [60-second tour](#60-second-tour)
- **Cookbook** — [Open](#open-anything) · [Text & geometry](#text--geometry) ·
  [Tables](#tables) · [Search, outline & links](#search-outline--links) ·
  [Render & images](#render--images) · [Pages & boxes](#pages-rotation--boxes) ·
  [Metadata & labels](#metadata--page-labels) · [Write & assemble](#write--assemble) ·
  [Redact](#redact) · [Scrub & hidden content](#scrub--hidden-content) ·
  [Optional content (layers)](#optional-content-layers) · [Save options](#save-options)
- [API cheat-sheet](#api-cheat-sheet) · [Feature support](#feature-support)
- [Concurrency & memory](#concurrency--memory) · [Subpackages](#subpackages)
- [Acknowledgements](#acknowledgements) · [License](#license)

---

## Install

```sh
go get github.com/tredy-ai/gomupdf
```

### Requirements

gomupdf links against the MuPDF C library via cgo, so a **C toolchain** and
**MuPDF (shared library + development headers)** must be present at build time.
`CGO_ENABLED=1` is required (on by default for native builds).

| Platform | Install MuPDF |
|---|---|
| Alpine (3.23, the tested target) | `apk add build-base pkgconf mupdf-dev` |
| macOS (Homebrew) | `brew install mupdf` |
| Debian / Ubuntu | `sudo apt-get install libmupdf-dev mupdf-tools` |
| Fedora / RHEL | `sudo dnf install mupdf-devel` |
| Arch | `sudo pacman -S libmupdf` |

**Alpine / musl.** This fork is built and tested on Alpine 3.23 with its
packaged MuPDF 1.26.11 and Go 1.27 (MuPDF 1.26 or newer is required by the
compile guard; see the version notes below). On linux the link
flags come from `pkg-config --libs mupdf`, so nothing else needs configuring:

```sh
apk add build-base pkgconf mupdf-dev
go build ./...
```

- Extraction output depends on the MuPDF version (`gomupdf.MuPDFVersion()`
  reports the headers the binary was built against). On Homebrew's MuPDF 1.28.6
  (macOS) the package builds, but at last check (commit `f0ed073`) three tests
  failed: `TestRawDictWithOptionsDehyphenate` (1.28 dehyphenates differently)
  and two per-glyph redaction cases where the right glyphs are removed but
  following characters shift. 1.28 has not been re-run since many later
  changes. Only Alpine 3.23 / MuPDF 1.26.11 is a supported target.
- MuPDF 1.26 is the oldest version the compile guard accepts (older headers
  fail with a clear `#error`). CI and the corpus test's known-failures list are
  for 1.26.11. MuPDF 1.25 was supported until the `build!: require MuPDF 1.26`
  commit; before that the suite other than `TestCorpus` passed on 1.25.6
  (last run at `d381f88`), and `TestCorpus` did not.
- On macOS the cgo flags (`cgo_darwin.go`) point at Homebrew's Apple Silicon
  prefix, `/opt/homebrew`; elsewhere set `CGO_CFLAGS` / `CGO_LDFLAGS` as below.
- The Debian, Fedora and Arch rows in the table above are carried over from
  upstream and are not tested in this fork.
- MuPDF links **dynamically** on Alpine (`mupdf-dev` ships `libmupdf.so` and no
  static archive), so runtime images must install `mupdf-libs`, which pulls in
  MuPDF's shared dependencies (freetype, harfbuzz, jbig2dec, openjpeg, ...).
- A binary built on Alpine is a musl binary: it only runs on a musl runtime,
  so use an Alpine runtime image of the same release as the build image.
- For a MuPDF built from source with its bundled third-party code (a
  `libmupdf-third` next to `libmupdf`), build with `-tags mupdf_bundled`, which
  links `-lmupdf -lmupdf-third -lm` instead of using pkg-config. It was
  built and tested once, against MuPDF 1.25.6 built from source, and passed,
  before the minimum became 1.26; it has not been run against 1.26.
  The CI workflow does not exercise it, and has not run yet.

<details>
<summary><b>From source / custom prefix / Windows / troubleshooting</b></summary>

**From source** (any platform):

```sh
git clone --recursive https://github.com/ArtifexSoftware/mupdf.git
cd mupdf && make HAVE_X11=no HAVE_GLUT=no prefix=/usr/local install
```

**Custom install location.** The default cgo flags (`cgo_darwin.go`,
`cgo_linux.go`, `cgo_linux_bundled.go`) target `/opt/homebrew` on darwin and
pkg-config on linux (the `mupdf_bundled` tag links `-lmupdf -lmupdf-third -lm`
from the system paths). If MuPDF lives elsewhere, set `PKG_CONFIG_PATH` or
supplement via environment variables instead of editing source:

```sh
export CGO_CFLAGS="-I/your/prefix/include"
export CGO_LDFLAGS="-L/your/prefix/lib -lmupdf -lmupdf-third"
go build ./...
```

**Windows** is not covered by the default flags; build with a MinGW/MSYS2
toolchain and set `CGO_CFLAGS` / `CGO_LDFLAGS` to your MuPDF install.

**`ld: library 'mupdf' not found`** means MuPDF is not installed or not on the
library path — install it or set `CGO_LDFLAGS` as above.

</details>

---

## 60-second tour

```go
package main

import (
	"fmt"
	"log"

	"github.com/tredy-ai/gomupdf"
)

func main() {
	doc, err := gomupdf.Open("document.pdf")
	if err != nil {
		log.Fatal(err)
	}
	defer doc.Close() // always Close; the finalizer is only a backstop

	// Encrypted? Unlock it (or use OpenWithPassword to do both in one call).
	if doc.NeedsPass() && !doc.Authenticate("secret") {
		log.Fatal("wrong password")
	}

	fmt.Println("pages:", doc.PageCount())

	for i, page := range doc.Pages() { // ranges via iter.Seq2
		text, err := page.GetText()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("=== page %d ===\n%s\n", i, text)
	}
}
```

> **Coordinates.** All geometry is in PDF points. The `geometry` package uses a
> top-left origin with y growing downward (`Rect{X0,Y0,X1,Y1}`).

---

## Cookbook

### Open anything

```go
// PDF fast paths
doc, _ := gomupdf.Open("file.pdf")                      // from a file
doc, _ := gomupdf.OpenStream(pdfBytes)                  // from memory
doc, _ := gomupdf.OpenWithPassword("file.pdf", "pw")    // open + authenticate
doc, _ := gomupdf.OpenStreamWithPassword(b, "pw")

// Any MuPDF-supported format (XPS, EPUB, MOBI, FB2, CBZ, SVG, TXT, images)
doc, _ := gomupdf.OpenAny("book.epub")                  // format from extension
doc, _ := gomupdf.OpenAnyStream(pngBytes, "png")        // explicit format hint

// Turn a non-PDF (or any) document into a writable PDF
pdf, _ := doc.ConvertToPDF()
defer pdf.Close()
```

Non-PDF documents open **read-only** (text, geometry, rendering all work); use
`ConvertToPDF` to get a writable PDF `Document`.

Changed from upstream: `Open` reads the file on demand instead of loading it
(do not modify the file while the document is open); every object of a PDF is
loaded at open, so a damaged file is repaired before anything is read or
changed; and a password-protected PDF answers nothing (`PageCount` is 0,
`LoadPage` fails) until `Authenticate` succeeds. A page tree that claims more pages than it can serve is
refused at open: one declaring more pages than the file has objects is accepted only up to 1,048,576 pages and
only if MuPDF can build its page map from it, and one declaring more than 8,388,607 never.

### Text & geometry

```go
text, _   := doc.Text()            // whole document, pages joined by '\f'
pages, _  := doc.TextByPage()      // []string, one per page
lines, _  := doc.AllLines()        // flat lines: soft-hyphens stripped, blanks dropped

words, _  := page.Words()          // word boxes with character-level precision
spans, _  := page.Spans()          // positioned fragments + font name/size
blocks, _ := page.StructuredText() // []Block → []Span tree with bbox & baseline
raw, _    := page.StructuredJSON() // the raw block/line/char JSON, your way
```

`Words` is the recommended input for layout work and table reconstruction — the
boxes are unions of per-character quads, not span approximations.

Per-character geometry, with the options pinned (`RawDictWithOptions` takes the
same `TextOptions` as `ExtractText`):

```go
blocks, err := page.RawDictWithOptions(gomupdf.TextOptions{AccurateBBoxes: true})
if err != nil {
	log.Fatal(err)
}
for _, b := range blocks {
	for _, line := range b.Lines {
		fmt.Println(line.Dir, line.WMode) // baseline direction, 0 horizontal / 1 vertical
		for _, c := range line.Chars {
			fmt.Println(string(c.Rune), c.Origin, c.BBox, c.Quad) // page space, y down
		}
	}
}
```

Changed from upstream: extraction returns the glyphs drawn and **ignores
`/ActualText`** (the replacement text a PDF can declare for them). Pass
`TextOptions{UseActualText: true}` to `ExtractText` or `RawDictWithOptions` to
get MuPDF's default back; the other extraction APIs have no toggle. With it set,
MuPDF 1.26.11 can crash the whole process on crafted input, so do not enable it
on untrusted files outside a separate process. `RawDict` never skips a
character: unreadable output is an error. Form field values, link targets and
annotation contents are not part of page text; read them with `page.Widgets()`,
`page.Links()` and `page.Annotations()`.

### Tables

Two strategies: **word-alignment** (`StrategyText`, the default) and
**vector-ruling** (`StrategyLines`, follows drawn gridlines).

```go
tables, _ := page.FindTables() // default word-alignment strategy
tables, _ := page.FindTables(gomupdf.TableSettings{
	Strategy:      gomupdf.StrategyLines,
	SnapTolerance: 3,
})

for _, t := range tables {
	fmt.Println(t.NumRows(), "x", t.NumCols())
	for _, row := range t.Rows {
		fmt.Println(row) // []string cells
	}
}
```

### Search, outline & links

```go
quads, _ := page.Search("invoice")       // oriented quads (great for highlights)
rects, _ := page.SearchRects("invoice")  // axis-aligned rects
toc, _   := doc.TOC()                     // flat, depth-first outline entries
links, _ := page.Links()                  // clickable rects + target URIs
```

### Render & images

```go
pm, _  := page.Pixmap(gomupdf.PixmapOptions{Zoom: 2.0}) // 144 DPI RGB raster
png, _ := pm.PNG()                                       // encode to PNG bytes
_ = pm.SavePNG("page.png")
img, _ := pm.Image()                                     // a stdlib image.Image
px := pm.Pixel(10, 10)                                   // raw component bytes

infos, _ := page.GetImages()       // each image's placement bbox + source dims
raw, _   := page.ExtractImage(0)   // original encoded bytes (JPEG/PNG/…)
paths, _ := page.GetDrawings()     // vector fill/stroke paths with color & width
```

### Pages, rotation & boxes

```go
n      := doc.PageCount()
page, _ := doc.LoadPage(0)

rot, _  := page.Rotation()           // 0, 90, 180, or 270
_ = page.SetRotation(90)             // multiples of 90, normalized

mb, _ := page.MediaBox()             // full physical page, unrotated points
cb, _ := page.CropBox()              // visible region (defaults to MediaBox)
_ = page.SetCropBox(geometry.Rect{X0: 10, Y0: 10, X1: 400, Y1: 600})
```

### Metadata & page labels

```go
// Read / write the standard info dictionary
meta, _ := doc.Metadata()            // map: title, author, producer, dates, …
_ = doc.SetMetadata(map[string]string{
	"title":  "Quarterly Report",
	"author": "Finance",
	// an empty value clears a field; unknown keys are ignored
})

// XMP packet (Catalog /Metadata stream)
xmp, _ := doc.XMP()
_ = doc.SetXMP(xmlPacket)
_ = doc.DeleteXMP()

// Logical page numbering: front-matter as i, ii, iii then body 1, 2, 3 …
_ = doc.SetPageLabels([]gomupdf.PageLabel{
	{StartPage: 0, Style: gomupdf.LabelRomanLower, Start: 1},
	{StartPage: 2, Style: gomupdf.LabelDecimal, Prefix: "", Start: 1},
})
label, _ := page.Label()             // resolved label, e.g. "ii" or "A-3"
rules, _ := doc.PageLabels()          // read the rules back
```

### Write & assemble

```go
doc, _ := gomupdf.NewPDF()
defer doc.Close()

doc.NewPage(595, 842)                 // A4 in points
p, _ := doc.LoadPage(0)
p.InsertText(72, 800, 12, "Hello, gomupdf")
p.InsertImage(geometry.NewRect(0, 0, 200, 200), imageBytes)

doc.InsertPDF(otherPDFBytes, "")      // append every page of another PDF
doc.DeletePage(3)
```

### Redact

Mark characters by their quads, apply, and treat any error as "discard this
document". `isSecret` stands for your detector.

```go
for i, page := range doc.Pages() {
	blocks, err := page.RawDictWithOptions(gomupdf.TextOptions{})
	if err != nil {
		return err
	}
	var quads []geometry.Quad
	for _, b := range blocks {
		for _, l := range b.Lines {
			for _, c := range l.Chars {
				if isSecret(c) {
					quads = append(quads, c.Quad)
				}
			}
		}
	}
	if len(quads) == 0 {
		continue
	}
	if err := page.AddRedactionQuads(quads, gomupdf.RedactOptions{}); err != nil {
		return fmt.Errorf("page %d: %w", i, err)
	}
	_, err = page.ApplyRedactions(gomupdf.ApplyOptions{Images: gomupdf.ImageRemove})
	var ce *gomupdf.RedactionCheckError
	switch {
	case errors.Is(err, gomupdf.ErrTextRemains), errors.Is(err, gomupdf.ErrTextMoved):
		// the page is not redacted; reject the file. errors.As gives the counts.
		if errors.As(err, &ce) {
			log.Printf("page %d: %d remain, %d moved, %d excess", ce.Page, ce.Remaining, ce.Moved, ce.Excess)
		}
		return err
	case err != nil:
		return err // any error: discard doc; later saves fail with ErrRedactionFailed
	}
}
```

- `AddRedaction(rect, RedactOptions{})` takes a rect instead; it returns an
  error for an empty rect (zero width or height), which a union of glyph boxes
  can be: use `AddRedactionQuads` for those.
- `ApplyRedactions` applies every redaction annotation on the page, including
  ones already in the file. The zero `ApplyOptions` draws a black box, blanks
  covered image pixels, leaves line art and removes text; `ImageRemove` drops an
  overlapping image whole.
- A page with more than 65,536 redaction regions, or too deeply nested forms or
  structure, is refused before anything changes. Any other error may come after
  the page started to change.
- Redaction cannot reach tiling patterns, soft masks or Type 3 glyphs; the
  check after applying turns those leftovers into an error. See
  [FORK.md](FORK.md#44-redaction).

### Scrub & hidden content

`Scrub` removes document-level content that page redaction never touches
(metadata, attachments, scripts, annotations, form fields, outlines, private
data); `HiddenContent` reports what it would remove, without editing. Follow
`Scrub` with a non-incremental save with `Garbage >= 1`, and discard the
document if `Scrub` returns an error.

```go
hc, err := doc.HiddenContent()
if err != nil {
	return err
}
fmt.Println(hc.JavaScript, hc.EmbeddedFiles, hc.Widgets, hc.Layers)

if err := doc.Scrub(gomupdf.ScrubOptions{}); err != nil { // zero value removes everything it knows
	return err // discard doc
}
err = doc.SaveWithOptions("out.pdf", gomupdf.SaveOptions{Garbage: 3, Deflate: true})
```

`ScrubOptions` has `KeepAnnotations`, `KeepFormFields`, `KeepOutlines` and
`KeepStructure`. The zero value also removes the logical structure tree
(accessibility tagging, whose `/Alt`, `/ActualText`, `/T`, `/ID` and table
`/Summary` text extraction never returns), so the output is no longer a tagged
PDF; `KeepStructure` keeps it, text and all. Reopen the output and call
`HiddenContent` to verify it.

### Optional content (layers)

Content on a layer that MuPDF's default configuration hides is never
extracted, searched, rendered or redacted, but stays in the file. `Scrub` does
not touch it. `ShowAllLayers` makes all of it ordinary, visible content; call it
before extraction and detection. `HiddenLayers` counts what MuPDF hides but
cannot see inline `/OC << >> BDC` property lists in content streams, so call
`ShowAllLayers` whenever `Layers > 0` if you need certainty.

```go
hc, err := doc.HiddenContent()
if err != nil {
	return err
}
if hc.Layers > 0 { // only Layers == 0 rules out hidden content; reveal whenever certainty matters
	if err := doc.ShowAllLayers(); err != nil {
		return err
	}
}
```

### Save options

```go
data, _ := doc.SaveBytes(true)                     // quick: serialize (+garbage)
_ = doc.EzSave("out.pdf")                          // sensible defaults to a file

// Fine-grained control
data, _ = doc.SaveBytesWithOptions(gomupdf.SaveOptions{
	Garbage:       3,     // 0–4 dead-object collection
	Deflate:       true,  // compress streams
	Clean:         true,  // sanitize content streams
	Linear:        true,  // "fast web view"
	ASCII:         true,  // escape binary to 7-bit
})

// AES-256 encryption
data, _ = doc.SaveBytesWithOptions(gomupdf.SaveOptions{
	Encrypt: true, UserPassword: "open-me", OwnerPassword: "full", Permissions: -1,
})
data, _ = doc.SaveEncryptedBytes("user", "owner")  // shorthand

// Incremental update (stream-opened docs; needs OpenStream): appends changes after the
// original bytes instead of rewriting the whole file. A file MuPDF had to repair
// cannot be saved incrementally. Not for redacted output: use a full save.
_ = doc.SaveIncremental("out.pdf")
```

`SaveWithOptions` (plain, unencrypted) streams to a temporary file next to the
destination and renames it over the destination, so a failed save leaves no
partial file; encrypted and incremental saves, and the `SaveBytes*` functions,
are built in memory. It refuses to overwrite the file the document was opened
from. After a failed `ApplyRedactions`, every save returns an error wrapping
`ErrRedactionFailed`. `SaveOptions{}` writes an uncompressed file; use
`DefaultSaveOptions()` (`Garbage: 3`, `Deflate: true`) in production.

---

## API cheat-sheet

| Want to… | Call |
|---|---|
| Open a PDF | `Open` · `OpenStream` · `OpenWithPassword` |
| Open EPUB/XPS/image/… | `OpenAny` · `OpenAnyStream` |
| Convert to writable PDF | `doc.ConvertToPDF()` |
| Count / load / iterate pages | `doc.PageCount` · `doc.LoadPage` · `doc.Pages` |
| Extract text | `doc.Text` · `page.GetText` · `page.Lines` · `page.ExtractText` (clip+flags) |
| Get positioned text | `page.Words` · `page.Spans` · `page.StructuredText` · `page.RawDict`/`Dict` · `page.RawDictWithOptions` (per-char quads) |
| Export markup | `page.HTML` · `page.XHTML` · `page.XML` |
| Find tables | `page.FindTables` → `table.ToMarkdown` / `table.Header` |
| Search | `page.Search` · `page.SearchRects` · `page.SearchWith` (clip, cap) |
| Outline / links | `doc.TOC`/`SetTOC` · `page.Links`/`InsertLink`/`DeleteLink` |
| Annotate | `page.AddHighlight`/`AddFreeText`/`AddLine`/`Annotations`/`DeleteAnnotation` |
| Redact | `page.AddRedaction`/`AddRedactionQuads` → `page.ApplyRedactions(ApplyOptions)` (errors: `*RedactionCheckError`, `ErrRedactionFailed`) |
| Scrub / inspect | `doc.Scrub(ScrubOptions)` · `doc.HiddenContent` |
| Layers | `doc.ShowAllLayers` · `HiddenContent.Layers`/`HiddenLayers` |
| Version | `gomupdf.MuPDFVersion` |
| Forms | `page.Widgets` · `page.SetTextField` · `page.AddTextField` |
| Page ops | `doc.CopyPage`/`MovePage`/`SelectPages`/`InsertPDFRange` |
| Draw | `page.DrawRect`/`DrawLine`/`DrawCircle` · `page.InsertTextbox` |
| Render | `page.Pixmap(PixmapOptions{DPI,CMYK,Clip…})` → `pm.PNG`/`JPEG`/`Save`/`Image` |
| Pixmap ops | `pm.Invert` · `pm.Gamma` · `pm.Bytes` |
| Fonts | `page.GetFonts` · `doc.ExtractFont` · `NewFont(…).TextLength` |
| Images / drawings | `page.GetImages` · `page.ExtractImage` · `page.GetDrawings` |
| Rotation / boxes | `page.Rotation`/`SetRotation` · `page.MediaBox`/`CropBox`/`Set…` |
| Metadata / XMP | `doc.Metadata`/`SetMetadata` · `doc.XMP`/`SetXMP`/`DeleteXMP` |
| Page labels | `doc.PageLabels`/`SetPageLabels` · `page.Label` |
| Build / edit | `NewPDF` · `doc.NewPage` · `doc.DeletePage` · `doc.InsertPDF` |
| Write content | `page.InsertText` · `page.InsertImage` · `page.AddRectAnnot` |
| Save | `doc.SaveBytes` · `doc.EzSave` · `doc.SaveWithOptions` · `doc.SaveIncremental` |

Full reference: **[pkg.go.dev/github.com/tredy-ai/gomupdf](https://pkg.go.dev/github.com/tredy-ai/gomupdf)**

## Feature support

Legend: ✅ supported · 🟡 partial · ⬜ not yet

| Area | Capability | Status |
|---|---|---|
| Read | Open PDF (file / stream / password) | ✅ |
| Read | Open XPS / EPUB / CBZ / SVG / images, convert to PDF | ✅ |
| Read | Page count, load, iterate | ✅ |
| Read | Plain & structured text, char-level word boxes | ✅ |
| Read | dict/rawdict (per-char), HTML / XHTML / XML export | ✅ |
| Read | Text extraction options (clip rect, stext flags) | ✅ |
| Read | Geometry primitives (`geometry` pkg) | ✅ |
| Read | Text search (quads & rects; region clip + hit cap) | ✅ |
| Read | Tables — word-alignment & vector-ruling, → Markdown | ✅ |
| Structure | Outline (TOC) read & write | ✅ |
| Structure | Page links — read, insert, goto, delete | ✅ |
| Structure | Page labels, page boxes, rotation | ✅ |
| Structure | Metadata read/write + XMP | ✅ |
| Structure | Embedded files (read/write) | ⬜ (`Scrub` removes them) |
| Structure | Optional-content layers: report hidden layers, reveal all | 🟡 no per-layer toggling |
| Render | Pixmap → raster (DPI, RGB/Gray/CMYK, clip, annot toggle) | ✅ |
| Render | Pixmap ops (invert, gamma) + PNG/JPEG/PNM encode & save | ✅ |
| Render | Extract images + placement bbox | ✅ |
| Render | Vector drawings / paths | ✅ |
| Read | Fonts — enumerate, extract embedded program, glyph metrics | ✅ |
| Render | OCR, image masks | ⬜ |
| Write | New PDF, page insert/delete/copy/move/select, merge & range-merge | ✅ |
| Write | Insert text, images, word-wrapped text boxes | ✅ |
| Write | Draw lines / rects / circles | ✅ |
| Write | Annotations — markup, shapes, ink, free-text, sticky notes | ✅ |
| Write | Redaction (rect + quad, per-glyph removal, checked after apply) | ✅ |
| Write | Scrub document-level content, report it (`HiddenContent`) | ✅ |
| Write | Form widgets — read, fill, create text fields | 🟡 text/checkbox |
| Write | Save options (deflate, clean, linear, ASCII, incremental; plain saves streamed) | ✅ |
| Write | AES-256 encryption | ✅ |
| Write | Digital signatures, full Shape API, per-layer (OCG) editing | ⬜ |

> gomupdf is actively working toward feature parity with PyMuPDF. The remaining
> ⬜ items (digital signatures, full form types, OCR, per-layer editing) are on the
> roadmap.

## Concurrency & memory

A `Document` owns a single native MuPDF context and is **not** safe for
concurrent use; all of its methods are serialized internally by a mutex. To
process documents in parallel, **open one `Document` per goroutine**.

- Always `Close()` a document when done — the finalizer is only a backstop.
- MuPDF runs in your process. On untrusted files it can hang for minutes on a
  few crafted inputs, crash (with `UseActualText`), or allocate hundreds of
  megabytes (pixel-redacted images are stored uncompressed). A service should
  process each document in a separate worker process with time and memory
  limits; see [FORK.md](FORK.md#6-using-the-package-in-a-redaction-service).
- Bytes backing an `OpenStream` document are held in memory for its lifetime;
  `Open` reads the file lazily and keeps it open until `Close`. Either way
  MuPDF reads on demand.
- C resources are released deterministically in helper functions, so a Go-side
  mistake cannot leak the underlying objects.

## Backends

gomupdf's public API is **backend-neutral**: every operation that touches a
native engine is delegated through an internal interface. The **MuPDF (cgo)**
backend is the default and is compiled in automatically — you don't need to do
anything.

Building with `-tags nomupdf` excludes all cgo (no C toolchain or MuPDF needed
to compile); constructors then return a "no backend" error. This build tag is
the seam for alternative backends — e.g. a future cgo-free WASM or pure-Go
engine slots in under the opposite tag with no change to the public API.

```sh
go build ./...                 # default: MuPDF backend
go build -tags nomupdf ./...   # compile with the engine excluded
```

## Subpackages

- **[`geometry`](./geometry)** — the 2D primitives (`Point`, `Rect`, `IRect`,
  `Matrix`, `Quad`) used throughout the API, with full affine algebra.
- **[`experimental`](./experimental)** — a higher-level, ergonomic layer:
  lifecycle-managed documents, a row/word model for layout-aware extraction,
  region and key/value queries (`ValueRightOf`, `ValueBelow`), located regex
  search, and helpers for rendering, image extraction, and merging PDFs/images.

## Acknowledgements

- **[MuPDF](https://mupdf.com)** by [Artifex Software](https://artifex.com) is
  the C library that does the heavy lifting — parsing, rendering, and text
  extraction. gomupdf is a thin Go layer over it; all credit for the underlying
  PDF engine belongs to the MuPDF authors.
- **[PyMuPDF](https://github.com/pymupdf/PyMuPDF)** and
  **[pdfplumber](https://github.com/jsvine/pdfplumber)** are the prior art that
  shaped this library's extraction and table-detection design. Thank you to
  their authors and maintainers.

## License

MuPDF is distributed under the **GNU AGPL-3.0**, and this package links against
it, so the same license applies (see [LICENSE](./LICENSE)). Review its
obligations before distributing binaries built with this package.
