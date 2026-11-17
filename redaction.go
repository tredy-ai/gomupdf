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
// redacted.
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
// so it falls back rather than fails.
func (p *Page) ApplyRedactions(opts ApplyOptions) (int, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return 0, errors.New("gomupdf: document closed")
	}
	return d.b.applyRedactions(p.Number, opts)
}
