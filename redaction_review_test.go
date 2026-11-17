package gomupdf

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// applyFileRedactions opens pdf, applies the redaction annotations it already
// carries on page 0 (want of them), saves uncompressed with Garbage 1 and
// returns the reopened page's characters and the saved bytes.
func applyFileRedactions(t *testing.T, pdf []byte, opts ApplyOptions, want int) ([]Char, []byte) {
	t.Helper()
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := p.ApplyRedactions(opts); err != nil || n != want {
		t.Fatalf("ApplyRedactions = %d, %v; want %d", n, err, want)
	}
	out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
	if err != nil {
		t.Fatal(err)
	}
	return pdfChars(t, out), out
}

// A page whose redaction annotations share one large indirect /QuadPoints
// array, more quads in all than the per-page region limit. The per-glyph pass
// must not build a region per quad; the page falls back to MuPDF's own text
// removal, which tests each quad's bounds and so also loses the neighbours of
// slanted "Hello".
func TestRedactionRegionLimit(t *testing.T) {
	const annots, repeat = 20, 800 // 20 x 800 x 5 = 80000 quads > maxRedactRegions
	content := slantedAt("aaa Hello bbb", 50, 50)
	hello := pdfChars(t, pdfPage("", "", content))[4:9]
	var qp strings.Builder
	for range repeat {
		for _, c := range hello {
			q := c.Quad // page space; this page's PDF space has y up from 200
			fmt.Fprintf(&qp, "%g %g %g %g %g %g %g %g ",
				q.UL.X, 200-q.UL.Y, q.UR.X, 200-q.UR.Y, q.LL.X, 200-q.LL.Y, q.LR.X, 200-q.LR.Y)
		}
	}
	r := boundsOf(hello)
	var refs strings.Builder
	var extra []string
	arrayObj := 6 + annots
	for i := range annots {
		fmt.Fprintf(&refs, "%d 0 R ", 6+i)
		extra = append(extra, fmt.Sprintf("<< /Type /Annot /Subtype /Redact /Rect [%g %g %g %g] /QuadPoints %d 0 R >>",
			r.X0, 200-r.Y1, r.X1, 200-r.Y0, arrayObj))
	}
	extra = append(extra, "["+qp.String()+"]")
	pdf := pdfPage("/MediaBox [0 0 300 200] /Annots ["+refs.String()+"]", "", content, extra...)
	got, _ := applyFileRedactions(t, pdf, ApplyOptions{NoBox: true}, annots)
	// Stock removal: the word and one neighbour on each side.
	if s := runesOf(got); s != "aabb" {
		t.Errorf("left %q, want %q (MuPDF's own text removal)", s, "aabb")
	}
}

// pdfPageFont is pdfPage with font as /F1 instead of Helvetica.
func pdfPageFont(font, pageAttrs, content string, extra ...string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R %s /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>", pageAttrs),
		font,
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	return buildPDF(append(objs, extra...)...)
}

// glyphsOf drops the spaces extraction inserts between spaced-out glyphs.
func glyphsOf(cs []Char) []Char {
	var out []Char
	for _, c := range cs {
		if c.Rune != ' ' {
			out = append(out, c)
		}
	}
	return out
}

// tm returns a text matrix rotated deg degrees, at (x, y).
func tm(deg, x, y float64) string {
	s, c := math.Sincos(deg * math.Pi / 180)
	return fmt.Sprintf("%.7f %.7f %.7f %.7f %g %g Tm", c, s, -s, c, x, y)
}

