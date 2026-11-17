# FORK.md

`github.com/tredy-ai/gomupdf` is a fork of
[`srijanmukherjee/gomupdf`](https://github.com/srijanmukherjee/gomupdf), a cgo
binding over MuPDF. It is the upstream of `cryptid-lemur`, a PII-redaction
service. This file says what the fork changes, what a caller can rely on, what
it cannot, and how to test and release it. It describes the code at `main`; the
doc comments on the exported APIs are the detailed reference and win over this
file if they ever disagree.

## Contents

1. [Upstream and base](#1-upstream-and-base)
2. [What the fork is for](#2-what-the-fork-is-for)
3. [Patch list](#3-patch-list)
4. [Behavior differences from upstream and stock MuPDF](#4-behavior-differences-from-upstream-and-stock-mupdf)
5. [Migration notes](#5-migration-notes)
6. [Using the package in a redaction service](#6-using-the-package-in-a-redaction-service)
7. [Known limitations](#7-known-limitations)
8. [Open decisions](#8-open-decisions)
9. [Testing](#9-testing)
10. [Releases and staying current with upstream](#10-releases-and-staying-current-with-upstream)

## 1. Upstream and base

- Upstream: https://github.com/srijanmukherjee/gomupdf
- Base tag: `v1.1.0`, commit `60bb6252c7550a56a9034315613cc29926034d9e`
  (annotated tag object `52b6633f2107f96cd3796041ee6f796c1b268962`).
- The patch series is applied on fork `main` at `f3b7339`, which is `v1.1.0`
  plus one upstream README-only commit ("docs: add Built with Claude Code badge
  to README"). Nothing from upstream has been merged since.
- Build and test target: Go 1.27 on Alpine 3.23 with its packaged MuPDF 1.26.11
  (apk `mupdf-dev`), dynamically linked, on musl. The code is guarded at compile
  time by MuPDF >= 1.25; CI and the committed corpus's known-failures list are
  for MuPDF 1.26.11 (see [MuPDF versions](#mupdf-versions) for what was run on
  which version).
- `LICENSE` (AGPL-3.0) is unchanged.

## 2. What the fork is for

A redaction service has to be sure that text it redacted is gone from the saved
file, and that text it did not see is not still there. Upstream gomupdf reads,
renders and writes PDFs well, but it reports success when MuPDF removes less
than asked, drops characters it cannot parse, and says nothing about the places
outside the page content where text survives. The fork keeps upstream's API
shape and adds per-character geometry, per-glyph redaction with a check after
it, a document-level scrub, layer handling, and the guards that stop a failure
being reported as success. Where MuPDF cannot be made to behave, the fork
detects it and returns an error.

## 3. Patch list

Every commit on top of `f3b7339`, in order (`git log --reverse --format='%h %s'
f3b7339..main`), grouped by theme. The groups are thematic, not chronological:
the order inside each group is the order in the history. "API impact" is
`None`, `New API`, `Behavior change` (same signatures, different results or
errors) or `BREAKING` (existing callers must change code or handle a new
failure). Nothing has been pushed or tagged yet. 64 commits.


### Fork and build

| Commit | Subject | API impact |
| --- | --- | --- |
| `151a285` | `chore(fork): rename module to github.com/tredy-ai/gomupdf` | BREAKING: import path is now github.com/tredy-ai/gomupdf |
| `2eb07d9` | `build: link Alpine's packaged MuPDF without a stub library` | None (cgo link flags) |
| `5cb35d8` | `feat: add MuPDFVersion and require MuPDF 1.25+` | New API: MuPDFVersion |
| `a3a3b33` | `build!: require Go 1.27 and test on Alpine 3.23 (MuPDF 1.26.11)` | BREAKING: module requires Go 1.27 |

### Text extraction

| Commit | Subject | API impact |
| --- | --- | --- |
| `8e924b3` | `fix(rawdict): never drop a character` | Behavior change: RawDict and Dict return an error instead of skipping records; unnamed-font characters are returned |
| `04f6a7f` | `feat(text): add RawDictWithOptions and AccurateBBoxes` | New API: RawDictWithOptions, TextOptions.AccurateBBoxes |
| `e6794c4` | `feat(rawdict): expose per-character quads and line direction` | New API: Char.Quad, TextLine.Dir, TextLine.WMode |
| `a2e8b43` | `feat(text)!: ignore /ActualText by default and add TextOptions.UseActualText` | BREAKING: extraction ignores /ActualText by default; new TextOptions.UseActualText |
| `b7133d0` | `docs(text): warn that UseActualText can crash MuPDF on some input` | None (doc comment and skipped test) |

### Open and save

| Commit | Subject | API impact |
| --- | --- | --- |
| `3e331b0` | `feat(open): open documents from a path without reading them into memory` | Behavior change: Open reads the file lazily; incremental save needs OpenStream |
| `bfc9a17` | `feat(save): stream SaveWithOptions to the file` | Behavior change: SaveWithOptions streams to a temp file and renames |
| `8da9893` | `fix(save): apply the umask to files SaveWithOptions creates` | Behavior change: new files honor the umask |
| `191ce1f` | `fix(save): keep the temp file private when replacing an existing file` | Behavior change: temp file replacing an existing file is created 0600 |
| `32f2da8` | `fix: repair damaged files before changing them` | Behavior change: every object is loaded at open, so repair happens first; a repaired file cannot be saved incrementally |
| `8739cc5` | `fix: do not load or change a locked document` | BREAKING: changing or saving a locked document is refused |
| `b0bb5ad` | `fix: refuse to read a locked document` | BREAKING: reading a locked document is refused; PageCount is 0 until Authenticate |
| `d381f88` | `fix(open): refuse a page count the file cannot hold` | Behavior change: Open, OpenStream, OpenAnyStream and Authenticate refuse a page count larger than the object count |

### Redaction

| Commit | Subject | API impact |
| --- | --- | --- |
| `4d8a36f` | `fix(redact)!: apply MuPDF redact options at apply time and draw the box` | BREAKING: ApplyRedactions takes ApplyOptions; RedactOptions.Fill and RemoveImages removed |
| `998339b` | `feat(redact): add AddRedactionQuads` | New API: AddRedactionQuads |
| `5ab6cc0` | `feat(redact): remove text per glyph against the quad shape` | Behavior change: text removed per glyph against the quad shape |
| `f0ed073` | `fix(redact): build the glyph filter against MuPDF 1.27+` | None (builds against MuPDF 1.27+) |
| `3558a40` | `fix(redact): remove every glyph extraction reports under a redaction` | Behavior change: every glyph extraction reports under a redaction is removed |
| `ad7445a` | `fix(redact): strip declared text from marked content on redacted pages` | Behavior change: /ActualText, /Alt and /E removed from redacted pages |
| `f3e47b0` | `fix(redact): detach redacted forms from the structure tree` | Behavior change: redacted forms detached from the structure tree |
| `6ecd3d8` | `fix(redact): follow object references to replaced forms` | Behavior change: object references to replaced forms followed |
| `706780b` | `fix(redact): do not remove the glyph after a ligature or zero-advance mark` | Behavior change: the glyph after a ligature or zero-advance mark is kept |
| `ab93668` | `fix(redact): reject an empty redaction rectangle` | BREAKING: AddRedaction rejects an empty or non-finite rect |
| `c7a7850` | `fix(redact): verify the page after applying redactions` | New API: RedactionCheckError, ErrTextRemains, ErrTextMoved; ApplyRedactions verifies the page |
| `311cfc0` | `fix(redact): only fail on moved text next to a redaction` | Behavior change: the check fails only on redacted text still present or moved |
| `198982b` | `fix(redact): accept an inverted redaction rectangle` | Behavior change: an inverted rect is accepted again (normalised) |
| `066481a` | `fix(redact): say to discard the document after a failed redaction` | New API: ErrRedactionFailed; saves fail after a failed redaction |
| `73d666f` | `fix(redact): fail when more text remains than was kept` | Behavior change: the check also fails when more text remains than was kept (RedactionCheckError.Excess) |
| `0055d6e` | `fix(redact): do not count a kept ligature's tail as redacted text` | Behavior change: a kept ligature's tail next to a redaction no longer fails the post-redaction check |

### Scrub

| Commit | Subject | API impact |
| --- | --- | --- |
| `c7e5b64` | `feat(scrub): add Document.Scrub and Document.HiddenContent` | New API: Document.Scrub, Document.HiddenContent, ScrubOptions, HiddenContent |
| `a2d49fb` | `fix(scrub): remove JavaScript and Launch actions from kept links, outlines and form fields` | Behavior change: JavaScript and Launch actions removed from kept links, outlines and fields |
| `8e19639` | `feat(scrub): add ScrubOptions.KeepFormFields` | New API: ScrubOptions.KeepFormFields |
| `e1357c0` | `fix(scrub): remove XFA and the remaining script carriers` | Behavior change: XFA and other script carriers removed; new HiddenContent.XFA |
| `e55c2af` | `fix(scrub): keep working links in very large files and close two script-action gaps` | Behavior change: action budget scales with file size; two script-action gaps closed |
| `dfb6a4a` | `fix(scrub): remove /Info, shared-field scripts and /AF that real files kept` | Behavior change: /Info, shared-field scripts and /AF in any object removed |
| `3feba9c` | `fix(scrub): drop non-standard trailer keys` | Behavior change: non-standard trailer keys removed; new HiddenContent.TrailerKeys |
| `3d5f985` | `fix(scrub): remove pages that are not in the page tree` | Behavior change: pages outside the page tree emptied; new HiddenContent.OrphanPages |
| `6915e25` | `fix(scrub): remove private data and signature remnants from every object` | Behavior change: private data and signature remnants removed from every object; new HiddenContent fields |
| `bad5c7d` | `fix(scrub): keep the document savable after destroying objects it created` | None (document stays savable on MuPDF 1.25) |
| `daadd6e` | `fix(scrub): remove annotations and fields nothing displays` | Behavior change: annotations and fields on no page removed |
| `989f10c` | `fix(scrub): destroy removed annotations that a kept action targets` | Behavior change: annotations a kept action targets are destroyed |
| `5874ed1` | `fix(scrub): strip and report scripts on form nodes on no page` | Behavior change: scripts on form nodes on no page stripped and reported |
| `8ae9ebc` | `fix(scrub): empty orphan pages completely and treat uncounted pages as orphans` | Behavior change: orphan pages emptied completely; pages MuPDF does not serve are orphans |
| `b762bd9` | `fix(scrub): small corrections from review` | Behavior change: null trailer keys removed; kept widget appearances kept; /IRT no longer keeps an annotation |
| `eea128e` | `fix(scrub): keep every annotation under KeepAnnotations` | Behavior change: KeepAnnotations keeps every annotation, including those on pages MuPDF does not serve |
| `eaa11f5` | `fix(scrub): do not fail HiddenContent on a page tree MuPDF accepts` | Behavior change: HiddenContent and Scrub tolerate a page tree with an unloadable kid |

### Layers

| Commit | Subject | API impact |
| --- | --- | --- |
| `b696b6c` | `feat(layers): report optional content and add Document.ShowAllLayers` | New API: Document.ShowAllLayers, HiddenContent.Layers and HiddenLayers |
| `05f7509` | `fix(layers): count hidden groups in object streams and inline dictionaries` | Behavior change: HiddenLayers counts groups in object streams and inline dictionaries and fails closed |

### Hardening from review

| Commit | Subject | API impact |
| --- | --- | --- |
| `c41f87a` | `fix(redact): bound the redaction region list` | Behavior change: redaction region list bounded |
| `aaf0769` | `fix(redact): refuse a page with too many redaction regions` | Behavior change: a page with more than 65536 redaction regions is refused |
| `a5bfe66` | `fix(redact): check structure limits before changing the page` | Behavior change: structure limits checked before the page changes |
| `f62eb6b` | `fix(redact): never leave the private form marker in a file` | None (private form marker never left in the file) |
| `f021198` | `fix: never return an empty error message` | Behavior change: errors never have an empty message; out-of-memory is reported |
| `9eafc6e` | `fix: do not retry a broken object stream for every object` | None (Open no longer quadratic on a broken object stream) |
| `653d4c6` | `fix: report errors with an empty message from font and image helpers` | Behavior change: an error with an empty message is no longer read as "nothing found" by font and image helpers |
| `8cc4c9e` | `fix(scrub): do not re-parse unloadable objects in the sweep` | None (sweeps skip unloadable objects; no more minutes-long HiddenContent, Scrub, ShowAllLayers on broken object streams) |

### Tests

| Commit | Subject | API impact |
| --- | --- | --- |
| `3fa95bf` | `test: add a corpus invariant test and fuzz target` | None (TestCorpus, FuzzPipeline) |
| `46bba5a` | `test: add a curated PDF corpus` | None (committed corpus) |
| `8deacf5` | `test: report redacted words left after scrub in the corpus test` | None (corpus test reports words left after Scrub) |

### Documentation

| Commit | Subject | API impact |
| --- | --- | --- |
| `a0c1781` | `docs: describe the fork as it is` | None (FORK.md and README.md rewritten) |

## 4. Behavior differences from upstream and stock MuPDF

Rules a caller can rely on, by area. "Stock MuPDF" is MuPDF 1.26.11 unless
stated. The exported doc comments carry the exact limits and the corner cases.

### 4.1 Extraction

- Extraction returns the glyphs actually drawn. `/ActualText` is ignored by
  default, in every extraction API. `TextOptions.UseActualText` turns it back
  on for `ExtractText` and `RawDictWithOptions` only; the other APIs (`GetText`,
  `Words`, `Lines`, `Spans`, `StructuredText`, `StructuredJSON`, `HTML`,
  `XHTML`, `XML`, `Search`, `SearchRects`, `SearchWith`, `FindTables`, `Dict`)
  have no toggle. To get both views, extract twice.
- MuPDF's `FZ_STEXT_IGNORE_ACTUALTEXT` alone still appends the declared text on
  1.26.11, so the package builds the structured-text page itself
  (`gomupdf_stext.h`) and drops the device's metatext callbacks. Tested on
  1.26.11 only.
- With `UseActualText` set, MuPDF 1.26.11 can crash the whole process
  (SIGSEGV, not a Go error) on a specific span shape. Run that extraction
  only where a crash is contained (`TextOptions` doc comment;
  `TestActualTextCrashInput`).
- `RawDict` and `Dict` never skip a character. A character whose font has no
  name is returned (`Font` is `""`), an invalid code point becomes U+FFFD, and
  output the parser cannot read is an error naming the line.
- `RawDictWithOptions(TextOptions)` takes the same options as `ExtractText`;
  `RawDict()` is `RawDictWithOptions(TextOptions{})`. Pin your options and keep
  them fixed if you map text offsets to geometry. `FZ_STEXT_CLIP` is never set:
  text outside the page box is returned unless you pass `Clip`, which keeps
  the characters whose box intersects it.
- `Char` carries `Quad` (MuPDF's per-character quad; `BBox` is its bounds),
  `TextLine` carries `Dir` (unit baseline direction) and `WMode` (0 horizontal,
  1 vertical). Coordinates are page space (points, y down, page rotation
  applied), the space `AddRedaction` and `AddRedactionQuads` take.
- Content on hidden optional-content layers is not extracted (see 4.6).
- Form field values, link targets and annotation contents are not part of page
  text. Read them with `Page.Widgets`, `Page.Links` and `Page.Annotations`.
- `MuPDFVersion()` returns the version of the headers the binary was compiled
  against (`""` under `-tags nomupdf`). Extraction output depends on it.

### 4.2 Opening, repair and locked documents

- `Open` does not read the file into memory: MuPDF reads it on demand until
  `Close`. The file must not be modified while the document is open.
  `OpenStream` keeps the bytes in memory.
- Every object of a PDF is loaded once at open (after `Authenticate` for a
  locked one), in `Open`, `OpenStream` and `OpenAnyStream`. MuPDF repairs a
  damaged cross-reference table lazily and discards in-memory edits when it
  does; loading everything first makes any repair happen before anything is
  read or changed. Cost: about 1 microsecond and 0.5 KB per object (39 ms and
  17 MB for 40,000 objects); streams are not decoded.
- If a repair still happens during `AddRedaction`, `AddRedactionQuads`,
  `ApplyRedactions` or a save, the call returns an error saying so, instead of
  succeeding without the edits.
- A file MuPDF had to repair cannot be saved incrementally (`SaveOptions`
  doc comment); the refusal comes from MuPDF.
- A document that needs a password answers only `IsEncrypted`, `NeedsPass`,
  `Authenticate` and `Close` until `Authenticate` succeeds. `PageCount` is 0,
  `LoadPage` returns an error, `Pages` yields nothing, and every read, change,
  save, `Scrub`, `HiddenContent`, `ShowAllLayers` and `ConvertToPDF` returns an
  error. Reason: MuPDF decrypts each object once, when first loaded, and
  caches it, so any read with the wrong key garbles that object for good.
- `Authenticate` loads every object on success; if that fails it returns false
  and the document stays locked.
- `Open`, `OpenStream` and `OpenAnyStream` (and `Authenticate`, for a locked
  file, which then fails) refuse a page tree that claims more pages than it can
  serve, with an error naming the rule (`page tree declares N pages, ...`). A
  declared count within the file's number of objects is accepted as before, at
  no cost. A larger one (possible only with direct page dictionaries in `/Kids`
  or a page or node listed more than once) is accepted only if it is at most
  1,048,576 and MuPDF builds its page map from the tree at open with every page
  a dictionary; MuPDF then serves the pages the tree holds. A count above
  8,388,607 (MuPDF's maximum object number) is refused whatever the file. The
  page map takes 16 bytes per declared page (16 MiB at 1,048,576, 22 GB for a
  `/Count` of 1,410,065,407), and a count MuPDF cannot serve would otherwise
  cost a lookup per declared page in every `HiddenContent` and `Scrub` call.
  `HiddenContent` and `Scrub` apply the same rule. A page count MuPDF cannot
  read at all is left to fail where it is used.
- Errors from the C helpers never have an empty message; a helper that fails
  without one reports `unknown error (MuPDF gave no message)`. Out-of-memory
  is reported with the size asked for.
- `Scrub`, `HiddenContent` and `ShowAllLayers` return an error for non-PDF
  documents.

### 4.3 Saving

- `SaveWithOptions` (non-incremental, unencrypted) is streamed by MuPDF to a
  temporary file (`.gomupdf-*`) in the destination directory and renamed over
  the destination on success. A failed save leaves no partial file. Incremental
  and encrypted saves, and `SaveBytes*`, are built in memory.
- The saved file's mode is the replaced file's mode, or 0644 with the process
  umask applied for a new file. A temp file that will replace an existing file
  is created 0600, so the document is never more readable than before.
- Saving to the file a document was opened from by `Open` is an error. A
  symlink destination is written through; a dangling one is an error.
- A save returns an error if MuPDF repairs the cross-reference table while
  saving.
- After `ApplyRedactions` fails once the page had started to change, every save
  of that `Document` returns an error wrapping `ErrRedactionFailed`
  (`SaveBytesWithOptions`, `SaveWithOptions`, `SaveEncryptedBytes`, and the
  functions built on them, and `ConvertToPDF`) until it is closed.
- `SaveOptions.Incremental` needs a document opened with `OpenStream` and is
  incompatible with `Garbage`, `Linear` and `Encrypt`.
- The zero `SaveOptions` writes an uncompressed file with no garbage
  collection; `DefaultSaveOptions()` is `Garbage: 3, Deflate: true`.
- Pixel redaction of images makes MuPDF store them uncompressed; the save of a
  990 KB file needed 306 MB. Budget memory for a save accordingly.

### 4.4 Redaction

**Adding.** `AddRedaction(rect)` normalizes an inverted rect. It returns an
error for a rect with zero width or height or a NaN or infinite coordinate
(after conversion to float32); use `AddRedactionQuads` for zero-area glyph
boxes. `AddRedactionQuads(quads)` creates one redaction annotation whose
`/QuadPoints` are the quads; an empty slice or a non-finite coordinate is an
error, zero-area quads are accepted. Nothing is removed until
`ApplyRedactions`. `RedactOptions` has no fields.

**What `ApplyRedactions(ApplyOptions)` applies.** Every redaction annotation on
the page, including ones already in the file. The zero `ApplyOptions` draws
the black box, blanks only the covered pixels of images (`ImagePixels`), leaves
line art alone and removes text. `NoBox`, `ImageRemove`, `ImageNone`,
`LineArtRemoveIfCovered`, `LineArtRemoveIfTouched` select the other modes;
`TextNone` is insecure and exists for tests. The box is always black. An
out-of-range enum value is an error.

**Removal rule (`TextRemove`).** Text is removed glyph by glyph before MuPDF
does the rest. A glyph goes when either:

1. its box (the font's own advance by ascender and descender), shrunk towards
   its centre by a tenth of each side, overlaps a quad's shape, or its shrunk
   axis-aligned bounds overlap a rect redaction (stock MuPDF's rule for rects);
   or
2. text extraction (as `RawDict` does it by default) puts a character at the
   glyph's origin whose shrunk quad overlaps a redaction.

So every character `RawDict` reports under a redaction is removed, for rects
and quads alike, and on rotated or skewed text a quad redaction removes the
word and keeps its neighbours. Stock MuPDF tests each quad's bounding
rectangle, which removes neighbours too. A quad that is not strictly convex
with its corners in order is tested by its bounds, as stock MuPDF does.
Removed glyphs leave a gap; the rest of the text does not move. Ligatures and
zero-advance marks: the glyph after a redacted ligature or mark stays; a
ligature goes when its first component's quad is redacted. In vertical writing
MuPDF 1.26 places each quad one advance past the glyph's ink, so redacting a
vertical character also removes the next one.

**Declared text.** With `TextRemove` and at least one redaction, `/ActualText`,
`/Alt` and `/E` are removed from the marked content the redaction reaches on
that page (inline and named property lists, in the page and its forms) and from
the structure elements that own it, up to the structure tree root. Stock MuPDF
leaves them. The cost is the page's accessibility text: a structure ancestor
spanning other pages loses its declared text for them too, and a named property
dictionary other pages share is copied for this page and kept for them.

**Forms and the structure tree.** Redaction makes the page draw redacted copies
of its form XObjects. A structure element that still named the original form
(`/Stm`, `/Obj`) would keep the unredacted original in the file; the fork
repoints such references to the copy, or drops them.

**What it reaches.** The page's content streams and the form XObjects they
draw. It does not reach tiling patterns, soft-mask groups, Type 3 glyph
procedures or annotation appearances; stock MuPDF does not either.

**Verification.** After redacting with `TextRemove`, the page is extracted again
(one more extraction per page) and `ApplyRedactions` returns a
`*RedactionCheckError` (page number and counts `Remaining`, `Moved`,
`MovedElsewhere`, `Excess`) when:

- a redacted character is still extracted under a redaction (`errors.Is`
  `ErrTextRemains`); or
- a redacted character reappears outside every redaction, moved, on its line
  nearby (`ErrTextMoved`); or
- the page has more characters than the redactions kept (`ErrTextMoved`,
  `Excess`), which is what a form that uses the page's fonts without its own
  `/Resources` produces (MuPDF measures it with a fallback font).

The count rule counts real characters (spaces and zero-advance
characters are not counted), so one more than the redactions kept is an error
whatever the extra character is or where it went. Its limit: glyphs wrongly
removed elsewhere on the same page offset survivors one for one, which is why
the first two rules stay.

Kept text that moved elsewhere is counted in `MovedElsewhere` and is not an
error by itself. Spaces are not checked. A character with no advance where the
glyph before it ends (the "i" of a "fi" ligature, a mark on its letter) belongs
to that glyph and is judged with it, before and after redacting: a kept "fi"
right before a redaction is no error although its "i" lies on the redaction's
edge, and a mark on a redacted letter must go too. `TextNone` is not verified.

**Limits and refusals (nothing changes, redactions stay, the error names the
page).** More than 65,536 redaction regions on a page (each quad, plus one per
rect); form XObjects nested more than 32 deep or reached by more than 65,536
paths; a `/ParentTree` more than 64 levels deep; a structure tree or
`/ParentTree` larger than 16 nodes per object plus 65,536. A page with more than
1,048,576 extracted characters under its redactions falls back to MuPDF's own
text removal (still verified, except for the moved-next-to-redaction rule).

**What an error means for the Document.** The refusals above change nothing; a
retry returns the same error. Any other error from `ApplyRedactions` (the
check, a repair, a MuPDF failure while rewriting, out of memory) can come after
the page started to change: the page is half redacted and its redaction
annotations may be gone. Discard the `Document` and reopen the original or
reject the file. Saves then fail with `ErrRedactionFailed`.

### 4.5 Scrub and HiddenContent

`Document.Scrub(ScrubOptions)` removes the document-level places where content
survives page redaction. `Document.HiddenContent()` reports what it would
remove without editing; its counts saturate at the C int maximum. The exact
locations and limits are in the doc comments.

**Removed by default:** trailer `/Info` and non-standard trailer keys; catalog
`/Metadata`, `/AA`, `/AF`, `/PieceInfo`, `/LastModified`, `/SpiderInfo`,
`/NeedsRendering`, `/Perms`, `/DSS`; `/Names` entries `EmbeddedFiles`,
`JavaScript`, `IDS`, `URLS`; a catalog `/OpenAction` that is or chains to a
JavaScript or Launch action; `/Outlines`; page and page-tree-node `/Thumb`,
`/AA`, `/AF`, `/Metadata`, `/PieceInfo`, `/LastModified`; `/Metadata`,
`/PieceInfo` and `/LastModified` on images, forms, fonts, font descriptors,
structure elements, output intents, ICC profiles and embedded files; `/AF`
file specifications on any object; every annotation except Links (popups and
replies included), every widget and the catalog `/AcroForm`; `/AcroForm /XFA`
always; annotations and fields that are on no page; and everything in the page
dictionaries MuPDF does not serve (orphan pages, below). Scripts: on every annotation and
form node that stays, `/AA` and any `/A` or `/PA` that is or chains to a
JavaScript or Launch action.

**Orphan pages.** The pages MuPDF serves (the `PageCount` pages) are
authoritative. Any other page dictionary is an orphan: one outside the page
tree, or in it but not served (a `/Kids` entry past a `/Count` that is too
small, or a damaged file's extra pages). `Scrub` empties orphans completely
(`/Contents`, `/Resources`, `/Parent`, and the forms and images their own and
inherited resources reach, down through nested forms), keeps the dictionary
page-shaped for references to it, and `HiddenContent.OrphanPages` counts them.
A page MuPDF cannot look up in a damaged or cyclic page tree is skipped, not an
error. A page tree that claims more pages than it can serve is refused (4.2).

**Options:**

| Option | Keeps |
| --- | --- |
| `KeepAnnotations` | Every annotation (including those on pages MuPDF does not serve) and widget and the whole `/AcroForm` (and `/Perms`, `/DSS`, signature dictionaries), with scripts removed. `/XFA` still goes. Embedded media in kept annotations stay (a FileAttachment's file, Sound, Movie, Screen rendition, RichMedia assets, 3D streams). An annotation reachable only through an outline item's action (a GoTo3DView) is kept by Scrub but unreferenced when outlines are removed, so a garbage-collecting save drops it; keep outlines to keep it. |
| `KeepFormFields` | Widgets and the `/AcroForm` field tree, with scripts removed and the signature remnants kept as above. Other non-Link annotations still go. Hidden template pages (`/Names/Templates`) are emptied whatever the options, so templates spawn blank pages. You must inspect form values yourself (`Page.Widgets`); they are not reliably in page text. |
| `KeepOutlines` | `/Outlines`. Titles and `/Dest` stay; an `/A` that is or chains to a JavaScript or Launch action goes. |

**Destruction, not unlinking.** Removed objects that may be reachable from
elsewhere (a structure tree's `/OBJR`, an `/AF`) are destroyed, so surviving
references resolve to null. Scrub first marks everything kept content depends
on and never destroys a marked object. Other things (appearance streams, URI
and other actions, `/VRI`, parts beyond the walk limits) are only unlinked and
leave the file only if nothing else references them, which is why a
garbage-collecting save matters.

**HiddenContent** reports the same locations: booleans (`Info`, `XMP`,
`AcroForm`, `XFA`, `Outlines`, `JavaScript`, `Perms`, `DSS`, `WebCapture`) and
counts (`EmbeddedFiles`, `AssociatedFiles`, `NonLinkAnnotations`, `Widgets`,
`Thumbnails`, `PageMetadata`, `PieceInfo`, `TrailerKeys`, `OrphanPages`,
`ObjectMetadata`, `LastModified`, `Layers`, `HiddenLayers`). When a walk hits a
limit the field fails closed (`EmbeddedFiles` at least 1, `JavaScript` true).
It loads every object first, so `PageCount` can change if the file needed
repair. Between `ApplyRedactions` and `Scrub` (or a save and reopen) it counts
each applied redaction annotation, which MuPDF unlinks but does not delete, as
a `NonLinkAnnotations` entry. Trailer keys with a null value are counted and
removed. After `Scrub`, a garbage-collecting save and a reopen, everything it
reads except `Layers` and `HiddenLayers` is zero when the matching `Keep*`
option was not set (the locations a `Keep*` option keeps still read non-zero);
that is how to verify the output.

**Discard on error.** If `Scrub` returns an error the document must not be
treated as scrubbed: it may have stopped partway, and every later `Scrub` call
on it returns an error. Discard it.

**Save requirement.** `Scrub` is meaningful only followed by a non-incremental
save with `Garbage >= 1`, which drops unreferenced objects. An incremental save
keeps the original bytes.

### 4.6 Layers

Content hidden by the default optional-content configuration (an `/OFF` group,
a group hidden by usage or intent, a membership dictionary whose policy hides
it) is never extracted, searched, rendered or redacted, stays in the file, is
copied unchanged into a redacted page, and can be switched on by a viewer's
user. `Scrub` leaves layers alone.

`Document.ShowAllLayers()` deletes `/OCProperties` and destroys the layer
dictionaries (strictly typed `/OCG` and `/OCMD`, so their names do not stay in a
saved file). The spans that referred to them now refer to nothing, so all
content is ordinary and visible, in this `Document` and in a saved and
reopened copy. Call it before extraction and detection. Cost: the page shows
every layer at once (all language alternatives drawn over each other, say) and
annotations with `/OC` become visible.

`HiddenContent.Layers` is the number of groups `/OCProperties` lists; with
`Layers == 0` MuPDF hides nothing. `HiddenLayers` is 0 when `Layers` is 0;
otherwise it is the number of groups and membership dictionaries MuPDF hides
for viewing. It reads indirect ones (in object streams too) and direct ones
written as an object's own `/OC` value or in a `/Properties` dictionary, and
it **fails closed**: it is at least 1 when any of those is a membership
dictionary, a group with `/Usage` or with an `/Intent` other than `/View`, or a
direct one, even if it shows. `HiddenLayers == 0` with `Layers > 0` therefore
means every group those places hold is an indirect plain group the
configuration shows.

The one documented gap: it does not read content streams, so a group or
membership dictionary written inline as a marked-content property list
(`/OC << ... >> BDC`) is not seen, and MuPDF honors it. MuPDF also does not
evaluate `/VE` visibility expressions and shows their content, so
`HiddenLayers == 0` never meant that no viewer hides anything. **Rule for
callers: only `Layers == 0` rules out hidden content. If you need certainty,
call `ShowAllLayers` whenever `Layers > 0`.**

## 5. Migration notes

For anyone coming from upstream `v1.1.0`. Each item is a breaking change or a
change a caller can trip over.

### 5.1 `ApplyRedactions` takes `ApplyOptions`; `Fill` and `RemoveImages` are gone

MuPDF applies redaction options per page at apply time, so they moved from
`RedactOptions` to an argument of `ApplyRedactions`.

```go
// before
page.AddRedaction(rect, gomupdf.RedactOptions{RemoveImages: true})
n, err := page.ApplyRedactions()

// after
err := page.AddRedaction(rect, gomupdf.RedactOptions{})
n, err := page.ApplyRedactions(gomupdf.ApplyOptions{Images: gomupdf.ImageRemove})
```

- `ImageRemove` drops overlapping images whole (what upstream always did,
  whatever the field said); `ImagePixels` (the zero value) blanks only the
  covered pixels; `ImageNone` leaves images alone.
- `RedactOptions.Fill` is gone: MuPDF's box is always black and ignores the
  annotation's colour (observed on 1.25.6; not re-checked on later versions), so
  the field never did anything. `ApplyOptions.NoBox` suppresses the box.
- The zero `ApplyOptions` draws the box (upstream drew none), removes text and
  leaves line art alone.

### 5.2 Text extraction ignores `/ActualText` by default

Output changes for PDFs that use `/ActualText`.

```go
// before: MuPDF's default, the declared text
s, err := page.ExtractText(gomupdf.TextOptions{})

// after: drawn glyphs; opt back in (ExtractText and RawDictWithOptions only)
s, err := page.ExtractText(gomupdf.TextOptions{UseActualText: true})
```

Read the crash warning in 4.1 before enabling it on untrusted input.

### 5.3 Go 1.27

`go.mod` says `go 1.27`.

### 5.4 `Open` repairs at open; a repaired file refuses incremental save

Nothing to change unless you save incrementally. A file MuPDF repaired cannot
be saved incrementally; fall back to a full save.

```go
// after
if err := doc.SaveIncremental(path); err != nil {
	err = doc.SaveWithOptions(path, gomupdf.SaveOptions{Garbage: 1})
}
```

`Open` also no longer reads the file into memory (an incremental save needs
`OpenStream`), and `SaveWithOptions` streams to the file; see 4.2 and 4.3.

### 5.5 Locked documents answer nothing until `Authenticate`

```go
// before: reads before Authenticate "worked" and could garble strings for good
doc, _ := gomupdf.Open("locked.pdf")
n := doc.PageCount() // was the real count; now 0

// after
doc, err := gomupdf.Open("locked.pdf")
if err != nil { /* ... */ }
if doc.NeedsPass() && !doc.Authenticate(pw) { /* wrong password */ }
n := doc.PageCount()
```

`OpenWithPassword` does both steps.

### 5.6 `AddRedaction` rejects an empty rect

A rect with zero width or height, or a non-finite coordinate, is an error
(upstream accepted it and removed next to nothing). The union of `Char.BBox` of
glyphs with no box height (some Type 3 fonts) is such a rect.

```go
// before
page.AddRedaction(wordRect, gomupdf.RedactOptions{}) // silently removed ~nothing

// after: redact by character quads
err := page.AddRedactionQuads(quads, gomupdf.RedactOptions{})
```

### 5.7 Saves fail after a failed redaction

```go
if _, err := page.ApplyRedactions(gomupdf.ApplyOptions{}); err != nil {
	doc.Close() // discard; saving would return an error wrapping ErrRedactionFailed
	return err
}
```

Also new and error-visible: `ApplyRedactions` can return `*RedactionCheckError`
(4.4), `RawDict` returns errors instead of skipping records, and errors are
never empty.

## 6. Using the package in a redaction service

This section is guidance derived from the findings above and from the corpus
run. It is not a guarantee of the package.

1. **Isolate each document in a worker process with memory and time limits.**
   MuPDF can hang for minutes on a few tiny crafted files (pdf.js
   `operator_list_cycle.pdf`, `tiling-pattern-tiny-steps*.pdf`), can crash with
   `UseActualText`, and can allocate hundreds of megabytes (a pixel-redacted
   990 KB file needed 306 MB). `TestCorpus` runs each file in a child process
   with a 3 GiB address-space cap and a timeout for the same reason.
1a. **An encrypted input must be authenticated before anything else is called**
   (`NeedsPass`, then `Authenticate`, or `OpenWithPassword`): until then
   `PageCount` is 0 and every read, change and save returns an error, and a
   read before it would garble strings for good.
2. **Inspect first.** Call `HiddenContent`. Layers: if `Layers == 0` nothing is
   hidden. If `Layers > 0` and you need certainty, call `ShowAllLayers` before
   extraction and detection (every layer is then drawn at once); or refuse the
   file. `HiddenLayers` alone is not enough (inline `/OC << >> BDC` property
   lists in content streams are not counted). See
   [Open decisions](#8-open-decisions) for the owner's choice.
3. **Extract** with `RawDictWithOptions` and a fixed `TextOptions`. Also run
   detection on form field values (`Page.Widgets`), link targets
   (`Page.Links`) and annotation contents (`Page.Annotations`): none of them are
   in page text.
4. **Redact** with `AddRedactionQuads` from the characters' `Quad`, then
   `ApplyRedactions`.
5. **Treat any error from `ApplyRedactions` or `Scrub` as "discard this
   Document"** (4.4, 4.5).
6. **Scrub**, then **save non-incrementally with `Garbage >= 1`**, with
   compression in production (`DefaultSaveOptions()`; the zero value's
   uncompressed output is for tests).
7. **Verify** by reopening the output and extracting again, in both
   `/ActualText` modes if the process isolation allows (the `UseActualText`
   pass can crash), and by `HiddenContent` on the reopened file. Reject on any
   leftover.

Error handling around step 4 and 5:

```go
_, err := page.ApplyRedactions(gomupdf.ApplyOptions{Images: gomupdf.ImageRemove})
switch {
case errors.Is(err, gomupdf.ErrTextRemains), errors.Is(err, gomupdf.ErrTextMoved):
	// the page is not redacted; reject the file, discard the Document
case errors.Is(err, gomupdf.ErrRedactionFailed):
	// returned by a later save of a Document whose redaction failed
case err != nil:
	// any other error: the page may be half redacted; discard the Document
}
```

Compilable snippets for steps 2 to 6 are in the README ("Redact", "Scrub & hidden
content", "Optional content (layers)").

## 7. Known limitations

Accurate as of `main`.

**What still survives redaction + default `Scrub` + a garbage-collecting save**,
by design or as an open decision:

- text on hidden layers, when `ShowAllLayers` is not called;
- link URIs, and the `/Contents` of Link annotations that are kept;
- accessibility structure attributes other than those stripped on redacted pages
  (`/ID`, `/Summary`, `/T`, and `/Alt`, `/ActualText`, `/E` on unredacted pages);
- optional-content group names;
- named-destination and name-tree keys;
- font names and subset `/CharSet`;
- the document `/ID`.

**What redaction cannot reach (the service's own checks or process isolation
must contain it).** MuPDF's redaction never visits tiling patterns, soft-mask
groups or Type 3 glyph procedures, and mis-measures a form XObject that uses a
font from the page's resources. The fork detects the leftovers after applying
and returns an error; it does not remove them.

**Other MuPDF behaviors, all verified on 1.26.11:**

- `UseActualText` can crash MuPDF (NULL font in the structured-text device) on a
  specific span shape.
- MuPDF can hang for minutes in extraction or rendering on a few tiny crafted
  files.
- Pixel redaction of images makes MuPDF store them uncompressed (306 MB from
  990 KB at save).
- Vertical-writing quads sit one advance past the ink, so redacting a vertical
  character also removes the next one.
- `pdf_drop_ocg` leaves a dangling pointer; `ShowAllLayers` clears it.
- Deleting an object created in the same session breaks saves on 1.25.6; Scrub
  leaves an empty dictionary in its place.

**Corpus results (final run, at `d381f88`, MuPDF 1.26.11 on Alpine 3.23, Go 1.27).**
5,347 public test and real-world PDFs from the pdf.js, veraPDF, qpdf, PyMuPDF,
Poppler, corkami and PDF Association suites (kept outside the repo), plus the
163 committed regression files. Each file is opened, extracted, redacted by
character quads and by rectangle, saved, reopened and checked, then scrubbed in
three modes and compared with an unscrubbed save.

| Outcome | Files |
|---|---|
| Passed every check | 5,189 |
| Rejected at open | 30 |
| Need a password | 46 |
| Broke a cosmetic invariant (see below) | 77 |
| Exceeded the 2-minute limit inside MuPDF | 4 |
| Crashed MuPDF, only with `UseActualText` | 1 |
| Redacted text extracted after a successful redaction and save | **0** |

- **Refusals.** On 23 files the library refused a redaction it could not
  guarantee: text in soft masks, transparency groups or patterns; glyphs MuPDF's
  rewriter measures wrongly; empty rectangles; the per-page region limit. Every
  save after a failed `ApplyRedactions` was refused.
- **Cosmetic side effects.** Of the 2,421 files that had text to redact, 75
  (3.1%) show a side effect outside the redaction areas: rendering changes on
  61, neighbouring characters lost on 28, kept text shifted on 8. These come
  from MuPDF's content rewriter on unusual constructs. This is the observed
  rate, not a bound.
- **Layers.** 71 files have optional content; `ShowAllLayers` revealed words on
  6 of them and lost none. 48 of the 71 report `HiddenLayers >= 1`.
- **One file cannot be saved after a rectangle redaction**: pixel redaction makes
  MuPDF store its images uncompressed, and the uncompressed save needs 306 MB.
- **What still sits in the bytes after redaction, default `Scrub` and a
  garbage-collecting save** (a redacted word found somewhere in 40 files): font
  programs, signatures and name tables (mostly coincidental matches), link
  `/URI` strings and the `/Contents` of kept Link annotations, structure-tree
  `/ID`, `/Summary` and `/T`, layer names, named-destination keys, and
  hidden-layer content when `ShowAllLayers` is not called. See sections 7 and 8.

**Gaps that remain in layers.** `HiddenLayers` does not read content streams: an
inline `/OC << >> BDC` property list is not counted (section 4.6). Call
`ShowAllLayers` whenever `Layers > 0` if you need certainty.

**Platform.**

- Encrypted and incremental saves, and `SaveBytes*`, are built in memory.
- See [MuPDF versions](#mupdf-versions). On MuPDF 1.28.6 (Homebrew, macOS), at
  last check (commit `f0ed073`), the package built and three tests failed:
  `TestRawDictWithOptionsDehyphenate` (1.28 dehyphenates differently) and two
  per-glyph redaction cases where the right glyphs are removed but following
  characters shift. 1.28 has not been re-run since many later changes.
- The `mupdf_bundled` build tag (static `libmupdf.a` + `libmupdf-third.a`) was
  built and tested once against MuPDF 1.25.6 from source and passed.
- The GitHub Actions workflow has never run: nothing has been pushed.

## 8. Open decisions

Both belong to the owner. Neither is decided; the package does not pick one.

1. **Hidden layers.** What a service should do with them:
   - refuse any file with `HiddenContent.Layers > 0` (or `HiddenLayers > 0`);
   - call `ShowAllLayers` before detection, accepting that every layer is then
     drawn and redactable at once; or
   - a future "delete hidden content" operation that removes what the
     configuration hides, which does not exist.
3. **Raising the compile guard to MuPDF 1.26.** Today the guard allows 1.25.
   Raising it drops 1.25.6, on which `TestCorpus` already fails because some
   corpus files behave differently there, and makes the supported set match
   what CI and the known-failures list cover; keeping it lets consumers on 1.25
   (older distributions) build, with a corpus test that cannot be run clean.
2. **The accessibility structure tree.** Whether `Scrub` should drop or strip
   it. Today it keeps the tree (except the declared text stripped on redacted
   pages) and so keeps `/ID`, `/Summary`, `/T` and the other attributes listed
   in section 7.

## 9. Testing

**The gate, in Docker** (the same environment as CI: Alpine 3.23, MuPDF
1.26.11, Go 1.27). Run it from the repository root:

```sh
docker run --rm -v "$PWD":/src -w /src -e GOFLAGS=-buildvcs=false golang:1.27-alpine3.23 sh -c '
  apk add --no-cache build-base pkgconf mupdf-dev &&
  test -z "$(gofmt -l .)" &&
  go vet ./... &&
  go build ./... &&
  go build -tags nomupdf ./... &&
  go test -covermode=atomic -coverprofile=/tmp/coverage.out ./... &&
  go tool cover -func=/tmp/coverage.out | tail -1'
```

CI (`.github/workflows/ci.yml`) runs the same steps in a
`golang:1.27-alpine3.23` container and additionally fails the build below 80%
total coverage (the last line above prints the total). The tests take about
half a minute after the package install.

### MuPDF versions

- **1.26.11 (Alpine 3.23)**: the target. CI and the committed corpus test's
  `KNOWN_FAILURES.tsv` are for it. The whole suite passes.
- **1.25.6**: the compile guard still allows 1.25. The test suite other than
  `TestCorpus` passes on 1.25.6 (re-run at `d381f88`). `TestCorpus` fails on
  1.25.6 because some corpus files behave differently there.
- **1.28.6** (Homebrew, macOS): last checked at commit `f0ed073`; it built and
  three tests failed (section 7). Not re-run since.
- **`mupdf_bundled`**: built and tested once against MuPDF 1.25.6 from source
  and passed (section 7).

**Corpus test (`TestCorpus`, `corpus_test.go`).** Walks `testdata/corpus` (163
PDFs, 17 MB; sources in `testdata/corpus/SOURCES.md`) and `testdata/fuzz/crashers`
(raw PDFs of inputs that crashed the fuzz target; the directory has no files
yet) and checks each file in a child process, so a MuPDF crash names the file.
Per file it opens the document (with `Open` and `OpenStream`), extracts up to
five pages, redacts words by quads and by rect, applies, saves, reopens and
checks that the words are gone (by position, in `GetText` and in the
`UseActualText` text), that no text outside the redaction areas disappeared or
moved, and that the rendering outside the redaction areas did not change.
Redacted words still found in the saved bytes (also after `Scrub`) are reported
as notes with the key or stream that holds them, not as failures. It then scrubs in three modes and checks the
reopened output with `HiddenContent`, link survival and unchanged text. Files
with optional content go round again after `ShowAllLayers`. Refusals the
library documents (empty rects, failed checks, limits, saves refused after a
failed apply) are recorded as notes, not failures. A child that crashes in
`/ActualText` extraction is rerun without it.

`testdata/corpus/KNOWN_FAILURES.tsv` lists the committed files that fail an
invariant for a tracked reason: tab-separated `path`, `category`, `reason`. A
failure not listed fails the test. **A listed failure that stops failing also
fails the test**, so the list cannot rot: remove the entry in the same commit
as the fix. To regenerate, run `TestCorpus` and add a line for each unlisted
failure it reports (it prints `path: category: detail`); `GOMUPDF_CORPUS_REPORT`
gives the full findings as JSON Lines.

Environment variables (all optional):

| Variable | Meaning |
| --- | --- |
| `GOMUPDF_CORPUS` | A directory of PDFs to walk as well. Never committed; its failures are logged and reported, not fatal. |
| `GOMUPDF_CORPUS_MATCH` | Only files whose path contains this string. The "listed but not in the corpus" check is skipped when set. |
| `GOMUPDF_CORPUS_REPORT` | Write one JSON object per file (JSON Lines) to this path. |
| `GOMUPDF_CORPUS_TIMEOUT` | Per-file timeout in seconds. Default 60; committed files get at most 30 (10 when listed as timing out). |
| `GOMUPDF_CORPUS_JOBS` | Concurrent child processes. Default 4. |

Each child has a 3 GiB address-space cap. `-short` runs five files. For a bulk
run, mount a directory of PDFs and lift the test timeout:

```sh
docker run --rm -v "$PWD":/src -v /path/to/pdfs:/pdfs:ro -w /src \
  -e GOFLAGS=-buildvcs=false -e GOMUPDF_CORPUS=/pdfs \
  -e GOMUPDF_CORPUS_REPORT=/tmp/report.jsonl -e GOMUPDF_CORPUS_JOBS=8 \
  golang:1.27-alpine3.23 sh -c '
  apk add --no-cache build-base pkgconf mupdf-dev &&
  go test -run "^TestCorpus$" -count=1 -v -timeout 0 .'
```

The report is lost with the container; mount a volume for it if you want it.

**Fuzz target (`FuzzPipeline`, `fuzz_test.go`).** Runs the service's flow on
arbitrary bytes (open, `RawDict` on page 0, redact the first word by quads,
apply, scrub, save, reopen); errors are fine, crashes and hangs are not.
Inputs over 256 KB are skipped; `/ActualText` extraction is left out because it
crashes MuPDF 1.26.11. Plain `go test` replays the seeds. To fuzz:

```sh
docker run --rm -v "$PWD":/src -w /src -e GOFLAGS=-buildvcs=false golang:1.27-alpine3.23 sh -c '
  apk add --no-cache build-base pkgconf mupdf-dev &&
  go test -run "^$" -fuzz "^FuzzPipeline$" -fuzztime 60s .'
```

Keep an input that crashes it as a raw PDF in `testdata/fuzz/crashers`, where
`TestCorpus` runs it in a child process.

**`GOMUPDF_TEST_ACTUALTEXT_CRASH=1`.** `TestActualTextCrashInput` reproduces
the `UseActualText` segfault and is skipped because it kills the test binary.
Set this variable after a MuPDF upgrade to see whether it is fixed; if it
passes, update the `UseActualText` warning.

## 10. Releases and staying current with upstream

- **The fork is frozen at its own line.** `v1.2.0` is not tagged yet. Later fork
  releases are bug fixes to our own patches only. Tags are never moved or
  deleted; protect them with a GitHub tag ruleset, because consumers' rebuilds
  depend on them.
- Keep an `upstream` remote for reference, but don't rebase routinely.
  Adopting a new upstream release or MuPDF version is a deliberate project, done
  only for a reason (a security fix that affects us, or a feature we need). It
  is gated by consumers' extraction-diff tests and by the corpus test.
- **The pin is Alpine 3.23 with MuPDF 1.26.11.** Compile-time guard:
  MuPDF >= 1.25 (see [MuPDF versions](#mupdf-versions); raising it to 1.26 is an
  open decision, section 8).
- Semver on the fork's own module path. Breaking API changes bump the minor
  version while we're on v1 and are noted in this file, matching how upstream
  has versioned so far.
- Every release note lists the MuPDF version CI ran against, since consumers'
  extraction output depends on it.
- **Any change to extraction output** (stext flags, the canonical character
  set, coordinate precision, `/ActualText` handling) **is a release-note
  headline.** `cryptid-lemur`'s page hashes depend on that output.
- Offering the upstream bug fixes (open from a path, streaming save, rawdict
  fixes, redact option handling) upstream as PRs is optional goodwill, not
  something we wait on.
