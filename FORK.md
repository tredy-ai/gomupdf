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
8. [Decisions](#8-decisions)
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
  time by MuPDF >= 1.26; CI and the committed corpus's known-failures list are
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
failure). Nothing has been pushed or tagged yet. 96 commits, the commit that last updated this list included.


### Fork and build

| Commit | Subject | API impact |
| --- | --- | --- |
| `151a285` | `chore(fork): rename module to github.com/tredy-ai/gomupdf` | BREAKING: import path is now github.com/tredy-ai/gomupdf |
| `2eb07d9` | `build: link Alpine's packaged MuPDF without a stub library` | None (cgo link flags) |
| `5cb35d8` | `feat: add MuPDFVersion and require MuPDF 1.25+` | New API: MuPDFVersion |
| `a3a3b33` | `build!: require Go 1.27 and test on Alpine 3.23 (MuPDF 1.26.11)` | BREAKING: module requires Go 1.27 |
| `b1b009d` | `build!: require MuPDF 1.26` | BREAKING: MuPDF 1.25 no longer builds (compile guard) |
| `b09c5a8` | `fix(scrub): build against MuPDF 1.27 and later` | None (compiles against MuPDF 1.27+ headers) |
| `46d63b2` | `ci: allow the workflow to be started by hand` | None (workflow_dispatch trigger) |
| (this commit) | `ci: run on Blacksmith and update the actions to their latest versions` | None (runner `blacksmith-2vcpu-ubuntu-2604`, `contents: read`, Blacksmith checkout v1, codecov-action v7) |

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
| `1eaaa10` | `fix(open): bound the page count by a fixed ceiling` | Behavior change: superseded by the next row (on its own it lets a hostile page count through to slow lookups) |
| `2ff3ebe` | `fix(open): refuse a page tree that claims more pages than it can serve` | Behavior change: a count above the object count is refused above 1,048,576 pages, or when MuPDF cannot build its page map at open |

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
| `41c98c6` | `fix(scrub): keep what a served page inherits from a page-shaped parent` | Behavior change: a page-shaped /Parent of a served page keeps its /Resources |
| `e5f5dfe` | `fix(scrub): keep the appearance of a kept annotation on an orphan page` | Behavior change: a kept annotation on no page keeps its appearance stream |
| `54ca4ae` | `fix(scrub): destroy every content-bearing resource of an orphan page` | Behavior change: orphan pages' patterns, Type 3 glyph procedures and soft-mask groups are destroyed too |
| `ecd4da9` | `fix(scrub): treat a page entry that is not a dictionary as not served` | Behavior change: Scrub and HiddenContent skip a page-tree entry that is not a dictionary |
| `190caaf` | `feat(scrub)!: remove the structure tree by default and add ScrubOptions.KeepStructure` | BREAKING: Scrub removes the logical structure tree unless KeepStructure; new ScrubOptions.KeepStructure, HiddenContent.StructTree |
| `99f4942` | `fix(scrub): do not keep what the catalog /Lang points at` | Behavior change: a catalog /Lang naming /Info, XMP, an embedded file or a script no longer keeps it |
| `f096247` | `fix(scrub): keep what the catalog and kept objects use when removing the structure tree` | Behavior change: page labels, name trees, viewer preferences, the /OpenAction and kept objects' actions, /MK and /BS survive a crafted structure tree |
| `69c4890` | `fix(scrub): leave no structure reference on kept outline items and budget the walk per object` | Behavior change: kept outline items lose /SE and kept GoTo actions /SD; the structure walk budget is per object; Scrub returns an error past it |
| `69c77cb` | `fix(scrub): do not fail on kept indirect arrays when removing structure references` | None (KeepFormFields and KeepAnnotations no longer fail on a form with an indirect /Fields, /Kids or /CO array) |
| `fd1ae14` | `fix(scrub): bound the structure walk's work on shared arrays` | None (an indirect array shared by many structure elements or tree nodes is scanned once) |
| `784ff69` | `fix(scrub): do not let a structure destination keep the element` | Behavior change: structure destinations of kept objects lead to the element's page or are removed; a structure element that is a catalog value is not kept |

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
| `71a998e` | `fix: run deeply recursive MuPDF calls on a large stack` | None (saves, Scrub, extraction and rendering run on a 256 MiB stack; deep files no longer crash) |
| `a54c345` | `fix: parse SetTOC, SetPageLabels and SetMetadata input reentrantly` | None (concurrent edits of different documents no longer lose entries or crash) |
| `3a4b88a` | `fix: run the remaining document-reading MuPDF calls on a large stack` | Behavior change: InsertPDF, InsertPDFRange, Scrub, HiddenContent and saves fail where MuPDF silently read past an exception-stack overflow; every document call runs on the large stack |
| `63c7bd1` | `fix: return an error when the large-stack thread cannot be created` | Behavior change: a call whose large-stack thread cannot be created returns an error instead of running on the small stack; the stack is 64 MiB |

