package gomupdf

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// A diamond (its bounding rect's corners lie outside it) and a separate
// square, on newBlankPage's 400x400 page.
var (
	diamond = geometry.Quad{
		UL: geometry.Point{X: 100, Y: 200}, UR: geometry.Point{X: 200, Y: 100},
		LL: geometry.Point{X: 200, Y: 300}, LR: geometry.Point{X: 300, Y: 200},
	}
	square = geometry.NewRect(320, 320, 380, 380).Quad()
)

func TestAddRedactionQuadsErrors(t *testing.T) {
	d, p := newBlankPage(t)
	nan, inf := math.NaN(), math.Inf(1)
	for _, qs := range [][]geometry.Quad{
		nil,
		{},
		{{UL: geometry.Point{X: nan}}},
		{square, {LR: geometry.Point{Y: inf}}},
		{{UR: geometry.Point{X: 1e39}}}, // finite float64, +Inf as float32
	} {
		if err := p.AddRedactionQuads(qs, RedactOptions{}); err == nil {
			t.Errorf("AddRedactionQuads(%v): want error", qs)
		}
	}
	if a, err := p.Annotations(); err != nil || len(a) != 0 {
		t.Errorf("rejected input left annotations %v (err %v)", a, err)
	}
	d.Close()
	if err := p.AddRedactionQuads([]geometry.Quad{square}, RedactOptions{}); err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("closed document: err = %v", err)
	}
}

// One annotation for all quads. pdf_update_annot sets /Rect to the union of
// the quads' bounds expanded by 1pt (its outline), overriding any /Rect set
// earlier.
func TestAddRedactionQuadsAnnotation(t *testing.T) {
	d, p := newBlankPage(t)
	defer d.Close()
	if err := p.AddRedactionQuads([]geometry.Quad{diamond, square}, RedactOptions{}); err != nil {
		t.Fatal(err)
	}
	a, err := p.Annotations()
	if err != nil {
		t.Fatal(err)
	}
	want := geometry.NewRect(99, 99, 381, 381) // union (100,100)-(380,380) + 1pt
	if len(a) != 1 || a[0].Type != "Redact" {
		t.Fatalf("annotations = %+v, want one Redact", a)
	}
	got := a[0].Rect
	if math.Abs(got.X0-want.X0) > 1e-3 || math.Abs(got.Y0-want.Y0) > 1e-3 ||
		math.Abs(got.X1-want.X1) > 1e-3 || math.Abs(got.Y1-want.Y1) > 1e-3 {
		t.Errorf("rect = %v, want %v", got, want)
	}
}

// With the default ApplyOptions MuPDF fills each quad's own shape, not the
// annotation's bounding rect.
func TestAddRedactionQuadsBoxFollowsQuads(t *testing.T) {
	d, p := newBlankPage(t)
	defer d.Close()
	if err := p.AddRedactionQuads([]geometry.Quad{diamond, square}, RedactOptions{}); err != nil {
		t.Fatal(err)
	}
	if n, err := p.ApplyRedactions(ApplyOptions{}); err != nil || n != 1 {
		t.Fatalf("ApplyRedactions = %d, %v", n, err)
	}
	for _, c := range []struct {
		x, y int
		want [3]byte
	}{
		{200, 200, black}, // diamond centre
		{350, 350, black}, // square centre
		{110, 110, white}, // diamond's bounding-rect corner
		{300, 300, white}, // inside the union rect, outside both quads
	} {
		if got := px(t, p, c.x, c.y); got != c.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", c.x, c.y, got, c.want)
		}
	}
}

// A zero-area quad (a point at the centre of 'e') is accepted and still
// removes the glyph it touches.
func TestAddRedactionQuadsZeroArea(t *testing.T) {
	content := "BT /F1 12 Tf 20 100 Td (Hello) Tj ET"
	qs, _ := helloGeometry(t, content)
	c := qs[1].Rect()
	pt := geometry.Point{X: (c.X0 + c.X1) / 2, Y: (c.Y0 + c.Y1) / 2}
	got := afterRedaction(t, content, func(p *Page) error {
		return p.AddRedactionQuads([]geometry.Quad{{UL: pt, UR: pt, LL: pt, LR: pt}}, RedactOptions{})
	})
	if got != "Hllo" {
		t.Errorf("left %q, want %q", got, "Hllo")
	}
}

