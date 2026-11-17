package gomupdf

import (
	"math"
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// flatType3 is a Type 3 font whose /FontBBox has no height, so extraction
// gives its glyphs boxes of zero height. Glyphs a and b draw a square.
const flatType3 = `<< /Type /Font /Subtype /Type3 /FontBBox [0 0 0 0] /FontMatrix [0.001 0 0 0.001 0 0]
/CharProcs << /a 6 0 R /b 6 0 R >> /Encoding << /Type /Encoding /Differences [97 /a /b] >>
/FirstChar 97 /LastChar 98 /Widths [600 600] /Resources << >> >>`

func flatType3PDF() []byte {
	return pdfPageFont(flatType3, "/MediaBox [0 0 300 200]",
		"BT /F1 20 Tf 20 100 Td (a) Tj 40 0 Td (b) Tj ET",
		"<< /Length 22 >>\nstream\n600 0 d0 0 0 600 600 re f\nendstream")
}

// A rect built from glyph boxes with no height is empty: AddRedaction
// refuses it, pointing at AddRedactionQuads, which redacts the same glyph.
func TestAddRedactionEmptyRect(t *testing.T) {
	d, err := OpenStream(flatType3PDF())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	cs := pageGlyphs(t, p)
	if runesOf(cs) != "ab" || cs[0].BBox.Height() != 0 {
		t.Fatalf("glyphs %q, first box %v; want ab with zero-height boxes", runesOf(cs), cs[0].BBox)
	}
	err = p.AddRedaction(cs[0].BBox, RedactOptions{})
	if err == nil || !strings.Contains(err.Error(), "AddRedactionQuads") {
		t.Fatalf("AddRedaction(%v) = %v, want an error naming AddRedactionQuads", cs[0].BBox, err)
	}
	if err := p.AddRedactionQuads([]geometry.Quad{cs[0].Quad}, RedactOptions{}); err != nil {
		t.Fatal(err)
	}
	if n, err := p.ApplyRedactions(ApplyOptions{}); err != nil || n != 1 {
		t.Fatalf("ApplyRedactions = %d, %v; want 1 (only the quad redaction was added)", n, err)
	}
	if got := runesOf(pageGlyphs(t, p)); got != "b" {
		t.Errorf("after redacting a by its quad: %q, want b", got)
	}

	nan, inf := math.NaN(), math.Inf(1)
	for _, r := range []geometry.Rect{
		{X0: 10, Y0: 10, X1: 10, Y1: 50},         // zero width
		{X0: 10, Y0: 10, X1: 60, Y1: 10 + 1e-50}, // zero height as float32
		{X0: nan, Y0: 10, X1: 60, Y1: 50},
		{X0: 10, Y0: 10, X1: inf, Y1: 50},
		{X0: 10, Y0: -inf, X1: 60, Y1: 50},
	} {
		if err := p.AddRedaction(r, RedactOptions{}); err == nil {
			t.Errorf("AddRedaction(%v) = nil, want an error", r)
		}
	}
	if err := p.AddRedaction(geometry.Rect{X0: 10, Y0: 10, X1: 60, Y1: 50}, RedactOptions{}); err != nil {
		t.Errorf("AddRedaction of a proper rect: %v", err)
	}
}
