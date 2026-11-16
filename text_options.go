package gomupdf

import (
	"errors"

	"github.com/tredy-ai/gomupdf/geometry"
)

// TextOptions controls how text is extracted from a page by ExtractText and
// RawDictWithOptions. The zero value sets no MuPDF structured-text flags.
//
// MuPDF's FZ_STEXT_CLIP is never set implicitly, so text outside the page box
// is returned unless the caller passes Clip.
type TextOptions struct {
	Clip               *geometry.Rect // if non-nil, only text intersecting this rect (PDF points) is returned
	Dehyphenate        bool           // join words split by a soft hyphen at line end
	PreserveWhitespace bool           // keep original whitespace instead of normalizing
	PreserveLigatures  bool           // keep ligatures (fi, fl, …) instead of expanding
	PreserveImages     bool           // keep image placeholders in the output
	InhibitSpaces      bool           // do not synthesize spaces between glyphs

	// AccurateBBoxes computes character bboxes from the glyph outlines
	// (FZ_STEXT_ACCURATE_BBOXES) instead of the font's ascender and
	// descender. New in the tredy-ai fork.
	AccurateBBoxes bool
}

// ExtractText returns the page's text honoring opts (reading order, lines separated by '\n').
func (p *Page) ExtractText(opts TextOptions) (string, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return "", errors.New("gomupdf: document closed")
	}
	return d.b.extractTextRaw(p.Number, opts)
}
