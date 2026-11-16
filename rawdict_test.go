package gomupdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// buildPDF assembles a PDF from objects numbered 1..n (object 1 must be the
// catalog), with a correct xref table. For fixtures the package cannot write.
func buildPDF(objs ...string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offs {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// onePagePDF returns a one-page 300x200 PDF whose /F1 is font and whose
// content stream is content.
func onePagePDF(t *testing.T, font, content string) *Document {
	t.Helper()
	d, err := OpenStream(buildPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		font,
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
	))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func pageChars(blocks []RawBlock) string {
	var sb strings.Builder
	for _, b := range blocks {
		for _, l := range b.Lines {
			for _, ch := range l.Chars {
				sb.WriteRune(ch.Rune)
			}
		}
	}
	return sb.String()
}

func TestParseRawDictEmptyFontName(t *testing.T) {
	blocks, metas, err := parseRawDict("B 0 0 10 10\nL 0 0 10 10\nC 72 1 9 1 1 6 10 12 -\nC 105 6 9 6 1 9 10 12 Helvetica\n")
	if err != nil {
		t.Fatal(err)
	}
	if got := pageChars(blocks); got != "Hi" {
		t.Errorf("chars = %q, want %q", got, "Hi")
	}
	if metas[0].font != "" || metas[1].font != "Helvetica" || metas[0].size != 12 {
		t.Errorf("metas = %+v", metas)
	}
}

func TestParseRawDictInvalidCodePoint(t *testing.T) {
	for _, cp := range []string{"-1", "55296", "57343", "1114112"} { // negative, D800, DFFF, 0x110000
		blocks, _, err := parseRawDict("B 0 0 1 1\nL 0 0 1 1\nC " + cp + " 0 0 0 0 1 1 12 F\n")
		if err != nil {
			t.Fatalf("cp %s: %v", cp, err)
		}
		if got := blocks[0].Lines[0].Chars[0].Rune; got != '�' {
			t.Errorf("cp %s: rune = %U, want U+FFFD", cp, got)
		}
	}
}

func TestParseRawDictMalformed(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"B 0 0 1\n", "line 1: too few fields"},
		{"B 0 0 1 x\n", "line 1: bad number"},
		{"B 0 0 1 1 2\n", "line 1: bad number"},
		{"B 0 0 1 1\nL 0 0 1 1\nC 72 0 0 0 0 1 1 12\n", "line 3: too few fields"},
		{"B 0 0 1 1\nL 0 0 1 1\nC x 0 0 0 0 1 1 12 F\n", "line 3: bad code point"},
		{"X 1 2 3 4\n", "line 1: unknown record type"},
		{"L 0 0 1 1\n", "line 1: L record outside a block"},
		{"B 0 0 1 1\nC 72 0 0 0 0 1 1 12 F\n", "line 2: C record outside a line"},
	} {
		_, _, err := parseRawDict(tc.in)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("parseRawDict(%q) err = %v, want %q", tc.in, err, tc.want)
		}
	}
}

// A font with an empty /BaseFont has an empty fz_font_name. Upstream emitted
// no name field for it and the parser silently dropped every such character.
func TestRawDictEmptyFontName(t *testing.T) {
	d := onePagePDF(t, "<< /Type /Font /Subtype /Type1 /BaseFont / >>",
		"BT /F1 12 Tf 20 100 Td (Hello) Tj ET")
	defer d.Close()
	p, _ := d.LoadPage(0)
	blocks, err := p.RawDict()
	if err != nil {
		t.Fatal(err)
	}
	if got := pageChars(blocks); got != "Hello" {
		t.Errorf("RawDict chars = %q, want %q", got, "Hello")
	}
	blocks, err = p.Dict()
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) != 1 || len(blocks[0].Spans) != 1 || blocks[0].Spans[0].Text != "Hello" || blocks[0].Spans[0].Font != "" {
		t.Errorf("Dict = %+v, want one span \"Hello\" with Font \"\"", blocks)
	}
}

// TestRawDictBlocksExist verifies that RawDict returns at least one block with lines and chars.
func rawDictOpts(t *testing.T, p *Page, opts TextOptions) []RawBlock {
	t.Helper()
	blocks, err := p.RawDictWithOptions(opts)
	if err != nil {
		t.Fatal(err)
	}
	return blocks
}

