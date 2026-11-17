# FORK.md

`github.com/tredy-ai/gomupdf` is a fork of
[`srijanmukherjee/gomupdf`](https://github.com/srijanmukherjee/gomupdf), used as
the upstream for `cryptid-lemur`. It is a thin cgo layer over MuPDF; the patches
below fix bugs and add the APIs we need.

## Upstream and base

- Upstream: https://github.com/srijanmukherjee/gomupdf
- Base tag: `v1.1.0`, commit `60bb6252c7550a56a9034315613cc29926034d9e`
  (annotated tag object `52b6633f2107f96cd3796041ee6f796c1b268962`).
- The patch series is applied on fork `main` at `f3b7339`, which is `v1.1.0`
  plus one upstream README-only commit ("docs: add Built with Claude Code badge
  to README").
- Build and test target: Go 1.27 on Alpine 3.23 with its packaged MuPDF 1.26.11, on musl.
- `LICENSE` (AGPL-3.0) is unchanged.

## Patch list

Eleven commits, applied in this order, then tagged `v1.2.0`. Only commit 9
breaks an existing API.

| # | Commit subject | API impact |
| --- | --- | --- |
| 1 | `chore(fork): rename module to github.com/tredy-ai/gomupdf` | Import path changes |
| 2 | `build: link Alpine's packaged MuPDF without a stub library` | None |
| 3 | `feat: add MuPDFVersion and require MuPDF 1.25+` | New `MuPDFVersion()` |
| 4 | `fix(rawdict): never drop a character` | `RawDict`/`Dict` now return errors instead of skipping lines |
| 5 | `feat(text): add RawDictWithOptions and AccurateBBoxes` | New method and field |
| 6 | `feat(rawdict): expose per-character quads and line direction` | New fields on `Char` and `TextLine` |
| 7 | `feat(open): open documents from a path without reading them into memory` | `Open` behavior; same signature |
| 8 | `feat(save): stream SaveWithOptions to the file` | `SaveWithOptions` behavior; same signature |
| 9 | `fix(redact)!: apply MuPDF redact options at apply time and draw the box` | **Breaking:** `ApplyRedactions` takes `ApplyOptions`; `RedactOptions.RemoveImages` removed |
| 10 | `feat(redact): add AddRedactionQuads` | New method |
| 11 | `feat(scrub): add Document.Scrub and Document.HiddenContent` | New methods and types |

## Migration notes

### Commit 9: `ApplyRedactions` takes `ApplyOptions`

MuPDF applies redaction options per page at apply time, so they moved from
`RedactOptions` to a new argument of `ApplyRedactions`.

```go
// before
page.AddRedaction(rect, gomupdf.RedactOptions{RemoveImages: true})
n, err := page.ApplyRedactions()

// after
page.AddRedaction(rect, gomupdf.RedactOptions{})
n, err := page.ApplyRedactions(gomupdf.ApplyOptions{Images: gomupdf.ImageRemove})
```

- `RedactOptions.RemoveImages` is gone. Use `ApplyOptions.Images`:
  `ImageRemove` drops overlapping images whole (what upstream always did
  regardless of the field), `ImagePixels` (the zero value) blanks only the
  covered pixels, `ImageNone` leaves images alone.
- `RedactOptions.Fill` is gone. MuPDF's black box is always black and ignores the
  annotation's colour (observed on MuPDF 1.25.6; not re-checked on later versions), so the field never did
  anything. `ApplyOptions.NoBox` suppresses the box.
- The zero `ApplyOptions` now draws the box (upstream drew none), removes text,
  and leaves line art alone. `LineArt` and `Text` select the other MuPDF modes;
  `TextNone` is insecure.
- `RedactOptions` has no fields now but is kept as the parameter type of
  `AddRedaction`.

### `/ActualText` is ignored by default

A PDF can draw one set of glyphs and declare different replacement text for
them (`/Span << /ActualText (DECLARED) >> BDC ... EMC`). MuPDF returns the
declared text by default, so a PII detector would read text that is not what is
drawn on the page. Every extraction API now returns the glyphs actually drawn:
`ExtractText`, `RawDict`, `RawDictWithOptions`, `GetText`, `Lines`, `Words`,
`StructuredText`, `StructuredJSON`, `HTML`, `XHTML`, `XML`, `Search`,
`SearchRects`, `SearchWith` and `FindTables`.

```go
// before: MuPDF's default, declared text
// after: drawn glyphs; opt back in on the options-taking APIs
s, err := page.ExtractText(gomupdf.TextOptions{UseActualText: true})
```

- `TextOptions.UseActualText` (default false) restores MuPDF's behaviour for
  `ExtractText` and `RawDictWithOptions`. APIs without options have no toggle.
  A consumer that wants both views extracts twice.
- MuPDF's `FZ_STEXT_IGNORE_ACTUALTEXT` alone is not enough on 1.26.11: it keeps
  the drawn glyphs but still appends the declared text as zero-width characters.
  So the package also drops the stext device's metatext callbacks
  (`gomupdf_stext.h`), which is how `/ActualText` reaches the device. Tests
  assert this on 1.26.11; other MuPDF versions were not checked.
- Output of `RawDict` and the other APIs changes for PDFs that use
  `/ActualText`; no existing test fixture does.

### Go 1.27

Consumers need Go 1.27 or newer (`go.mod` says `go 1.27`).

## Known limitations

- Encrypted saves (`SaveEncryptedBytes`, `SaveOptions.Encrypt`) and incremental
  saves stay buffered in memory; only plain `SaveWithOptions` streams to disk.
- `AddRedactionQuads` is no more precise than rects for removing content on
  MuPDF 1.25.6: a glyph is removed when its axis-aligned bbox (shrunk 10% per
  side) overlaps any quad's axis-aligned bounds. On rotated text, redacting a
  word's character quads still removes the neighbouring characters, outside the
  drawn box, which does follow the quads. Quads only spare content in the
  corners of the word's bounding rect.

## Staying current with upstream

- **The fork is frozen at `v1.2.0`.** Later fork releases are bug fixes to our
  own patches only. Tags are never moved or deleted; protect them with a GitHub
  tag ruleset, because consumers' rebuilds depend on them.
- Keep an `upstream` remote for reference, but don't rebase routinely. Adopting a
  new upstream release or MuPDF version is a deliberate project, done only for a
  reason (a security fix that affects us, or a feature we need). It is gated by
  consumers' extraction-diff tests on their fixture corpora.
- The pin is Alpine 3.23 with MuPDF 1.26.11. The code is guarded at compile
  time by `MuPDF >= 1.25`; it builds and passes on 1.25.6, 1.26.11 and 1.28.6
  (except `TestRawDictWithOptionsDehyphenate` on 1.28.6).
- Patches 4, 7, 8 and 9 fix upstream bugs. Offering them upstream as PRs is
  optional goodwill, not something we wait on.

### Releases

- Semver on the fork's own module path. Breaking API changes bump the minor
  version while we're on v1 and note the break here, matching how upstream has
  versioned so far.
- Every release note lists the MuPDF version CI ran against, since consumers'
  extraction output depends on it.
- Any change to extraction output (stext flags, the canonical character set,
  coordinate precision) is a release-note headline. `cryptid-lemur`'s page
  hashes depend on that output.