### Tests

| Commit | Subject | API impact |
| --- | --- | --- |
| `3fa95bf` | `test: add a corpus invariant test and fuzz target` | None (TestCorpus, FuzzPipeline) |
| `46bba5a` | `test: add a curated PDF corpus` | None (committed corpus) |
| `8deacf5` | `test: report redacted words left after scrub in the corpus test` | None (corpus test reports words left after Scrub) |
| `2fe60dc` | `test: align the corpus test with the final library behaviour` | None (corpus test expectations) |
| `e383895` | `test: compare the scrubbed render with the unscrubbed control` | None (corpus test compares renders with the control) |
| `29f613d` | `test: fail the corpus test when Scrub refuses a file the control accepts` | None (corpus test: scrub-error and redact-error failures) |
| `7c6b638` | `test: make time limits hold on a slow machine` | None (timeLimit helper, GOMUPDF_TEST_TIME_FACTOR) |
| `b16792c` | `test: accept known hangs that finish and report a timeout once` | None (corpus test: `timeout` entries, 120 s cap) |

### Documentation

| Commit | Subject | API impact |
| --- | --- | --- |
| `a0c1781` | `docs: describe the fork as it is` | None (FORK.md and README.md rewritten) |
| `8a05b28` | `docs: update for the final review fixes` | None |
| `10306a9` | `docs: record the final corpus run` | None |
| `d26050a` | `docs: correct two statements about scrub and open` | None |
| `f6bd637` | `docs: list the final commits and corpus figures` | None |
| `0f38b4b` | `docs: record the owner's decisions on layers, structure and MuPDF` | None |
| `d87d563` | `docs: correct the structure and stack statements` | None |

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
dictionary other pages share is copied for this page and kept for them. A
default `Scrub` then removes the whole structure tree (4.5); the property lists
in content streams stay, stripped only on redacted pages.

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
always; annotations and fields that are on no page; everything in the page
dictionaries MuPDF does not serve (orphan pages, below); and the logical
structure tree (below). Scripts: on every annotation and
form node that stays, `/AA` and any `/A` or `/PA` that is or chains to a
JavaScript or Launch action.