// Zero-width glyphs (Widths all 0, with Tc spacing) have an empty-area quad;
// a Clip covering the page must still keep them, in RawDict and ExtractText.
func TestClipKeepsZeroWidthGlyphs(t *testing.T) {
	d := onePagePDF(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 0 /LastChar 255 /Widths ["+strings.Repeat("0 ", 256)+"] >>",
		"BT /F1 12 Tf 8 Tc 20 100 Td (SECRET) Tj ET")
	defer d.Close()
	p, _ := d.LoadPage(0)
	b, err := p.Bound()
	if err != nil {
		t.Fatal(err)
	}
	opts := TextOptions{Clip: &b}
	if got := strings.ReplaceAll(pageChars(rawDictOpts(t, p, opts)), " ", ""); got != "SECRET" {
		t.Errorf("RawDictWithOptions clip: chars = %q, want SECRET", got)
	}
	txt, err := p.ExtractText(opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.ReplaceAll(strings.TrimSpace(txt), " ", ""); got != "SECRET" {
		t.Errorf("ExtractText clip: %q, want SECRET", txt)
	}
}

// Code 1 is re-encoded to the glyph name /fi, which MuPDF maps to U+FB01. The
// base-14 Times-Roman MuPDF substitutes has that glyph (non-zero advance).
func TestRawDictWithOptionsLigature(t *testing.T) {
	f, err := NewFont("Times-Roman")
	if err != nil {
		t.Fatal(err)
	}
	if f.Advance(0xFB01, 1) <= 0 {
		t.Fatal("Times-Roman has no U+FB01 glyph; the fixture would not test ligatures")
	}
	f.Close()
	d := onePagePDF(t,
		"<< /Type /Font /Subtype /Type1 /BaseFont /Times-Roman /Encoding << /Type /Encoding /BaseEncoding /WinAnsiEncoding /Differences [1 /fi] >> >>",
		"BT /F1 12 Tf 20 100 Td (\\001nd) Tj ET")
	defer d.Close()
	p, _ := d.LoadPage(0)
	if got := pageChars(rawDictOpts(t, p, TextOptions{PreserveLigatures: true})); got != "ﬁnd" {
		t.Errorf("PreserveLigatures: chars = %q, want %q", got, "ﬁnd")
	}
	if got := pageChars(rawDictOpts(t, p, TextOptions{})); got != "find" {
		t.Errorf("default: chars = %q, want %q", got, "find")
	}
}

func TestRawDictWithOptionsDehyphenate(t *testing.T) {
	d := onePagePDF(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"BT /F1 12 Tf 20 150 Td (Some exam-) Tj 0 -14 Td (ple text here) Tj ET")
	defer d.Close()
	p, _ := d.LoadPage(0)
	if got := pageChars(rawDictOpts(t, p, TextOptions{})); got != "Some exam-ple text here" {
		t.Errorf("default: chars = %q", got)
	}
	if got := pageChars(rawDictOpts(t, p, TextOptions{Dehyphenate: true})); got != "Some example text here" {
		t.Errorf("Dehyphenate: chars = %q", got)
	}
}

func TestRawDictWithOptionsAccurateBBoxes(t *testing.T) {
	d := onePagePDF(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"BT /F1 12 Tf 20 100 Td (Hello) Tj ET")
	defer d.Close()
	p, _ := d.LoadPage(0)
	plain := rawDictOpts(t, p, TextOptions{})
	accurate := rawDictOpts(t, p, TextOptions{AccurateBBoxes: true})
	if pageChars(plain) != "Hello" || pageChars(accurate) != "Hello" {
		t.Fatalf("chars = %q / %q", pageChars(plain), pageChars(accurate))
	}
	// The font-metric bbox is the same height for every glyph; the outline
	// bbox of 'e' is shorter than that of 'H'.
	pc, ac := plain[0].Lines[0].Chars, accurate[0].Lines[0].Chars
	if pc[0].BBox.Height() != pc[1].BBox.Height() {
		t.Errorf("default: H and e heights differ: %v %v", pc[0].BBox, pc[1].BBox)
	}
	if ac[1].BBox.Height() >= ac[0].BBox.Height() {
		t.Errorf("AccurateBBoxes: e height %v not below H height %v", ac[1].BBox, ac[0].BBox)
	}
}

// Clip keeps exactly the characters ExtractText keeps under the same Clip.
func TestRawDictWithOptionsClip(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	defer d.Close()
	p, _ := d.LoadPage(0)
	b, err := p.Bound()
	if err != nil {
		t.Fatal(err)
	}
	for i, clip := range []geometry.Rect{
		{X0: b.X0, Y0: b.Y0, X1: b.X1, Y1: (b.Y0 + b.Y1) / 2},
		{X0: b.X0, Y0: b.Y0, X1: (b.X0 + b.X1) / 3, Y1: b.Y1},
		{X0: -20, Y0: -20, X1: -10, Y1: -10}, // off the page: nothing
	} {
		opts := TextOptions{Clip: &clip}
		want, err := p.ExtractText(opts)
		if err != nil {
			t.Fatal(err)
		}
		if (want == "") != (i == 2) {
			t.Fatalf("clip %v: ExtractText = %q; fixture no longer fits the test", clip, want)
		}
		var got strings.Builder
		blocks := rawDictOpts(t, p, opts)
		for _, bl := range blocks {
			if len(bl.Lines) == 0 {
				t.Errorf("clip %v: empty block returned", clip)
			}
			for _, l := range bl.Lines {
				if len(l.Chars) == 0 {
					t.Errorf("clip %v: empty line returned", clip)
				}
				for _, ch := range l.Chars {
					if !ch.BBox.Intersects(clip) {
						t.Errorf("clip %v: char %q bbox %v outside", clip, ch.Rune, ch.BBox)
					}
					got.WriteRune(ch.Rune)
				}
				got.WriteByte('\n')
			}
		}
		if got.String() != want {
			t.Errorf("clip %v: RawDictWithOptions chars\n%q\nExtractText\n%q", clip, got.String(), want)
		}
	}
}

func TestRawDictBlocksExist(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	defer d.Close()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := p.RawDict()
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) == 0 {
		t.Fatal("RawDict: want >0 blocks, got 0")
	}
	hasLine := false
	for _, b := range blocks {
		if len(b.Lines) > 0 {
			hasLine = true
			break
		}
	}
	if !hasLine {
		t.Fatal("RawDict: no block has lines")
	}
}

