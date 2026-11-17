package gomupdf

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/tredy-ai/gomupdf/geometry"
)

// RedactOptions controls how a redaction is added. It currently has no fields.
//
// Changed from upstream: Fill and RemoveImages are removed. MuPDF applies
// redaction options per page at apply time (see ApplyOptions), not per
// annotation, and its black box ignores the annotation's colour, so neither
// field could do what it claimed. The type and the parameter are kept so that
// per-annotation options can be added without another signature change.
type RedactOptions struct{}

// ImageMethod selects what ApplyRedactions does to images that overlap a
// redaction. The zero value is ImagePixels.
type ImageMethod int

const (
	// ImagePixels blanks only the covered pixels (PDF_REDACT_IMAGE_PIXELS).
	ImagePixels ImageMethod = iota
	// ImageNone leaves images unchanged (PDF_REDACT_IMAGE_NONE).
	ImageNone
	// ImageRemove drops every overlapping image whole (PDF_REDACT_IMAGE_REMOVE).
	ImageRemove
)

// LineArtMethod selects what ApplyRedactions does to vector graphics. The
// zero value is LineArtNone, matching MuPDF.
type LineArtMethod int

const (
	// LineArtNone leaves vector graphics unchanged.
	LineArtNone LineArtMethod = iota
	// LineArtRemoveIfCovered removes graphics lying entirely inside the area.
	LineArtRemoveIfCovered
	// LineArtRemoveIfTouched removes graphics that touch the area at all.
	LineArtRemoveIfTouched
)

// TextMethod selects what ApplyRedactions does to overlapping text. The zero
// value is TextRemove, the only secure choice.
type TextMethod int

const (
	// TextRemove removes any text overlapping the area, however slightly.
	TextRemove TextMethod = iota
	// TextNone leaves text in place. INSECURE; it exists for tests that prove
	// verification catches leftovers.
	TextNone
)

// ApplyOptions controls ApplyRedactions. The zero value draws the box, blanks
// covered image pixels, leaves line art alone and removes text.
type ApplyOptions struct {
	// NoBox suppresses the black box MuPDF draws over each redacted area. The
	// box is always black; the annotation's colour is ignored.
	NoBox   bool
	Images  ImageMethod
	LineArt LineArtMethod
	Text    TextMethod
}

// AddRedaction marks rect for redaction (a redaction annotation). Nothing is
// removed until ApplyRedactions is called.
//
// A rect inverted in x or y (X0 > X1 or Y0 > Y1) is normalised, as MuPDF
// does.
//
// Changed from upstream: a rect that is empty (zero width or height) or has
// a NaN or infinite coordinate, after conversion to float32 and
// normalisation, returns an error. MuPDF would accept it and remove next to
// nothing. Such a rect typically comes from the union of Char.BBox for
// glyphs whose boxes have no height (some Type 3 fonts); redact those
// characters with AddRedactionQuads, which accepts zero-area quads.
func (p *Page) AddRedaction(rect geometry.Rect, opts RedactOptions) error {
	x0, y0, x1, y1 := float32(rect.X0), float32(rect.Y0), float32(rect.X1), float32(rect.Y1)
	if !isFinite32(x0) || !isFinite32(y0) || !isFinite32(x1) || !isFinite32(y1) ||
		x0 == x1 || y0 == y1 {
		return fmt.Errorf("gomupdf: AddRedaction: empty or non-finite rect %v; "+
			"use AddRedactionQuads for degenerate (zero-width or zero-height) boxes", rect)
	}
	r := rect.Normalize()
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	return d.b.addRedaction(p.Number, [4]float64{r.X0, r.Y0, r.X1, r.Y1})
}

