package gomupdf

import (
	"errors"

	"github.com/tredy-ai/gomupdf/geometry"
)

// TextOptions controls how text is extracted from a page by ExtractText and
// RawDictWithOptions. The zero value sets no optional MuPDF structured-text flags and ignores
// /ActualText (see UseActualText).
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

	// UseActualText returns the replacement text a PDF declares with
	// /ActualText (a marked-content property that lets a producer say "these
	// glyphs mean this string") instead of the glyphs actually drawn.
	//
	// Changed from upstream and from MuPDF: MuPDF honours /ActualText by
	// default; this package ignores it (FZ_STEXT_IGNORE_ACTUALTEXT) unless
	// UseActualText is true, so a detector reading the output sees what is on
	// the page, not what a PDF claims is there. A consumer that wants both
	// views extracts twice. Extraction APIs without options (GetText, Words,
	// Search, HTML, ...) always ignore /ActualText.
	//
	// Warning: with UseActualText set, MuPDF 1.26.11 can crash the whole
	// process (SIGSEGV, not a Go error) on crafted input: its structured-text
	// device dereferences a NULL font when the drawn glyphs match a suffix of
	// the declared text and the span is the first text on the page. Enabling
	// this on untrusted input is unsafe; a service that does so MUST run that
	// extraction where a crash is contained, such as a separate process. The
	// default (false) is not affected. See TestActualTextCrashInput.
	UseActualText bool
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