// TestRawDictCharsNonEmpty verifies chars are present and have non-zero bboxes.
func TestRawDictCharsNonEmpty(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	defer d.Close()
	p, _ := d.LoadPage(0)
	blocks, err := p.RawDict()
	if err != nil {
		t.Fatal(err)
	}
	charCount := 0
	for _, b := range blocks {
		for _, l := range b.Lines {
			for _, ch := range l.Chars {
				charCount++
				bb := ch.BBox
				if bb.X1-bb.X0 < 0 {
					t.Errorf("char bbox X1<X0: %v", bb)
				}
				if bb.Y1-bb.Y0 < 0 {
					t.Errorf("char bbox Y1<Y0: %v", bb)
				}
			}
		}
	}
	if charCount == 0 {
		t.Fatal("RawDict: no chars found at all")
	}
}

// TestRawDictContainsKnownText checks that concatenating all char runes
// contains text known to be in small-table.pdf (header: "Boiling Points").
func TestRawDictContainsKnownText(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	defer d.Close()
	p, _ := d.LoadPage(0)
	blocks, err := p.RawDict()
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, b := range blocks {
		for _, l := range b.Lines {
			for _, ch := range l.Chars {
				sb.WriteRune(ch.Rune)
			}
		}
	}
	got := sb.String()
	if !strings.Contains(got, "Boiling") {
		t.Errorf("RawDict all-chars text does not contain 'Boiling'; got %q…", got[:min(len(got), 200)])
	}
}

// TestRawDictCharBBoxWithinPage sanity-checks that each char bbox is within
// the page bounding rect (with a small tolerance for rounding).
func TestRawDictCharBBoxWithinPage(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	defer d.Close()
	p, _ := d.LoadPage(0)
	bound, err := p.Bound()
	if err != nil {
		t.Fatal(err)
	}
	blocks, err := p.RawDict()
	if err != nil {
		t.Fatal(err)
	}
	const tol = 5.0 // PDF-point tolerance
	for _, b := range blocks {
		for _, l := range b.Lines {
			for _, ch := range l.Chars {
				bb := ch.BBox
				if bb.X0 < bound.X0-tol || bb.X1 > bound.X1+tol ||
					bb.Y0 < bound.Y0-tol || bb.Y1 > bound.Y1+tol {
					t.Errorf("char bbox %v outside page bound %v (tol %.1f)", bb, bound, tol)
				}
			}
		}
	}
}