**Structure tree** (the owner's decision of 2026-11-17, section 8). Unless
`KeepStructure` is set, `Scrub` deletes the catalog
`/StructTreeRoot` and `/MarkInfo` and destroys the tree's own objects: the root,
every structure element, the `/ParentTree` and `/IDTree` nodes, `/RoleMap` and
`/ClassMap`, attribute objects, indirect MCR and OBJR dictionaries and the
indirect strings of elements, attribute objects and `/IDTree` keys. Reason: the
tree holds text page-text extraction never returns, so the detector never sees
it (`/Alt`, `/ActualText`, `/E`, `/T`, `/ID` and `/IDTree` keys, `/Lang`, table
`/Summary` and `/Headers`); a corpus run found redacted words surviving the
whole pipeline in structure `/ID`, `/Summary`, `/T` and `/ActualText`. The
output is then no longer a tagged PDF: PDF/UA conformance and the structure
screen readers use are gone, and reading order follows content order. Rules:

- Kinds. Each object is destroyed only if it is of its kind: an element has an
  `/S` name, no `/Subtype`, and `/Type /StructElem` or no `/Type` and a `/P`
  naming an indirect dictionary that is the root or itself of the element
  shape (so an action, border style, page-label or rendition dictionary,
  whose `/P` is a page, a string or a parameter dictionary, is not one); the
  root no `/Type` or `/Type /StructTreeRoot`; a tree node no `/Type` and
  `/Kids`, `/Nums` or `/Names`; `/RoleMap` and `/ClassMap` no `/Type`; an
  attribute object no `/Type` and an `/O` name; MCR and OBJR their `/Type`.
  Arrays are never destroyed (nothing marks the arrays a page tree relies on);
  they leave with the objects holding them.
- Kept content wins. Page content, served pages and page-tree nodes, kept
  annotations, kept form nodes and outline items, and what a kept appearance
  draws are never destroyed, whatever the tree says. Neither is what a kept
  annotation, field or outline item uses in a defined role (its `/A`, `/PA`,
  `/MK`, `/BS`, `/BE`, `/Border` and `/Dest`), the kept `/OpenAction` and a
  kept action's `/Next`: such an object is not followed through `/P`, `/K`
  or `/SE`. Otherwise an element, attribute object, tree node, MCR or OBJR
  dictionary or typed root is not kept merely because a kept object
  references it (a kept Link's stray key, say), as for annotations.
  Exceptions, which are only unlinked and so stay if anything else references
  them: indirect strings, `/RoleMap`, `/ClassMap` and an untyped root, which
  an action or a field's own data could pass for, when a kept object also
  references them; and tree objects that are also page content (PyMuPDF
  `test_2710.pdf` uses its `/StructTreeRoot` and `/RoleMap` as page graphics
  states and its `/ParentTree` as a page font; that tree holds no text, only
  `/S` names).
- What the catalog uses stays. Whatever the catalog's `/PageLabels`, `/Names`
  (other than the trees `Scrub` removes), `/Dests`, `/ViewerPreferences`,
  `/Threads`, `/URI`, `/OutputIntents`, `/Legal`, `/Requirements`,
  `/Collection`, `/DPartRoot` and `/Extensions` reach is not destroyed as part
  of the tree (a `/ParentTree` that is also `/PageLabels`, an `/IDTree` that
  is also `/Names/Dests`), except a dictionary the tree recognises as its own
  that they reach only deeper than their values, name-tree `/Kids` and arrays
  (a typed element as a `/Dests` leaf). This protects only from the tree's
  removal: the catalog `/Lang` (when it is a string) and these values never
  keep `/Info`, XMP, an embedded file or a script `Scrub` removes otherwise.
  Stray catalog keys keep nothing, and a structure element that is itself
  such a value (`/Dests 7 0 R` naming one) is not protected.
- What the tree points at is not part of it: pages (`/Pg`), annotations and
  widgets (`/Obj`), forms and content streams (`/Stm`), fonts and namespaces
  stay unless something else removes them; `/AF` files go as everywhere. An
  annotation or page only the tree referenced (an `/OBJR`, a `/Pg`) has no
  referrer left, so the save drops it even under `KeepAnnotations`.
- Integers. `/StructParents` and `/StructParent` are deleted from pages,
  annotations, fields and XObjects, except on page content and kept
  appearances, where the stale integer stays.
- In content streams nothing changes: `/MCID` tags stay as inert markers, and
  inline or named property lists (`/Span << /ActualText (...) >> BDC`) are not
  removed by `Scrub`; `ApplyRedactions` strips their declared text on redacted
  pages only. The catalog `/Lang` stays when it is a string. Kept outline items lose `/SE` and
  kept GoTo actions their `/SD` structure destination, which point into the
  tree. Every other structure destination (PDF 2.0: a destination whose
  first entry is a structure element, as a Link's or outline item's `/Dest`,
  a GoTo `/D` or a `/Dests` or `/Names/Dests` entry) is pointed at the
  element's page (its `/Pg`, its first content item's or its nearest
  ancestor's), or removed when there is none (the Link or item stays), so
  the element goes like the rest of the tree.
- Limits. The walk is iterative, visits each object once per role and stops
  after 65,536 + 16 x (object count) units of work, one per array or
  dictionary it visits (an MCID, a null or an object already visited costs
  nothing): an ordinary tagged file uses a few percent (pdf.js
  `wdsg_fitc.pdf` 2,558 of 111,168, PyMuPDF `test_3594.pdf` 9,183 of
  221,392). Past it, what it reached is destroyed, the rest is only unlinked
  with the catalog key, and `Scrub` returns an error, since a kept object's
  stray reference can keep that rest: discard the document.
- `ApplyRedactions` works before or after such a `Scrub` on the same
  `Document`.

**Orphan pages.** The pages MuPDF serves (the `PageCount` pages) are
authoritative. Any other page dictionary is an orphan: one outside the page
tree, or in it but not served (a `/Kids` entry past a `/Count` that is too
small, or a damaged file's extra pages). `Scrub` empties orphans completely
(`/Contents`, `/Resources`, `/Parent`, and the content their own and
inherited resources reach: forms and images, tiling patterns, Type 3 glyph
procedures and soft-mask groups, down through the resources of each at any
depth), keeps the dictionary page-shaped for references to it, and
`HiddenContent.OrphanPages` counts them. An annotation on no page that `Scrub`
keeps, a Link under the default options included, keeps its appearance stream,
even when an orphan page's resources list the same stream.
A page MuPDF cannot look up in a damaged or cyclic page tree is skipped, not an
error. A page tree that claims more pages than it can serve is refused (4.2).

**Options:**

| Option | Keeps |
| --- | --- |
| `KeepAnnotations` | Every annotation (including those on pages MuPDF does not serve) and widget and the whole `/AcroForm` (and `/Perms`, `/DSS`, signature dictionaries), with scripts removed. `/XFA` still goes. Embedded media in kept annotations stay (a FileAttachment's file, Sound, Movie, Screen rendition, RichMedia assets, 3D streams). An annotation reachable only through an outline item's action (a GoTo3DView) is kept by Scrub but unreferenced when outlines are removed, so a garbage-collecting save drops it; keep outlines to keep it. |
| `KeepFormFields` | Widgets and the `/AcroForm` field tree, with scripts removed and the signature remnants kept as above. Other non-Link annotations still go. Hidden template pages (`/Names/Templates`) are emptied whatever the options, so templates spawn blank pages. You must inspect form values yourself (`Page.Widgets`); they are not reliably in page text. |
| `KeepOutlines` | `/Outlines`. Titles and `/Dest` stay; an `/A` that is or chains to a JavaScript or Launch action goes. |
| `KeepStructure` | The logical structure tree, `/MarkInfo` and every `/StructParents` and `/StructParent`, as before this option existed, with all their text (`/Alt`, `/ActualText`, `/E`, `/T`, `/ID`, `/Lang`, attribute values) except the declared text `ApplyRedactions` strips on redacted pages. Independent of the other options. |

**Destruction, not unlinking.** Removed objects that may be reachable from
elsewhere (a kept structure tree's `/OBJR`, an `/AF`) are destroyed, so surviving
references resolve to null. Scrub first marks everything kept content depends
on and never destroys a marked object. Other things (appearance streams, URI
and other actions, `/VRI`, parts beyond the walk limits) are only unlinked and
leave the file only if nothing else references them, which is why a
garbage-collecting save matters.

**HiddenContent** reports the same locations: booleans (`Info`, `XMP`,
`AcroForm`, `XFA`, `Outlines`, `JavaScript`, `Perms`, `DSS`, `WebCapture`,
`StructTree`) and
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
user. `Scrub` leaves layers alone. The decided rule (section 8): call
`ShowAllLayers` before extraction whenever `HiddenContent.Layers > 0`.

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

### 5.8 MuPDF 1.26 is the minimum

The compile guard requires MuPDF 1.26 or newer; building against 1.25 headers
stops with `#error "gomupdf (tredy-ai) requires MuPDF >= 1.26"`. CI, Docker and
the committed corpus test only ever held on 1.26 (1.26.11 on Alpine 3.23).
Nothing changes in the API. Build against MuPDF 1.26 or newer (Alpine 3.23's
`mupdf-dev`, or a newer distribution package), and check `MuPDFVersion()` in
your build if you record extraction output.

### 5.9 `Scrub` removes the structure tree by default

A zero `ScrubOptions` now also removes the logical structure tree (4.5): the
output of `Scrub` is no longer a tagged PDF, and `HiddenContent` has a new
field, `StructTree`, which reads false afterwards. A caller that must keep the
file accessible sets `KeepStructure` and accepts the text the tree keeps.

```go
// before: the tree stayed
err := doc.Scrub(gomupdf.ScrubOptions{})

// after: the same call removes it; to keep it
err := doc.Scrub(gomupdf.ScrubOptions{KeepStructure: true})
```

An annotation or page only the structure tree referenced now leaves the saved
file even under `KeepAnnotations` or `KeepFormFields`. A tree larger than the
walk budget (4.5) makes `Scrub` return an error: discard that document.

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
2. **Inspect first.** Call `HiddenContent`. Layers (decided, see
   [Decisions](#8-decisions)): whenever `Layers > 0`, call `ShowAllLayers`
   before extraction and detection, so the detector sees, and redaction
   removes, everything on every layer; the output then shows every layer at
   once. With `Layers == 0` nothing is hidden. Do not go by `HiddenLayers`:
   inline `/OC << >> BDC` property lists in content streams are not counted.
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
   uncompressed output is for tests). The zero `ScrubOptions` removes the
   structure tree too (decided, section 8), so the output is not a tagged
   PDF; set `KeepStructure` only where the output must stay accessible, and
   accept the text the tree keeps, which detection never saw (4.5).
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
by design:

- link URIs, and the `/Contents` of Link annotations that are kept;
- layer names (optional-content group names), and the text of hidden layers,
  when `ShowAllLayers` is not called (the decided rule calls it whenever
  `Layers > 0`, section 8);
- named-destination and name-tree keys;
- font names and subset `/CharSet`;
- the document `/ID`;
- declared text written inline or as named property lists in the content
  streams of pages that were not redacted (`/Span << /ActualText (...) >>
  BDC`): `Scrub` does not rewrite content streams, and `ApplyRedactions` strips
  it on redacted pages only.

With `KeepStructure`, the structure tree's text survives as well (`/Alt`,
`/ActualText`, `/E`, `/T`, `/ID`, `/Lang`, table `/Summary` and `/Headers`),
except the declared text stripped on redacted pages.

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
- Deleting an object created in the same session broke saves on 1.25.6, so
  Scrub and `ShowAllLayers` leave an empty dictionary in its place; 1.26 does
  not need that, and it is harmless there.
- MuPDF recurses once per indirect reference hop when it saves, once per
  nesting level when it loads an outline, and once per nesting level when it
  interprets content. A cgo call runs on the calling thread's stack. On
  Alpine 3.23 (musl) the threads the Go runtime creates have musl's default
  128 KiB (`pthread_getattr_np` reads 133,872 bytes from a goroutine); only
  the process's main thread can grow to `ulimit -s` (8 MiB). Before
  `71a998e`, a Garbage 1 save called from ordinary goroutines killed the
  process with SIGSEGV between 1,500 hops (saved) and 1,700 (crashed) for
  each of three shapes: sibling outline items (a `/Next` chain), dictionaries
  chained under a page's `/Resources`, and a Link's `/Next` action chain. The
  same saves on the main thread survived 100,000.
  Saves, `Scrub`, `HiddenContent`, `ShowAllLayers`, the load at open,
  `ApplyRedactions` and `AddRedaction*`, `ConvertToPDF`, text extraction,
  search, image and drawing listing and rendering now run on a new thread
  with a large stack, which is address space committed only as it is used
  (peak RSS of a 20,000-item outline save: 43 MB before and after on the main
  thread, 48 MB from a goroutine after). The stack is 64 MiB: MuPDF needs
  about 81 bytes per hop, and a save from a goroutine handles 800,000
  sibling outline items (900,000 crash); about 40 such calls fit at once
  under a 3 GiB address-space limit (8 concurrent saves all succeed; of 100
  started at once 43 to 71 did and the rest were refused). It was 256 MiB
  until the commit after `3a4b88a` (2,560,000 items saved, 5,120,000
  crashed, about ten calls under 3 GiB). Each such call pays for creating and joining that thread, about
  45 µs (`HiddenContent` on a small file 13 → 56 µs, `GetText` on a small
  page 27 → 74 µs; invisible on a 4 ms text page or a 42 ms render). Since
  the commit after `a54c345` every other call that reads or edits the
  document runs there too, so `TOC` reads an outline nested 400,000 levels
  deep (950 crashed the process before); only the page count, the password
  check and font metrics stay on the caller's stack.
  Form XObjects nested more than about 60 levels are refused by MuPDF:
  `GetText`, `RawDict`, `Pixmap` and the other page calls return an error
  saying "exception stack overflow" (MuPDF's fixed exception stack, about
  250 nested `fz_try`), and nothing of that page is returned. The same limit
  hits a copy: MuPDF reads an object it could not load past it as null and
  carries on, so `InsertPDF` of a 20,000-long reference chain copied 124
  links and succeeded; it now fails with that error, and so do `Scrub`,
  `HiddenContent` and saves if one happens during them.
  If the large-stack thread cannot be created (an address-space limit) the
  call runs nothing and returns an error, "cannot create a thread with a
  large stack: ..." (`Authenticate` returns false; an `Open` that fails
  this way leaks nothing). A hostile file can still exceed
  any stack, and a crash on that thread kills the process without a Go
  traceback (it blocks every signal): the service must process each
  document in a separate worker process (section 6, step 1). Under the race
  detector the committed `large-linky/bug1978317.pdf` takes about 29 s, at
  about a quarter of `TestCorpus`'s 120 s default per-file cap, which is
  enough: set `GOMUPDF_CORPUS_TIMEOUT` higher or run `-race` without
  `TestCorpus` if a slower machine needs it.

**Corpus results (run of 2026-11-18 on the code of the commit that last
updated this paragraph, `fix(scrub): do not let a structure destination keep
the element`; MuPDF 1.26.11 on Alpine 3.23, Go 1.27; 413 s wall time with three
workers).** Every outcome and failure category matches the run at `190caaf`,
but for one file the corpus test now fails because `Scrub` refuses it
(`scrub-error`, see below).
5,347 public test and real-world PDFs from the pdf.js, veraPDF, qpdf, PyMuPDF,
Poppler, corkami and PDF Association suites (kept outside the repo), plus the
163 committed regression files. Each file is opened, extracted, redacted by
character quads and by rectangle, saved, reopened and checked, then scrubbed in
four modes and compared with an unscrubbed save. 800 of the 5,347 files (and 20
committed ones) have a structure tree.

| Outcome | Files |
|---|---|
| Passed every check | 5,190 |
| Rejected at open | 30 |
| Need a password | 46 |
| Broke a cosmetic invariant, cannot be saved, or refused by `Scrub` (see below) | 76 |
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
- **One file `Scrub` refuses** (PyMuPDF `test_5101.pdf`): MuPDF raises "cycle in
  resources" in three of the four modes; the error is the documented
  outcome (discard the document), and the test counts it as a failure.
- **One file cannot be saved after a rectangle redaction**: pixel redaction makes
  MuPDF store its images uncompressed, and the uncompressed save needs 306 MB.
- **What still sits in the bytes after redaction, default `Scrub` and a
  garbage-collecting save** (a redacted word found somewhere in 24 files, 40
  before the structure tree went): font programs, signatures and name tables
  (streams, 12 files, mostly coincidental matches), font `/CharSet` (3),
  font names (`/FontFamily`, `/FamilyName`, `/FullName`; 2), link `/URI`
  strings (3), named-destination and name-tree keys (2), a layer name (1) and
  an embedded-go-to target's file name (1). The structure-tree rows of the
  previous run (`/ID` 4 files, `/Summary` 3, `/T` 1, `/ActualText` 1) are
  gone, and so are 5 `/Contents` and 3 `/URI` rows: Links MuPDF's redaction
  removed from the page, which only the tree's `/OBJR` still referenced. See
  the list at the top of this section.

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
  built and tested once against MuPDF 1.25.6 from source and passed, before the
  minimum became 1.26; it has not been run against 1.26.
- The GitHub Actions workflow has never run: nothing has been pushed.

<a id="8-open-decisions"></a>

## 8. Decisions

The three questions this section used to leave open, decided by the owner on
2026-11-17.

1. **Hidden layers (decided 2026-11-17).** The service calls
   `Document.ShowAllLayers` before extraction whenever `HiddenContent` reports
   `Layers > 0`, so the detector sees, and redaction removes, everything on
   every layer. The output then shows every layer at once. Deleting the hidden
   content instead was considered and not built. See section 6, step 2, and
   4.6.
2. **Accessibility structure (decided 2026-11-17).** `Scrub` removes the
   logical structure tree by default; `ScrubOptions.KeepStructure` keeps it.
   The tree's text (`/Alt`, `/ActualText`, `/E`, `/T`, `/ID`, `/Summary`, ...)
   is invisible to page-text detection, and redacted words survived in it. The
   output of a default `Scrub` is no longer a tagged PDF. See 4.5 and 5.9.
3. **MuPDF (decided 2026-11-17).** MuPDF 1.26 is the minimum: the compile
   guard rejects older headers, matching what CI, Docker and the committed
   corpus test hold on (1.26.11). See 5.8 and [MuPDF versions](#mupdf-versions).

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
`golang:1.27-alpine3.23` container on Blacksmith's 2-vCPU runner
(`blacksmith-2vcpu-ubuntu-2604`, only 2 cores shared with the container, which
is why the time limits below are generous) and additionally fails the build below 80%
total coverage (the last line above prints the total). The tests take about
half a minute after the package install.

**Time limits.** A few tests assert that a hostile file opens, scrubs or is
refused within a time limit; they guard against quadratic or runaway work (the
bugs they caught went from milliseconds to tens of seconds or minutes). Each
test states its base limit, the figure that is comfortable on a fast machine,
and passes it through `timeLimit` (`timelimit_test.go`), which multiplies it by
10 by default or by `GOMUPDF_TEST_TIME_FACTOR` (a positive integer) when set, so
a shared CI runner several times slower than a laptop passes. Tests whose old
bug was under about 25x slower than their base limit cap the factor at 5 (2 for
`TestScrubUnloadableEntriesCost`, whose sparse-xref bug took 1.4 s against a
0.5 s base); the environment variable can only lower a capped factor. Pass
`GOMUPDF_TEST_TIME_FACTOR=1` on a fast machine to see the base limits.

### MuPDF versions

- **1.26.11 (Alpine 3.23)**: the target. CI and the committed corpus test's
  `KNOWN_FAILURES.tsv` are for it. The whole suite passes.
- **1.25.6**: no longer supported; the compile guard requires 1.26 and stops a
  1.25 build with an `#error`. History: until that change the test suite other
  than `TestCorpus` passed on 1.25.6 (last run at `d381f88`), and `TestCorpus`
  did not, because some corpus files behave differently there.
- **1.28.6** (Homebrew, macOS): last checked at commit `f0ed073`; it built and
  three tests failed (section 7). The tests have not been re-run since; the
  package compiles against the 1.28.6 headers (check below).
- **`mupdf_bundled`**: built and tested once against MuPDF 1.25.6 from source
  and passed, before the minimum became 1.26; not run against 1.26 (section 7).

**Compile check against newer MuPDF headers (manual, before a MuPDF
upgrade).** The package reads a few MuPDF struct fields directly
(`pdf_document` `repair_attempted` and `fwd_page_map`, xref entry `type`,
`ofs` and `gen`, `fz_device` callbacks, structured-text structs), and a field
renamed upstream breaks the build. `go vet` compiles every cgo preamble, so it
catches that without linking or installing anything. In the gate's
environment, with the new version's headers fetched inside the container
(the version is an example):

```sh
docker run --rm -v "$PWD":/src:ro -w /src -e GOFLAGS=-buildvcs=false golang:1.27-alpine3.23 sh -c '
  apk add --no-cache build-base pkgconf mupdf-dev &&
  mkdir /tmp/m && cd /tmp/m &&
  wget -q https://mupdf.com/downloads/archive/mupdf-1.28.6-source.tar.gz &&
  tar xzf mupdf-1.28.6-source.tar.gz mupdf-1.28.6-source/include &&
  cd /src && CGO_CFLAGS=-I/tmp/m/mupdf-1.28.6-source/include go vet ./...'
```

`CGO_CFLAGS` comes before the `-I` that pkg-config adds for the installed
1.26.11 headers, so the preambles compile against the new ones. That it
really compiles them: with `pdf->page_tree_broken` (a 1.26 field 1.27
removed) put back into `gomupdf_page_count`, the same command fails with
`'pdf_document' has no member named 'page_tree_broken'`, while plain
`go vet` against 1.26.11 passes. It proves the code compiles, not that it
behaves the same: run the tests and `TestCorpus` against a build of that
version before adopting it. Last run: 1.28.6 at `69c77cb`, clean.

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
as notes with the key or stream that holds them, not as failures. It then scrubs in four modes (default,
`KeepFormFields`, `KeepAnnotations` with `KeepOutlines`, and `KeepStructure`) and checks the
reopened output with `HiddenContent`, link, annotation and widget survival and
unchanged text and rendering. Without `KeepStructure`, fewer annotations or
widgets on no page than the unscrubbed control is accepted only when the same
options plus `KeepStructure` give the control's counts (they were ones only
the structure tree referenced; noted as `scrub-structure-orphans`). Files
with optional content go round again after `ShowAllLayers`. Refusals the
library documents (empty rects, failed checks, limits, saves refused after a
failed apply) are recorded as notes, not failures. An error from `Scrub` or
`HiddenContent` (in any scrub mode, or the `Scrub` after redaction) is a
failure (`scrub-error`) when the file serves pages and its unscrubbed
Garbage 1 save was written and reopened; otherwise (no pages to scrub, or a
document that is not a PDF) it stays a `scrub-rejected` note. An
`AddRedaction` refusal other than an empty rect is a failure
(`redact-error`). A child that crashes in
`/ActualText` extraction is rerun without it.

`testdata/corpus/KNOWN_FAILURES.tsv` lists the committed files that fail an
invariant for a tracked reason: tab-separated `path`, `category`, `reason`. A
failure not listed fails the test. **A listed failure that stops failing also
fails the test**, so the list cannot rot: remove the entry in the same commit
as the fix. The one exception is category `timeout`: it means the file
**may** not finish (MuPDF hangs on it on some machines or architectures, and
`misc/operator_list_cycle.pdf` hangs on arm64 but finishes on x86-64). Such a
file gets a 10 s cap; if it finishes anyway, that is not a failure and
whatever outcome it has is accepted, and its other entries, if any, are not
checked. A file that is not listed as `timeout` and times out fails with the
timeout only; its other entries are not reported as stale, since the checks
after the timeout never ran. To regenerate, run `TestCorpus` and add a line for each unlisted
failure it reports (it prints `path: category: detail`); `GOMUPDF_CORPUS_REPORT`
gives the full findings as JSON Lines.

Environment variables (all optional):

| Variable | Meaning |
| --- | --- |
| `GOMUPDF_CORPUS` | A directory of PDFs to walk as well. Never committed; its failures are logged and reported, not fatal. |
| `GOMUPDF_CORPUS_MATCH` | Only files whose path contains this string. The "listed but not in the corpus" check is skipped when set. |
| `GOMUPDF_CORPUS_REPORT` | Write one JSON object per file (JSON Lines) to this path. |
| `GOMUPDF_CORPUS_TIMEOUT` | Per-file timeout in seconds. Default 120, so a machine several times slower than a laptop passes; files listed as `timeout` get at most 10. |
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
  MuPDF >= 1.26 (see [MuPDF versions](#mupdf-versions)).
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
