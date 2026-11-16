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
- MuPDF target: Alpine 3.22's packaged MuPDF 1.25.6, on musl.
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

None yet.

## Known limitations

None yet.

## Staying current with upstream

- **The fork is frozen at `v1.2.0`.** Later fork releases are bug fixes to our
  own patches only. Tags are never moved or deleted; protect them with a GitHub
  tag ruleset, because consumers' rebuilds depend on them.
- Keep an `upstream` remote for reference, but don't rebase routinely. Adopting a
  new upstream release or MuPDF version is a deliberate project, done only for a
  reason (a security fix that affects us, or a feature we need). It is gated by
  consumers' extraction-diff tests on their fixture corpora.
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
