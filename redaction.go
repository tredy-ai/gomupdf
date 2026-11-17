package gomupdf

import (
	"errors"
	"fmt"
	"math"

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
func (p *Page) AddRedaction(rect geometry.Rect, opts RedactOptions) error {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	return d.b.addRedaction(p.Number, [4]float64{rect.X0, rect.Y0, rect.X1, rect.Y1})
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

// ApplyRedactions permanently removes the content under every redaction
// annotation on the page according to opts, then deletes the redaction marks.
// Returns the number of redactions applied. Effective on Save.
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
// The removal reaches what stock MuPDF's reaches: the page's content streams
// and the form XObjects they draw, but not patterns, soft masks, Type3 glyph
// procedures or annotation appearances. Removed glyphs leave a gap; the rest
// of the text does not move. MuPDF then draws the boxes, handles images and
// line art, removes overlapping links and FreeText annotations and deletes the
// redaction annotations, without its own text removal. TextNone goes through
// stock MuPDF unchanged. A page whose redactions have more than 65536 quads
// and rects in all (annotations in a file can share one huge /QuadPoints
// array), or more than 1048576 extracted characters under them, gets MuPDF's
// own text removal instead (each quad's bounds against each glyph's box): the
// per-glyph pass would need memory per quad or character.
func (p *Page) ApplyRedactions(opts ApplyOptions) (int, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return 0, errors.New("gomupdf: document closed")
	}
	return d.b.applyRedactions(p.Number, opts)
}