// AddRedactionQuads marks the area covered by quads for redaction: one
// redaction annotation whose /QuadPoints are the quads. MuPDF sets its /Rect
// to the union of the quads' bounds plus 1pt for the annotation's outline.
// Quads are in page space, like AddRedaction's rect and RawDict's Char.Quad.
// Nothing is removed until ApplyRedactions is called.
//
// New relative to upstream.
//
// Precision: ApplyRedactions (with TextRemove) removes a glyph when the
// glyph's own quad, shrunk towards its centre by a tenth of its advance and
// of its height on each side, overlaps a quad's shape, or when extraction
// puts a character under the quad at the glyph's origin (see
// ApplyRedactions). So on rotated or
// skewed text, redacting the quads of a word's characters removes that word
// and keeps its neighbours. Stock MuPDF instead tests the glyph's axis-aligned
// box against each quad's axis-aligned bounds, which also removes neighbours;
// see ApplyRedactions. A quad that is not strictly convex with its corners in
// order (UL, UR, LR, LL around the edge, either winding), for example a
// zero-area quad or one with corners swapped, is not tested by shape but by
// its bounds as stock MuPDF does, which never removes less. The black box is
// drawn over each quad's shape. Images and line art are still matched
// against, and image pixels blanked over, each quad's axis-aligned bounds, as
// in stock MuPDF.
//
// An empty slice or a non-finite coordinate (after conversion to float32)
// returns an error: MuPDF treats a NaN quad as empty and would silently redact
// nothing. Zero-area quads are accepted and remove the glyphs they touch.
func (p *Page) AddRedactionQuads(quads []geometry.Quad, opts RedactOptions) error {
	if len(quads) == 0 {
		return errors.New("gomupdf: AddRedactionQuads: no quads")
	}
	flat := make([]float32, 0, len(quads)*8)
	for _, q := range quads {
		for _, pt := range [4]geometry.Point{q.UL, q.UR, q.LL, q.LR} {
			x, y := float32(pt.X), float32(pt.Y)
			if !isFinite32(x) || !isFinite32(y) {
				return fmt.Errorf("gomupdf: AddRedactionQuads: non-finite coordinate in quad %v", q)
			}
			flat = append(flat, x, y)
		}
	}
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	return d.b.addRedactionQuads(p.Number, flat)
}

func isFinite32(f float32) bool {
	return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0)
}

// Errors a *RedactionCheckError matches with errors.Is. New relative to
// upstream; see ApplyRedactions.
var (
	// ErrTextRemains: text is still extracted under a redaction.
	ErrTextRemains = errors.New("gomupdf: text remains under a redaction")
	// ErrTextMoved: a redacted character reappears, moved, on its line, so
	// the redaction missed the glyph it was meant for.
	ErrTextMoved = errors.New("gomupdf: redacted text moved next to its redaction")
)

// RedactionCheckError is returned by ApplyRedactions when the page, checked
// after redacting, still shows text it should not. New relative to upstream.
type RedactionCheckError struct {
	Page           int // 0-based page number
	Remaining      int // redacted characters still under a redaction (ErrTextRemains)
	Moved          int // redacted characters that reappear moved on their line (ErrTextMoved)
	MovedElsewhere int // kept characters that moved, elsewhere or under a box (not an error by itself)
}

func (e *RedactionCheckError) Error() string {
	var parts []string
	if e.Remaining > 0 {
		parts = append(parts, fmt.Sprintf("%d characters remain under redactions", e.Remaining))
	}
	if e.Moved > 0 {
		parts = append(parts, fmt.Sprintf("%d redacted characters reappear moved next to their redactions", e.Moved))
	}
	return fmt.Sprintf("gomupdf: apply redactions: page %d: %s (text in a pattern, soft mask, or other content "+
		"redaction cannot reach or measures wrongly); the page must not be treated as redacted",
		e.Page, strings.Join(parts, " and "))
}

// Unwrap returns ErrTextRemains and ErrTextMoved, as they apply.
func (e *RedactionCheckError) Unwrap() []error {
	var errs []error
	if e.Remaining > 0 {
		errs = append(errs, ErrTextRemains)
	}
	if e.Moved > 0 {
		errs = append(errs, ErrTextMoved)
	}
	return errs
}

