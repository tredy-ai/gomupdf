package gomupdf

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// shiftPDF is the shape of pymupdf cms-etc-filled.pdf's footer: a page
// number set with word spacing -0.278 in a text matrix scaled by 7, then
// "0 Tw" before drawing a form that holds the footer line "one two ace",
// with "SECRET" far above. MuPDF's content filter does not write the
// pending "0 Tw" before the Do, so the rewritten page draws the footer with
// the page number's word spacing: each word moves left by 1.946pt per space
// before it, the width of a space.
func shiftPDF() []byte {
	form := "BT /F1 1 Tf 7 0 0 7 20 40 Tm (one two ace) Tj ET"
	return pdfPage("", "/Fm0 6 0 R",
		"BT /F1 1 Tf -0.278 Tw 7 0 0 7 250 20 Tm (1  ) Tj ET q 0 Tw /Fm0 Do Q BT /F1 24 Tf 20 150 Td (SECRET) Tj ET",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
			len(form), form))
}

// Kept text that MuPDF's rewriting of the page moves, far from the
// redaction or into its box, is not an error: the redacted word is gone.
func TestApplyRedactionsShiftedKeptTextIsNoError(t *testing.T) {
	for _, c := range []struct {
		name       string
		from, to   int     // glyphs redacted
		want       string  // glyphs left
		intoRegion bool    // "a" moves into the redacted word's box
		withSpace  bool    // redact a rect that takes the space after the word too
		spaces     float64 // kept spaces before "a", each moving it by 0.278 x 7
	}{
		{"far", 10, 16, "1onetwoace", false, false, 2},
		{"into the box", 4, 7, "1oneaceSECRET", true, false, 2},
		// The space after "two" moves next to the redacted space after "one".
		{"with its space", 1, 4, "1twoaceSECRET", false, true, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, err := OpenStream(shiftPDF())
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			p, err := d.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			before := pageGlyphs(t, p)
			if s := runesOf(before); s != "1onetwoaceSECRET" {
				t.Fatalf("glyphs %q", s)
			}
			region := before[c.from].BBox
			for _, g := range before[c.from:c.to] {
				region = region.IncludeRect(g.BBox)
			}
			add := p.AddRedactionQuads(quadsOf(before[c.from:c.to]), RedactOptions{})
			if c.withSpace {
				r := region
				r.X1 = before[c.to].BBox.X0 - 0.3
				add = p.AddRedaction(r, RedactOptions{})
			}
			if add != nil {
				t.Fatal(add)
			}
			if _, err := p.ApplyRedactions(ApplyOptions{}); err != nil {
				t.Fatalf("ApplyRedactions: %v", err)
			}
			after := pageGlyphs(t, p)
			if s := runesOf(after); s != c.want {
				t.Fatalf("after redaction %q, want %q", s, c.want)
			}
			// The fixture must shift the footer.
			a := after[strings.IndexRune(c.want, 'a')]
			if dx := before[7].Origin.X - a.Origin.X; math.Abs(dx-c.spaces*0.278*7) > 0.01 {
				t.Fatalf("footer a moved by %.3f, want %.3f (fixture no longer shifts)", dx, c.spaces*0.278*7)
			}
			if in := a.BBox.X0 < region.X1-0.2*a.BBox.Width() && a.BBox.Y0 < region.Y1 && a.BBox.Y1 > region.Y0; in != c.intoRegion {
				t.Fatalf("a at %v, redacted %v: into the box %v, want %v", a.BBox, region, in, c.intoRegion)
			}
		})
	}
}