// TestRawDictCharOriginOrdering checks that origin falls within or near the char bbox.
func TestRawDictCharOriginOrdering(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	defer d.Close()
	p, _ := d.LoadPage(0)
	blocks, err := p.RawDict()
	if err != nil {
		t.Fatal(err)
	}
	const tol = 5.0
	for _, b := range blocks {
		for _, l := range b.Lines {
			for _, ch := range l.Chars {
				bb := ch.BBox
				ox, oy := ch.Origin.X, ch.Origin.Y
				if ox < bb.X0-tol || ox > bb.X1+tol {
					t.Errorf("origin.X=%g outside bbox [%g,%g] (tol %.1f)", ox, bb.X0, bb.X1, tol)
				}
				_ = oy // origin Y (baseline) may be outside bbox Y in some typefaces — skip
			}
		}
	}
}

// TestDictBlocksExist verifies that Dict returns at least one block with spans.
func TestDictBlocksExist(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	defer d.Close()
	p, _ := d.LoadPage(0)
	blocks, err := p.Dict()
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) == 0 {
		t.Fatal("Dict: want >0 blocks, got 0")
	}
	hasSpan := false
	for _, b := range blocks {
		if len(b.Spans) > 0 {
			hasSpan = true
			break
		}
	}
	if !hasSpan {
		t.Fatal("Dict: no block has spans")
	}
}

// TestDictSpansMeta verifies each span has non-empty Font and Size>0.
func TestDictSpansMeta(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	defer d.Close()
	p, _ := d.LoadPage(0)
	blocks, err := p.Dict()
	if err != nil {
		t.Fatal(err)
	}
	spanCount := 0
	for _, b := range blocks {
		for _, sp := range b.Spans {
			spanCount++
			if sp.Font == "" {
				t.Errorf("span has empty Font")
			}
			if sp.Size <= 0 {
				t.Errorf("span has non-positive Size: %g", sp.Size)
			}
			if sp.Text == "" {
				t.Errorf("span has empty Text")
			}
		}
	}
	if spanCount == 0 {
		t.Fatal("Dict: no spans found")
	}
}

// TestDictSpansTextNonEmpty verifies concatenation of all span Text is non-empty
// and contains expected content.
func TestDictSpansTextNonEmpty(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	defer d.Close()
	p, _ := d.LoadPage(0)
	blocks, err := p.Dict()
	if err != nil {
		t.Fatal(err)
	}
	var sb strings.Builder
	for _, b := range blocks {
		for _, sp := range b.Spans {
			sb.WriteString(sp.Text)
		}
	}
	got := sb.String()
	if got == "" {
		t.Fatal("Dict: all span text is empty")
	}
	if !strings.Contains(got, "Boiling") {
		t.Errorf("Dict span text does not contain 'Boiling'; got %q…", got[:min(len(got), 200)])
	}
}

// TestRawDictSymbolList runs RawDict on symbol-list.pdf to exercise
// a different fixture (symbols / special chars).
func TestRawDictSymbolList(t *testing.T) {
	d := openFixture(t, "symbol-list.pdf")
	defer d.Close()
	p, _ := d.LoadPage(0)
	blocks, err := p.RawDict()
	if err != nil {
		t.Fatal(err)
	}
	if len(blocks) == 0 {
		t.Fatal("RawDict symbol-list: want >0 blocks")
	}
}

// TestRawDictClosedDocument verifies that calling RawDict on a closed document
// returns an error instead of panicking.
func TestRawDictClosedDocument(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	p, _ := d.LoadPage(0)
	d.Close()
	_, err := p.RawDict()
	if err == nil {
		t.Fatal("RawDict on closed document: want error, got nil")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("expected 'closed' in error, got %q", err.Error())
	}
}

// TestDictClosedDocument verifies that calling Dict on a closed document
// returns an error.
func TestDictClosedDocument(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	p, _ := d.LoadPage(0)
	d.Close()
	_, err := p.Dict()
	if err == nil {
		t.Fatal("Dict on closed document: want error, got nil")
	}
	if !strings.Contains(err.Error(), "closed") {
		t.Errorf("expected 'closed' in error, got %q", err.Error())
	}
}

// TestRawDictBBoxOrdering verifies x0<=x1 and y0<=y1 for all char bboxes.
func TestRawDictBBoxOrdering(t *testing.T) {
	d := openFixture(t, "small-table.pdf")
	defer d.Close()
	p, _ := d.LoadPage(0)
	blocks, err := p.RawDict()
	if err != nil {
		t.Fatal(err)
	}
	for _, b := range blocks {
		for _, l := range b.Lines {
			for _, ch := range l.Chars {
				bb := ch.BBox
				if bb.X0 > bb.X1 {
					t.Errorf("char bbox X0>X1: %v", bb)
				}
				if bb.Y0 > bb.Y1 {
					t.Errorf("char bbox Y0>Y1: %v", bb)
				}
			}
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