// ApplyRedactions permanently removes the content under every redaction
// annotation on the page according to opts, then deletes the redaction marks.
// Returns the number of redactions applied. Effective on Save.
//
// Every redaction annotation on the page is applied, not only the ones the
// caller added with AddRedaction or AddRedactionQuads: redaction annotations
// already present in the opened file are applied (and count towards the
// limit below) too.
//
// A page whose redaction annotations hold more than 65536 regions in all
// (each quad of a quad redaction, plus one per rect redaction; annotations
// in a file can share one huge /QuadPoints array, which counts once per
// annotation) is refused, for every Text mode, with an error naming the page
// (its 0-based Number), the count and the limit. Nothing on the page changes
// and its redaction annotations stay, so the caller can inspect or reject the
// file: both this package's pass and MuPDF's own redaction cost memory and
// time per region.
//
// The page is refused the same way, before anything changes, when its form
// XObjects are nested more than 32 deep or reached by more than 65536 paths
// (each path is a copy MuPDF's redaction makes), when its /ParentTree is
// more than 64 levels deep, or when its structure tree (or its /ParentTree,
// which is read whole when its /Limits do not lead to the page's entries) is
// larger than 16 nodes per object in the file plus 65536. A second call
// returns the same error. Any other error (one MuPDF raises while rewriting
// the page, or running out of memory) can come after the page has started to
// change: an error from ApplyRedactions means the page must not be treated as
// redacted. In particular, if MuPDF repairs the file's cross-reference table
// during the call (which loading every object at open prevents; see Open),
// the changes made so far are lost and ApplyRedactions, like AddRedaction
// and AddRedactionQuads, returns an error saying so.
//
// After an error that came once the page had started to change (a MuPDF
// failure in the rewriting, a repair, or the check below), the page is
// half redacted and its redaction annotations may be gone. Discard this
// Document: reopen the original to retry, or reject the file. Every later
// save of it (SaveBytesWithOptions, SaveWithOptions, SaveEncryptedBytes and
// the functions built on them, and ConvertToPDF) returns an error wrapping
// ErrRedactionFailed until it is closed. The refusals above, which change
// nothing, do not. New relative to upstream.
//
// Also changed, with opts.Text TextRemove: after redacting, the page is
// extracted again (as RawDict does by default) and checked. ApplyRedactions
// returns a *RedactionCheckError if a redacted character is still extracted:
// a character lies under a redaction (shrunk as above) and the same
// character was under a redaction before, on the same line nearby (within
// two character heights along its baseline and half a height across it)
// (errors.Is ErrTextRemains); or a character lies, outside every
// redaction, where no character outside the redactions was before (to
// 0.25pt), and the same character was under a redaction before, on the
// same line nearby (errors.Is ErrTextMoved). The first happens to text the
// removal cannot reach (in a tiling pattern or a soft mask); the second to
// text it measures with the wrong font (a form XObject without /Resources
// that uses the page's fonts, which MuPDF's rewritten copy of the form
// cannot find, so its glyphs move and the one redacted escapes). Kept text that
// moved, elsewhere on the page or under a box, is not an error (MuPDF's
// rewriting of the page can shift kept text slightly); when the call fails
// anyway, the error counts it in MovedElsewhere. Spaces (real ones, and
// those extraction makes up for gaps), and ligature trailing components and zero-advance marks at the next glyph's
// origin, are not checked. The error comes after the page has changed: the
// page must not be treated as redacted. The check costs one more
// extraction of the page.
//
// Changed from upstream: it takes ApplyOptions. Upstream passed zeroed MuPDF
// options, so no box was drawn, and always removed overlapping images whole.
// Out-of-range enum values return an error.
//
// Also changed: with opts.Text TextRemove, text is removed glyph by glyph
// before MuPDF applies the rest. A glyph goes when either:
//   - its box hits a redaction: the box (the font's own advance by its
//     ascender and descender), shrunk towards its centre by a tenth of each
//     side, overlaps a quad's shape (see AddRedactionQuads), or its shrunk
//     axis-aligned bounds overlap a rect redaction (stock MuPDF's rule); or
//   - text extraction (as RawDict does it by default) puts a character at the
//     glyph's origin whose quad, shrunk the same way, overlaps a redaction.
//
// The second rule is new relative to stock MuPDF. Extraction places glyphs by
// the PDF's /Widths and MuPDF's glyph box uses the font's own advance, so a
// glyph whose /Widths are far narrower than its outline, or a glyph in
// vertical writing, can lie where its RawDict quad says without its box
// overlapping that quad; stock MuPDF then keeps it. Every character RawDict
// reports under a redaction is now removed, for rects and quads alike. In
// vertical writing MuPDF 1.26 extraction puts each quad one advance past the
// glyph's ink, over the next glyph, so redacting a glyph by its quad also
// removes the next glyph (whose ink the black box covers).
//
// Extraction gives a ligature's trailing components ("i" of "fi") and a
// zero-advance mark (an accent with no width) a zero-width quad at the next
// glyph's origin. Such a character under a redaction removes glyphs at its
// origin except one that starts with the next character, so the glyph after
// a redacted ligature or mark stays. A zero-advance mark lying on a redacted
// letter's edge goes with it; a ligature goes when its first component's
// quad is redacted, not for its trailing components alone (as in stock
// MuPDF). A glyph that is genuinely zero-width (no character with an advance
// shares its origin) is matched as before.
//
// Also changed: with opts.Text TextRemove and at least one redaction, the
// page's declared text is removed: /ActualText, /Alt and /E, from every
// marked-content property list the redaction reaches (inline in the page's
// content streams and the form XObjects they draw, and named in their
// /Resources /Properties), and from the structure elements that own the
// page's marked content (through the page's and those forms' /StructParents)
// and their ancestors up to the structure tree root. Declared text repeats
// the glyphs (or describes them), so a redacted word would otherwise survive
// there, invisible to extraction that ignores /ActualText. Stock MuPDF leaves
// inline and named property lists as they are and edits structure elements
// only partly. The cost is the page's accessibility text. Another page that
// shares a named property dictionary keeps it (this page gets a stripped
// copy); a structure ancestor that spans other pages loses its declared text
// for them too.
//
// Also changed, in every Text mode: redaction makes the page draw redacted
// copies of its form XObjects, but a structure element can still name the
// original form, in a marked-content reference (/Stm) or an object reference
// (/Obj), which keeps the unredacted original in the saved file. In the
// structure elements under the page's and its forms' /StructParents and
// /StructParent, and anywhere in the structure tree below its root, a
// reference on this page (its /Pg, or its element's, is this page or absent)
// is moved to the copy when the page drew that form once; otherwise an MCR
// loses its /Stm and an OBJR is removed. The original form object stays for
// other pages that draw it, with their references. Stock MuPDF leaves the
// references to the originals.
//
// The removal reaches what stock MuPDF's reaches: the page's content streams
// and the form XObjects they draw, but not patterns, soft masks, Type3 glyph
// procedures or annotation appearances. Removed glyphs leave a gap; the rest
// of the text does not move. MuPDF then draws the boxes, handles images and
// line art, removes overlapping links and FreeText annotations and deletes the
// redaction annotations, without its own text removal. TextNone goes through
// stock MuPDF unchanged. A page with more than 1048576 extracted characters
// under its redactions gets MuPDF's own text removal instead (each quad's
// bounds against each glyph's box), since the per-glyph pass records each
// one; unlike the region limit, that is bounded by the page's own content,
// so it falls back rather than fails (the check after redacting still
// applies, except for characters that moved, which it then cannot tell).
func (p *Page) ApplyRedactions(opts ApplyOptions) (int, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return 0, errors.New("gomupdf: document closed")
	}
	n, err := d.b.applyRedactions(p.Number, opts)
	var ce changedPageError
	if errors.As(err, &ce) {
		d.badRedact = true
		err = ce.error
	}
	return n, err
}

// changedPageError marks a backend ApplyRedactions error that came after the
// page started to change.
type changedPageError struct{ error }

func (e changedPageError) Unwrap() error { return e.error }

// ErrRedactionFailed is wrapped by the error every save of a Document
// returns after ApplyRedactions failed on it once the page had started to
// change. New relative to upstream.
var ErrRedactionFailed = errors.New("gomupdf: a redaction failed after changing the page; discard this document")

// redactionFailed returns the error saves return after a failed redaction,
// or nil.
func (d *Document) redactionFailed(op string) error {
	if d.badRedact {
		return fmt.Errorf("gomupdf: %s: %w", op, ErrRedactionFailed)
	}
	return nil
}
