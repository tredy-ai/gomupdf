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
// Precision on MuPDF 1.25.6: ApplyRedactions draws the black box over each
// quad's own shape, but it removes a glyph when the glyph's axis-aligned
// bounding box, shrunk by 10% on each side, overlaps the axis-aligned bounds of
// any quad. Images and line art are matched against, and image pixels blanked
// over, each quad's bounds too. So each quad removes content exactly as
// AddRedaction of its bounds would. On rotated or
// skewed text, redacting the quads of a word's characters still removes
// neighbouring characters whose bounding boxes overlap theirs, and those
// characters disappear outside the drawn box. What quads add over one rect
// around the whole word is that content inside that rect but outside every
// quad's bounds is kept.
//
// An empty slice or a non-finite coordinate (after conversion to float32)
// returns an error: MuPDF treats a NaN quad as empty and would silently redact
// nothing. Zero-area quads are accepted; MuPDF still removes glyphs they touch.
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
func (p *Page) ApplyRedactions(opts ApplyOptions) (int, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return 0, errors.New("gomupdf: document closed")
	}
	return d.b.applyRedactions(p.Number, opts)
}