// slanted is "aaa Hello bbb" in 12pt Helvetica at 30 degrees, with extra
// character spacing tc and an optional extra content-stream fragment.
func slanted(tc float64, extra string) string {
	return fmt.Sprintf("BT /F1 12 Tf %g Tc 0.8660254 0.5 -0.5 0.8660254 50 50 Tm (aaa Hello bbb) Tj ET %s", tc, extra)
}

const helvetica = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"

// helloGeometry returns the quads of "Hello" in content and their union.
func helloGeometry(t *testing.T, content string) ([]geometry.Quad, geometry.Rect) {
	t.Helper()
	d := onePagePDF(t, helvetica, content)
	defer d.Close()
	p, _ := d.LoadPage(0)
	var qs []geometry.Quad
	var r geometry.Rect
	for _, b := range rawDictOpts(t, p, TextOptions{}) {
		for _, l := range b.Lines {
			for _, c := range l.Chars {
				if strings.ContainsRune("Hello", c.Rune) {
					if len(qs) == 0 {
						r = c.BBox
					}
					qs = append(qs, c.Quad)
					r = r.IncludeRect(c.BBox)
				}
			}
		}
	}
	if len(qs) != 5 {
		t.Fatalf("found %d chars of Hello", len(qs))
	}
	return qs, r
}

// afterRedaction applies one redaction added by add (default ApplyOptions)
// and returns the remaining characters, spaces dropped.
func afterRedaction(t *testing.T, content string, add func(*Page) error) string {
	t.Helper()
	d := onePagePDF(t, helvetica, content)
	defer d.Close()
	p, _ := d.LoadPage(0)
	if err := add(p); err != nil {
		t.Fatal(err)
	}
	if n, err := p.ApplyRedactions(ApplyOptions{}); err != nil || n != 1 {
		t.Fatalf("ApplyRedactions = %d, %v", n, err)
	}
	return strings.ReplaceAll(pageChars(rawDictOpts(t, p, TextOptions{})), " ", "")
}

// Documents the MuPDF 1.25.6 limitation: on 30 degree text, redacting the
// exact quads of "Hello" removes the same characters as a rect around the
// word, including one neighbour on each side, because MuPDF tests glyph
// bounding boxes against each quad's bounds. Only with wide spacing do the
// neighbours survive, and then they survive the rect too. The spec asked for
// quads to keep the neighbours; MuPDF 1.25.6 cannot.
func TestAddRedactionQuadsSlantedNeighbours(t *testing.T) {
	for _, c := range []struct {
		tc   float64
		want string
	}{
		{0, "aabb"},   // one 'a' and one 'b' lost
		{5, "aaabbb"}, // neighbours kept
	} {
		content := slanted(c.tc, "")
		qs, r := helloGeometry(t, content)
		byQuads := afterRedaction(t, content, func(p *Page) error { return p.AddRedactionQuads(qs, RedactOptions{}) })
		byRect := afterRedaction(t, content, func(p *Page) error { return p.AddRedaction(r, RedactOptions{}) })
		if byQuads != c.want || byRect != c.want {
			t.Errorf("Tc %g: quads left %q, rect left %q, want %q for both", c.tc, byQuads, byRect, c.want)
		}
	}
}

// What quads do buy on 1.25.6: content inside the word's bounding rect but
// outside every character quad's bounds is kept. A 3pt "Z" sits in the top
// left corner of the rect around slanted "Hello".
func TestAddRedactionQuadsKeepCornerText(t *testing.T) {
	qs, r := helloGeometry(t, slanted(0, ""))
	content := slanted(0, fmt.Sprintf("BT /F1 3 Tf %g %g Td (Z) Tj ET", r.X0+0.5, 200-r.Y0-3))
	if got := afterRedaction(t, content, func(p *Page) error { return p.AddRedactionQuads(qs, RedactOptions{}) }); got != "aabbZ" {
		t.Errorf("quads left %q, want %q", got, "aabbZ")
	}
	if got := afterRedaction(t, content, func(p *Page) error { return p.AddRedaction(r, RedactOptions{}) }); got != "aabb" {
		t.Errorf("rect left %q, want %q", got, "aabb")
	}
}