// redactionHits redacts glyphs[targets] of pdf by their quads (one
// annotation) and by their Char.BBox rects (one annotation each), and
// requires every target gone. keepOthers also requires every other glyph
// kept; otherwise the survivors are returned for the caller to check.
func redactionHits(t *testing.T, label string, pdf []byte, targets []int, keepOthers bool) (byQuads, byRects string) {
	t.Helper()
	gs := glyphsOf(pdfChars(t, pdf))
	var tq []geometry.Quad
	var tr []func(*Page) error
	want := ""
	for i, g := range gs {
		if slices.Contains(targets, i) {
			tq = append(tq, g.Quad)
			tr = append(tr, addRect(g.BBox))
		} else {
			want += string(g.Rune)
		}
	}
	for k, adds := range [][]func(*Page) error{{addQuads(tq)}, tr} {
		got, _ := applySaveReopen(t, pdf, ApplyOptions{}, adds...)
		left := runesOf(got)
		if k == 0 {
			byQuads = left
		} else {
			byRects = left
		}
		shape := [...]string{"quads", "bbox rects"}[k]
		for _, i := range targets {
			// Runes repeat ("SECRET"): no more of the target's rune than
			// the non-targets have.
			if r := string(gs[i].Rune); strings.Count(left, r) > strings.Count(want, r) {
				t.Errorf("%s, %s of %v: left %q, target %q kept", label, shape, targets, left, gs[i].Rune)
			}
		}
		if keepOthers && left != want {
			t.Errorf("%s, %s of %v: left %q, want %q", label, shape, targets, left, want)
		}
	}
	return byQuads, byRects
}

// Helvetica with /Widths 30: extraction places each glyph in a 0.36pt slot,
// while MuPDF's glyph box (and the ink) is the full Helvetica advance. Wide
// Tc keeps the inks apart, so redacting glyphs by their own extraction
// geometry must remove exactly those glyphs, at every angle.
func TestRedactionNarrowWidths(t *testing.T) {
	font := "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 0 /LastChar 255 /Widths [" + strings.Repeat("30 ", 256) + "] >>"
	for _, deg := range []float64{0, 5, 30, 45, 90} {
		pdf := pdfPageFont(font, "/MediaBox [0 0 400 400]", "BT /F1 12 Tf 32 Tc "+tm(deg, 60, 60)+" (aSECRETb) Tj ET")
		if s := runesOf(pdfChars(t, pdf)); s != "aSECRETb" {
			t.Fatalf("%g degrees: fixture chars %q", deg, s)
		}
		label := fmt.Sprintf("%g degrees", deg)
		for i := 1; i <= 6; i++ {
			redactionHits(t, label, pdf, []int{i}, true)
		}
		redactionHits(t, label, pdf, []int{1, 2, 3, 4, 5, 6}, true)
	}
}

// Two glyphs drawn at the same origin are both removed by a redaction of
// one's quad.
func TestRedactionSameOrigin(t *testing.T) {
	pdf := pdfPage("", "", "BT /F1 12 Tf 50 100 Td (aaa ) Tj ET BT /F1 12 Tf 73.352 100 Td (X) Tj ET BT /F1 12 Tf 73.352 100 Td (Y) Tj ET")
	gs := glyphsOf(pdfChars(t, pdf))
	if runesOf(gs) != "aaaXY" {
		t.Fatalf("fixture chars %q", runesOf(gs))
	}
	got, _ := applySaveReopen(t, pdf, ApplyOptions{}, addQuads([]geometry.Quad{gs[3].Quad}))
	if s := runesOf(got); s != "aaa" {
		t.Errorf("left %q, want %q", s, "aaa")
	}
}

// vfont is a non-embedded Adobe-Japan1 font with vertical writing.
const vfont = `<< /Type /Font /Subtype /Type0 /BaseFont /MS-Mincho /Encoding /Identity-V /DescendantFonts [<< /Type /Font /Subtype /CIDFontType0 /BaseFont /MS-Mincho /CIDSystemInfo << /Registry (Adobe) /Ordering (Japan1) /Supplement 2 >> /FontDescriptor << /Type /FontDescriptor /FontName /MS-Mincho /Flags 4 /FontBBox [0 -141 1000 859] /ItalicAngle 0 /Ascent 859 /Descent -141 /CapHeight 700 /StemV 80 >> /DW 1000 >>] >>`

