package gomupdf

import (
	"bytes"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// pdfPage builds a one-page PDF. pageAttrs replaces the default MediaBox
// (and may add /Rotate); xobjects is the page's /XObject resource dict body;
// extra objects are numbered from 6. Object 4 is Helvetica as /F1, object 5
// the page content.
func pdfPage(pageAttrs, xobjects, content string, extra ...string) []byte {
	if pageAttrs == "" {
		pageAttrs = "/MediaBox [0 0 300 200]"
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R %s /Resources << /Font << /F1 4 0 R >> /XObject << %s >> >> /Contents 5 0 R >>", pageAttrs, xobjects),
		helvetica,
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	return buildPDF(append(objs, extra...)...)
}

// slantedAt shows s in 12pt /F1 rotated 30 degrees, starting at (x, y) in PDF
// user space.
func slantedAt(s string, x, y float64) string {
	return fmt.Sprintf("BT /F1 12 Tf 0.8660254 0.5 -0.5 0.8660254 %g %g Tm (%s) Tj ET", x, y, s)
}

// pdfChars returns page 0's characters, spaces included.
func pdfChars(t *testing.T, pdf []byte) []Char {
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
	var out []Char
	for _, b := range rawDictOpts(t, p, TextOptions{}) {
		for _, l := range b.Lines {
			out = append(out, l.Chars...)
		}
	}
	return out
}

// runesOf returns the characters' runes with spaces dropped.
func runesOf(cs []Char) string {
	var b strings.Builder
	for _, c := range cs {
		if c.Rune != ' ' {
			b.WriteRune(c.Rune)
		}
	}
	return b.String()
}

func quadsOf(cs []Char) []geometry.Quad {
	qs := make([]geometry.Quad, len(cs))
	for i, c := range cs {
		qs[i] = c.Quad
	}
	return qs
}

func boundsOf(cs []Char) geometry.Rect {
	r := cs[0].BBox
	for _, c := range cs[1:] {
		r = r.IncludeRect(c.BBox)
	}
	return r
}

func addQuads(qs []geometry.Quad) func(*Page) error {
	return func(p *Page) error { return p.AddRedactionQuads(qs, RedactOptions{}) }
}

func addRect(r geometry.Rect) func(*Page) error {
	return func(p *Page) error { return p.AddRedaction(r, RedactOptions{}) }
}

// applySaveReopen adds the redactions, applies them with opts, saves without
// compression or garbage collection beyond dropping unreferenced objects,
// and returns the reopened page's characters and the saved bytes.
func applySaveReopen(t *testing.T, pdf []byte, opts ApplyOptions, adds ...func(*Page) error) ([]Char, []byte) {
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
	for _, add := range adds {
		if err := add(p); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := p.ApplyRedactions(opts); err != nil || n != len(adds) {
		t.Fatalf("ApplyRedactions = %d, %v; want %d", n, err, len(adds))
	}
	out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
	if err != nil {
		t.Fatal(err)
	}
	return pdfChars(t, out), out
}

// near reports whether two points agree to 0.01pt.
func near(a, b geometry.Point) bool {
	return math.Abs(a.X-b.X) < 0.01 && math.Abs(a.Y-b.Y) < 0.01
}

// checkSurvivors fails unless got is exactly want (the characters outside the
// redacted range), each at its original origin and quad.
func checkSurvivors(t *testing.T, label string, got, want []Char) {
	t.Helper()
	if runesOf(got) != runesOf(want) {
		t.Errorf("%s: left %q, want %q", label, runesOf(got), runesOf(want))
		return
	}
	var g, w []Char
	for _, c := range got {
		if c.Rune != ' ' {
			g = append(g, c)
		}
	}
	for _, c := range want {
		if c.Rune != ' ' {
			w = append(w, c)
		}
	}
	for i := range w {
		if !near(g[i].Origin, w[i].Origin) || !near(g[i].Quad.UL, w[i].Quad.UL) || !near(g[i].Quad.UR, w[i].Quad.UR) ||
			!near(g[i].Quad.LL, w[i].Quad.LL) || !near(g[i].Quad.LR, w[i].Quad.LR) {
			t.Errorf("%s: %q moved from %+v / %+v to %+v / %+v", label, w[i].Rune, w[i].Origin, w[i].Quad, g[i].Origin, g[i].Quad)
		}
	}
}

// perGlyphCase redacts chars [from, to) of page 0 of pdf by their quads and
// requires exactly those to go, the rest to stay in place; a rect redaction
// over their bounds must lose at least one neighbour (else the fixture does
// not show the difference) when rectLoses is set.
func perGlyphCase(t *testing.T, pdf []byte, from, to int, rectLoses bool) {
	t.Helper()
	before := pdfChars(t, pdf)
	if to > len(before) {
		t.Fatalf("fixture has %d chars (%q), want at least %d", len(before), runesOf(before), to)
	}
	target := before[from:to]
	want := append(append([]Char{}, before[:from]...), before[to:]...)
	got, _ := applySaveReopen(t, pdf, ApplyOptions{}, addQuads(quadsOf(target)))
	checkSurvivors(t, "quads of "+strconv.Quote(runesOf(target)), got, want)
	if rectLoses {
		byRect, _ := applySaveReopen(t, pdf, ApplyOptions{}, addRect(boundsOf(target)))
		if len(runesOf(byRect)) >= len(runesOf(want)) {
			t.Errorf("rect over %q left %q: the fixture no longer shows quads beating rects", runesOf(target), runesOf(byRect))
		}
	}
}

// The point of AddRedactionQuads: on 30 degree text, redacting the character
// quads of a word removes exactly that word, while a rect over the word's
// bounds also removes neighbours.
func TestQuadRedactionPerGlyph(t *testing.T) {
	for _, c := range []struct {
		text     string
		from, to int
	}{
		{"aaa Hello bbb", 4, 9},
		{"aaaHellobbb", 3, 8},
		{"abcdefg", 3, 4},
	} {
		t.Run(c.text, func(t *testing.T) {
			perGlyphCase(t, pdfPage("", "", slantedAt(c.text, 50, 50)), c.from, c.to, true)
		})
	}
	// TJ kerning (0.6pt of overlap at the word's edges, within the tenth
	// each glyph is shrunk by) and word spacing; and "Hello" as a text object
	// of its own, which is emptied. Both 30 degrees, laid out as above.
	tm := "0.8660254 0.5 -0.5 0.8660254"
	for name, content := range map[string]string{
		"kerned": "BT /F1 12 Tf 3 Tw " + tm + " 50 50 Tm [(aaa ) 50 (Hello) -40 ( bbb)] TJ ET",
		"own text object": "BT /F1 12 Tf " + tm + " 50 50 Tm (aaa ) Tj ET " +
			"BT /F1 12 Tf " + tm + " 70.2234 61.676 Tm (Hello) Tj ET " +
			"BT /F1 12 Tf " + tm + " 93.8971 75.344 Tm ( bbb) Tj ET",
	} {
		t.Run(name, func(t *testing.T) {
			pdf := pdfPage("", "", content)
			if got := runesOf(pdfChars(t, pdf)); got != "aaaHellobbb" {
				t.Fatalf("fixture chars %q", got)
			}
			perGlyphCase(t, pdf, 4, 9, false)
		})
	}
}

// A glyph whose quad has no height (text matrix with zero vertical scale) is
// a segment along the baseline; a convex quad crossing it removes it.
func TestQuadRedactionZeroHeightGlyph(t *testing.T) {
	// 12pt Helvetica from x=50 on y=100 (PDF space): "aaa " is 23.352pt,
	// "H" 8.664pt, so H spans x 73.352..82.016, page y 100.
	pdf := pdfPage("", "", "BT /F1 12 Tf 1 0 0 0 50 100 Tm (aaa Hello bbb) Tj ET")
	c := geometry.Point{X: 77.7, Y: 100}
	diamond := geometry.Quad{
		UL: geometry.Point{X: c.X - 3, Y: c.Y}, UR: geometry.Point{X: c.X, Y: c.Y - 3},
		LL: geometry.Point{X: c.X, Y: c.Y + 3}, LR: geometry.Point{X: c.X + 3, Y: c.Y},
	}
	_, out := applySaveReopen(t, pdf, ApplyOptions{}, addQuads([]geometry.Quad{diamond}))
	if s := strings.ReplaceAll(shownText(t, out), " ", ""); s != "aaaellobbb" {
		t.Errorf("content streams show %q, want %q", s, "aaaellobbb")
	}
}

// Rotated pages and MediaBoxes not at the origin: quads from RawDict (page
// space) still meet the glyphs (PDF user space) in one space.
func TestQuadRedactionPageSpaces(t *testing.T) {
	for _, c := range []struct {
		name, attrs string
		x, y        float64
	}{
		{"rotate 90", "/MediaBox [0 0 300 200] /Rotate 90", 50, 50},
		{"rotate 270", "/MediaBox [0 0 300 200] /Rotate 270", 50, 50},
		{"offset mediabox", "/MediaBox [100 50 400 250]", 150, 100},
		{"offset rotate 90", "/MediaBox [-200 -300 100 -100] /Rotate 90", -150, -250},
	} {
		t.Run(c.name, func(t *testing.T) {
			perGlyphCase(t, pdfPage(c.attrs, "", slantedAt("aaa Hello bbb", c.x, c.y)), 4, 9, true)
		})
	}
}

// Text inside a form XObject (with its own /Matrix, drawn under a cm) is
// reached and redacted with the same precision.
func TestQuadRedactionFormXObject(t *testing.T) {
	form := slantedAt("aaa Hello bbb", 20, 20)
	pdf := pdfPage("", "/Fm0 6 0 R", "q 1 0 0 1 10 15 cm /Fm0 Do Q",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 300 200] /Matrix [1 0 0 1 5 5] /Resources << /Font << /F1 4 0 R >> >> /Length %d >>\nstream\n%s\nendstream", len(form), form))
	perGlyphCase(t, pdf, 4, 9, true)
}

// Horizontal text: a quad redaction and a rect redaction of the same word
// remove the same characters.
func TestQuadRedactionHorizontalMatchesRect(t *testing.T) {
	pdf := pdfPage("", "", "BT /F1 12 Tf 20 100 Td (aaa Hello bbb) Tj ET BT /F1 12 Tf 20 60 Td (aaaHellobbb) Tj ET")
	before := pdfChars(t, pdf)
	for _, r := range [][2]int{{4, 9}, {16, 21}} {
		target := before[r[0]:r[1]]
		if runesOf(target) != "Hello" {
			t.Fatalf("chars %v are %q", r, runesOf(target))
		}
		byQuads, _ := applySaveReopen(t, pdf, ApplyOptions{}, addQuads(quadsOf(target)))
		byRect, _ := applySaveReopen(t, pdf, ApplyOptions{}, addRect(boundsOf(target)))
		if runesOf(byQuads) != runesOf(byRect) {
			t.Errorf("chars %v: quads left %q, rect left %q", r, runesOf(byQuads), runesOf(byRect))
		}
	}
}

// A zero-width glyph (all /Widths 0, spaced with Tc) inside a convex quad is
// removed; its zero-width neighbours outside the quad stay. The sanitize
// filter sizes each glyph by the font's own advance, not /Widths (the ink
// still extends that far), so Tc is wider than any glyph's advance.
func TestQuadRedactionZeroWidthGlyph(t *testing.T) {
	font := "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 0 /LastChar 255 /Widths [" + strings.Repeat("0 ", 256) + "] >>"
	content := "BT /F1 12 Tf 14 Tc 0.8660254 0.5 -0.5 0.8660254 50 50 Tm (aaHbb) Tj ET"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		font,
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	}
	pdf := buildPDF(objs...)
	before := pdfChars(t, pdf)
	if runesOf(before) != "aaHbb" {
		t.Fatalf("fixture chars %q", runesOf(before))
	}
	// RawDict adds a space between the spaced-out glyphs.
	hi := 0
	for before[hi].Rune != 'H' {
		hi++
	}
	h := before[hi].Quad
	if w := math.Hypot(h.UR.X-h.UL.X, h.UR.Y-h.UL.Y); w > 1e-3 {
		t.Fatalf("H is %gpt wide, want zero width", w)
	}
	c := geometry.Point{X: (h.UL.X + h.LR.X) / 2, Y: (h.UL.Y + h.LR.Y) / 2}
	diamond := geometry.Quad{
		UL: geometry.Point{X: c.X - 3, Y: c.Y}, UR: geometry.Point{X: c.X, Y: c.Y - 3},
		LL: geometry.Point{X: c.X, Y: c.Y + 3}, LR: geometry.Point{X: c.X + 3, Y: c.Y},
	}
	got, _ := applySaveReopen(t, pdf, ApplyOptions{}, addQuads([]geometry.Quad{diamond}))
	checkSurvivors(t, "diamond on zero-width H", got, append(append([]Char{}, before[:hi]...), before[hi+1:]...))
}

// One rect and one quad redaction on the same page: the quad is precise, the
// rect removes what it removes on its own.
func TestQuadRedactionMixedWithRect(t *testing.T) {
	pdf := pdfPage("", "", slantedAt("aaa Hello bbb", 50, 30)+" "+slantedAt("ccc WUW ddd", 150, 30))
	before := pdfChars(t, pdf)
	if runesOf(before) != "aaaHellobbbcccWUWddd" {
		t.Fatalf("fixture chars %q", runesOf(before))
	}
	hello, wuw := before[4:9], before[18:21]
	rectOnly, _ := applySaveReopen(t, pdf, ApplyOptions{}, addRect(boundsOf(wuw)))
	if !strings.HasPrefix(runesOf(rectOnly), "aaaHellobbb") || len(runesOf(rectOnly)) >= 17 {
		t.Fatalf("rect alone left %q; want the first line whole and a neighbour of WUW lost", runesOf(rectOnly))
	}
	want := "aaabbb" + strings.TrimPrefix(runesOf(rectOnly), "aaaHellobbb")
	got, _ := applySaveReopen(t, pdf, ApplyOptions{}, addQuads(quadsOf(hello)), addRect(boundsOf(wuw)))
	if runesOf(got) != want {
		t.Errorf("mixed left %q, want %q", runesOf(got), want)
	}
}

// Corners in the wrong order (a self-crossing quad) fall back to the quad's
// bounds, which never removes less; a mirrored quad (corners in the other
// winding) is still convex and stays precise.
func TestQuadRedactionCornerOrder(t *testing.T) {
	pdf := pdfPage("", "", slantedAt("aaa Hello bbb", 50, 50))
	before := pdfChars(t, pdf)
	var crossed, mirrored []geometry.Quad
	for _, q := range quadsOf(before[4:9]) {
		crossed = append(crossed, geometry.Quad{UL: q.UL, UR: q.UR, LL: q.LR, LR: q.LL})
		mirrored = append(mirrored, geometry.Quad{UL: q.UR, UR: q.UL, LL: q.LR, LR: q.LL})
	}
	got, _ := applySaveReopen(t, pdf, ApplyOptions{}, addQuads(crossed))
	if s := runesOf(got); strings.ContainsAny(s, "Helo") {
		t.Errorf("self-crossing quads left %q", s)
	}
	got, _ = applySaveReopen(t, pdf, ApplyOptions{}, addQuads(mirrored))
	checkSurvivors(t, "mirrored quads", got, append(append([]Char{}, before[:4]...), before[9:]...))
}

var (
	streamRE = regexp.MustCompile(`(?s)stream\r?\n(.*?)endstream`)
	// A literal string (no nested parentheses in these fixtures) or a hex string.
	stringRE = regexp.MustCompile(`\(((?:\\.|[^\\)])*)\)|<([0-9A-Fa-f\s]*)>`)
)

// shownText concatenates every string operand in every stream of an
// uncompressed PDF: what any text-showing operator could still show.
func shownText(t *testing.T, pdf []byte) string {
	t.Helper()
	var b strings.Builder
	for _, s := range streamRE.FindAllSubmatch(pdf, -1) {
		for _, m := range stringRE.FindAllSubmatch(s[1], -1) {
			if m[1] != nil {
				b.Write(m[1])
				continue
			}
			hex := strings.Join(strings.Fields(string(m[2])), "")
			if len(hex)%2 == 1 {
				hex += "0"
			}
			for i := 0; i+1 < len(hex); i += 2 {
				v, err := strconv.ParseUint(hex[i:i+2], 16, 8)
				if err != nil {
					t.Fatal(err)
				}
				b.WriteByte(byte(v))
			}
		}
	}
	return b.String()
}

// The redacted characters are gone from the saved file's content streams,
// not just from extraction.
func TestQuadRedactionContentStream(t *testing.T) {
	pdf := pdfPage("", "", slantedAt("aaa Hello bbb", 50, 50))
	if s := shownText(t, pdf); s != "aaa Hello bbb" {
		t.Fatalf("fixture shows %q", s)
	}
	before := pdfChars(t, pdf)
	_, out := applySaveReopen(t, pdf, ApplyOptions{}, addQuads(quadsOf(before[4:9])))
	if bytes.Contains(out, []byte("/FlateDecode")) {
		t.Fatal("saved file is compressed; the scan would see nothing")
	}
	if s := strings.ReplaceAll(shownText(t, out), " ", ""); s != "aaabbb" {
		t.Errorf("content streams still show %q, want %q", s, "aaabbb")
	}
}

// With the default box, the box follows the quads and nothing is drawn over
// the neighbours.
func TestQuadRedactionBoxSparesNeighbours(t *testing.T) {
	pdf := pdfPage("", "", slantedAt("aaa Hello bbb", 50, 50))
	before := pdfChars(t, pdf)
	centre := func(q geometry.Quad) (int, int) {
		return int((q.UL.X + q.UR.X + q.LL.X + q.LR.X) / 4), int((q.UL.Y + q.UR.Y + q.LL.Y + q.LR.Y) / 4)
	}
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, _ := d.LoadPage(0)
	others := append(append([]Char{}, before[:3]...), before[10:]...)
	var neighbours [][3]byte
	for _, c := range others {
		x, y := centre(c.Quad)
		neighbours = append(neighbours, px(t, p, x, y))
	}
	if err := p.AddRedactionQuads(quadsOf(before[4:9]), RedactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyRedactions(ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
	for i, c := range others {
		x, y := centre(c.Quad)
		if got := px(t, p, x, y); got != neighbours[i] {
			t.Errorf("neighbour %q centre (%d,%d) = %v after apply, was %v", c.Rune, x, y, got, neighbours[i])
		}
	}
	for _, c := range before[4:9] {
		x, y := centre(c.Quad)
		if got := px(t, p, x, y); got != black {
			t.Errorf("redacted %q centre (%d,%d) = %v, want black", c.Rune, x, y, got)
		}
	}
}

// TextNone still reaches MuPDF when the redaction is a quad.
func TestQuadRedactionTextNone(t *testing.T) {
	pdf := pdfPage("", "", slantedAt("aaa Hello bbb", 50, 50))
	before := pdfChars(t, pdf)
	got, _ := applySaveReopen(t, pdf, ApplyOptions{Text: TextNone}, addQuads(quadsOf(before[4:9])))
	if runesOf(got) != "aaaHellobbb" {
		t.Errorf("TextNone left %q", runesOf(got))
	}
}

// The per-glyph pass from several goroutines at once (cgo thread stacks).
func TestQuadRedactionConcurrent(t *testing.T) {
	pdf := pdfPage("", "/Fm0 6 0 R", slantedAt("aaa Hello bbb", 50, 50)+" /Fm0 Do",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
			len(slantedAt("ccc WUW ddd", 150, 30)), slantedAt("ccc WUW ddd", 150, 30)))
	for i := range 4 {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Parallel()
			perGlyphCase(t, pdf, 4, 9, false)
		})
	}
}