// Vertical writing, upright and rotated: redacting glyphs by their
// extraction geometry removes them. MuPDF 1.26.11 extraction places each
// vertical glyph's quad one advance below its ink, over the next glyph's ink,
// so the next glyph goes too (MuPDF's own box rule removes it, and the black
// box covers it). Pinned here; rects at 30 degrees remove more still (their
// axis-aligned bounds).
func TestRedactionVertical(t *testing.T) {
	for _, deg := range []float64{0, 30} {
		pdf := pdfPageFont(vfont, "/MediaBox [0 0 400 400]", "BT /F1 20 Tf "+tm(deg, 150, 350)+" <03430344034503460347> Tj ET")
		gs := glyphsOf(pdfChars(t, pdf))
		if len(gs) != 5 {
			t.Fatalf("%g degrees: fixture chars %q", deg, runesOf(gs))
		}
		all := []rune(runesOf(gs))
		// without returns all but glyphs from..to (inclusive).
		without := func(from, to int) string { return string(all[:from]) + string(all[to+1:]) }
		label := fmt.Sprintf("vertical %g degrees", deg)
		for i := range 4 {
			q, r := redactionHits(t, label, pdf, []int{i}, false)
			if q != without(i, i+1) || (deg == 0 && r != q) {
				t.Errorf("%s glyph %d: quads left %q, rects left %q, want %q", label, i, q, r, without(i, i+1))
			}
		}
		q, r := redactionHits(t, label, pdf, []int{1, 2, 3}, false)
		if q != without(1, 4) || (deg == 0 && r != q) {
			t.Errorf("%s word: quads left %q, rects left %q, want %q", label, q, r, without(1, 4))
		}
	}
}

// The origin rule in every space the filter and extraction disagree on:
// rotated and offset pages, /UserUnit, and text in form XObjects that are
// rotated, nested and drawn twice. The narrow-/Widths font makes the origin
// the only way to find the glyph.
func TestRedactionNarrowWidthsSpaces(t *testing.T) {
	font := "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 0 /LastChar 255 /Widths [" + strings.Repeat("30 ", 256) + "] >>"
	text := func(x, y float64) string { return "BT /F1 12 Tf 32 Tc " + tm(30, x, y) + " (aSECRETb) Tj ET" }
	form := func(content string) string {
		return fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [-1000 -1000 1000 1000] /Matrix [0.9659258 0.258819 -0.258819 0.9659258 5 5] /Resources << /Font << /F1 4 0 R >> /XObject << /Inner 7 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(content), content)
	}
	page := func(attrs, content string) []byte {
		return buildPDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R %s /Resources << /Font << /F1 4 0 R >> /XObject << /Fm0 6 0 R >> >> /Contents 5 0 R >>", attrs),
			font,
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			form(text(0, 0)+" /Inner Do"),
			form(text(0, 300)),
		)
	}
	for _, c := range []struct {
		name, attrs, content string
	}{
		{"rotate 90", "/MediaBox [0 0 400 400] /Rotate 90", text(60, 60)},
		{"offset mediabox", "/MediaBox [100 -300 500 100]", text(160, -240)},
		{"userunit", "/MediaBox [0 0 400 400] /UserUnit 2.5", text(60, 60)},
		{"nested forms drawn twice", "/MediaBox [0 0 1200 1200] /Rotate 270",
			"q 1 0 0 1 60 60 cm /Fm0 Do Q q 0.7071068 0.7071068 -0.7071068 0.7071068 800 100 cm /Fm0 Do Q"},
	} {
		t.Run(c.name, func(t *testing.T) {
			pdf := page(c.attrs, c.content)
			gs := glyphsOf(pdfChars(t, pdf))
			if len(gs)%8 != 0 || len(gs) == 0 {
				t.Fatalf("fixture chars %q", runesOf(gs))
			}
			// Each copy of the word: one glyph, then the whole word.
			for w := 0; w < len(gs); w += 8 {
				redactionHits(t, c.name, pdf, []int{w + 3}, true)
				redactionHits(t, c.name, pdf, []int{w + 1, w + 2, w + 3, w + 4, w + 5, w + 6}, true)
			}
		})
	}
}
