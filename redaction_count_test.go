package gomupdf

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// orphanFormPDF draws text from a form XObject without /Resources that uses
// the page's /F1 (font, object 4). MuPDF's filtered copy of the form cannot
// find /F1, so it measures (and, saved, draws) the glyphs with a fallback
// font, and the per-glyph removal misses the redacted ones. extra objects
// start at 7.
func orphanFormPDF(font, text string, extra ...string) []byte {
	stream := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	return buildPDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 400 200] /Resources << /Font << /F1 4 0 R >> " +
			"/XObject << /Fm1 6 0 R >> >> /Contents 5 0 R >>",
		font,
		stream("", "/Fm1 Do"),
		stream("/Type /XObject /Subtype /Form /BBox [0 0 400 200]", "BT /F1 24 Tf 20 100 Td ("+text+") Tj ET"),
	}, extra...)...)
}

// widths100 is Helvetica whose codes first..last are all 100 units wide.
func widths100(first, last int, more string) string {
	return fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar %d /LastChar %d /Widths [%s] %s >>",
		first, last, strings.Repeat("100 ", last-first+1), more)
}

// Redacted text the filter mis-measures survives re-encoded or moved past
// the near-hit band; the page then has more characters than the redaction
// kept, and ApplyRedactions fails.
func TestApplyRedactionsCountsSurvivors(t *testing.T) {
	const secretCMap = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CMapName /Sec def " +
		"1 begincodespacerange <00> <FF> endcodespacerange 6 beginbfchar " +
		"<61> <0053> <62> <0045> <63> <0043> <64> <0052> <65> <0045> <66> <0054> endbfchar " +
		"endcmap CMapName currentdict /CMap defineresource pop end end"
	for _, c := range []struct {
		name string
		pdf  []byte
		leak string // what the page shows afterwards: the survivors
	}{
		// a..f extract as SECRET through the font's /ToUnicode; the
		// filtered form loses the font, so survivors extract as code bytes.
		{"rune change", orphanFormPDF(widths100(97, 102, "/ToUnicode 7 0 R"), "abcdef",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(secretCMap), secretCMap)), "cdef"},
		// The kept prefix, measured wider, pushes SECRET beyond two heights.
		{"far shift", orphanFormPDF(widths100(32, 126, ""), "xxxxxxxxxxxxxxxxxxxx SECRET"), "SECRET"},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, err := OpenStream(c.pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			p, err := d.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			cs := pageGlyphs(t, p)
			s := runesOf(cs)
			i := strings.Index(s, "SECRET")
			if i < 0 {
				t.Fatalf("extracted %q, want SECRET in it", s)
			}
			if err := p.AddRedactionQuads(quadsOf(cs[i:i+6]), RedactOptions{}); err != nil {
				t.Fatal(err)
			}
			_, err = p.ApplyRedactions(ApplyOptions{})
			after := runesOf(pageGlyphs(t, p))
			if !strings.Contains(after, c.leak) {
				t.Fatalf("after redaction %q, want %q in it (fixture no longer leaks)", after, c.leak)
			}
			var ce *RedactionCheckError
			if !errors.Is(err, ErrTextMoved) || !errors.As(err, &ce) || ce.Excess == 0 ||
				!strings.Contains(err.Error(), "more than the redactions kept") {
				t.Fatalf("ApplyRedactions: %v (%+v); want ErrTextMoved with an excess (text now %q)", err, ce, after)
			}
			if _, err := d.SaveBytesWithOptions(SaveOptions{}); !errors.Is(err, ErrRedactionFailed) {
				t.Errorf("save after the failed check: %v, want ErrRedactionFailed", err)
			}
		})
	}
}

// Pages whose extraction could count differently once glyphs are gone keep
// the count: overprinted (fake bold) text, a form drawn twice and redacted
// in one place, and a redaction over empty space.
func TestApplyRedactionsCountUnchanged(t *testing.T) {
	form := "BT /F1 12 Tf 0 0 Td (keep SECRET more) Tj ET"
	formObj := fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
		len(form), form)
	for _, c := range []struct {
		name, xobjects, content string
		extra                   []string
		redact                  string // the glyphs redacted by their quads (first occurrence); "" for an empty area
		want                    string
	}{
		// Both copies are extracted (MuPDF 1.26 merges neither these nor
		// exact overprints) and both go.
		{"fake bold", "", "BT /F1 12 Tf 20 100 Td (keep SECRET more) Tj ET BT /F1 12 Tf 20.25 100 Td (keep SECRET more) Tj ET",
			nil, "SECRET", "keepmorekeepmore"},
		{"form twice", "/Fm0 6 0 R", "q 1 0 0 1 20 100 cm /Fm0 Do Q q 1 0 0 1 20 50 cm /Fm0 Do Q",
			[]string{formObj}, "SECRET", "keepmorekeepSECRETmore"},
		{"empty area", "", "BT /F1 12 Tf 20 100 Td (keep SECRET more) Tj ET", nil, "", "keepSECRETmore"},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, err := OpenStream(pdfPage("", c.xobjects, c.content, c.extra...))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			p, err := d.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			gs := pageGlyphs(t, p)
			if c.redact == "" {
				err = p.AddRedaction(geometry.Rect{X0: 100, Y0: 10, X1: 200, Y1: 40}, RedactOptions{})
			} else {
				i := strings.Index(runesOf(gs), c.redact)
				err = p.AddRedactionQuads(quadsOf(gs[i:i+len(c.redact)]), RedactOptions{})
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.ApplyRedactions(ApplyOptions{}); err != nil {
				t.Errorf("ApplyRedactions: %v", err)
			}
			if s := runesOf(pageGlyphs(t, p)); s != c.want {
				t.Errorf("chars %q: left %q, want %q", runesOf(gs), s, c.want)
			}
		})
	}
}

// A redacted ligature that survives (in a tiling pattern, which removal
// cannot reach) is still reported, though its trailing component is now
// judged with it.
func TestApplyRedactionsSurvivingLigature(t *testing.T) {
	stream := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	pdf := buildPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Pattern << /P1 6 0 R >> >> /Contents 5 0 R >>",
		stdHelvetica,
		stream("", "/Pattern cs /P1 scn 0 0 300 200 re f"),
		stream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 300 200] /XStep 300 /YStep 200 "+
			"/Resources << /Font << /F1 4 0 R >> >>", `BT /F1 24 Tf 20 100 Td (o\256x) Tj ET`),
	)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	gs := pageGlyphs(t, p)
	if s := runesOf(gs); s != "ofix" {
		t.Fatalf("glyphs %q", s)
	}
	if err := p.AddRedactionQuads(quadsOf(gs[1:3]), RedactOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = p.ApplyRedactions(ApplyOptions{})
	var ce *RedactionCheckError
	if !errors.Is(err, ErrTextRemains) || !errors.As(err, &ce) || ce.Remaining < 1 {
		t.Errorf("ApplyRedactions: %v; want the ligature remaining", err)
	}
}

// Ligatures, kerning and zero-advance marks keep the count, and a kept
// ligature's trailing components (at the next glyph's origin, on the edge of
// or inside a redaction that starts there) are not redacted text: redacting
// next to them is no error, and only the redacted glyphs go.
func TestApplyRedactionsCountLigaturesKerning(t *testing.T) {
	const ffiCM = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap /CMapName /Lig def 1 begincodespacerange <00> <FF> endcodespacerange 1 beginbfchar <AE> <006600660069> endbfchar endcmap CMapName currentdict /CMap defineresource pop end end"
	ffi := []string{fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(ffiCM), ffiCM)}
	for _, c := range []struct {
		name, font, show string
		extra            []string
		from, to         int // glyphs (spaces dropped) redacted by their quads
		want             string
	}{
		// The ligature's trailing i sits at the redacted S's origin.
		{"ligature before", stdHelvetica, `(of\256SECRET) Tj`, nil, 4, 10, "offi"},
		// A kern puts the trailing i inside the redacted S.
		{"ligature before, kerned", stdHelvetica, `[(of\256) 30 (SECRET)] TJ`, nil, 4, 10, "offi"},
		{"ligature before, rotated", stdHelvetica, `0.5 0.866 -0.866 0.5 20 100 Tm (of\256SECRET) Tj`, nil, 4, 10, "offi"},
		{"ligature before, char spacing", stdHelvetica, `1 Tc (of\256SECRET) Tj`, nil, 4, 10, "offi"},
		// Two trailing components (a ToUnicode "ffi") before redacted digits.
		{"ffi before digits", flToUnicode, `(ref\2561234) Tj`, ffi, 6, 10, "refffi"},
		{"ligature before space", stdHelvetica, `(of\256 SECRET) Tj`, nil, 4, 10, "offi"},
		// The redaction ends on the ligature; x starts at its trailing i's
		// origin and stays.
		{"ligature redacted, next kept", stdHelvetica, `(SECRE\256x) Tj`, nil, 0, 7, "x"},
		{"ligature inside", stdHelvetica, `(xSE\256ETy) Tj`, nil, 1, 7, "xy"},
		// SECRET's T ends where the fl ligature starts.
		{"ligature after", stdHelvetica, `(SECRET\257ow) Tj`, nil, 0, 6, "flow"},
		{"kerning", stdHelvetica, `[(W) 80 (a) -40 (\256) 30 (SECRET) -60 (AV) 90 (o\257)] TJ`, nil, 4, 10, "WafiAVofl"},
		{"kerning apart", stdHelvetica, `[(W) 80 (a) -40 (\256) -30 (SECRET) -60 (AV) 90 (o\257)] TJ`, nil, 4, 10, "WafiAVofl"},
		// The zero-width acute sits at the redacted x's origin, on the edge
		// of its box, and goes with it.
		{"mark before", zeroAcuteHelvetica(), `(cafe\302xyz) Tj`, nil, 5, 8, "cafe"},
		{"mark after", zeroAcuteHelvetica(), `(xyze\302cafe) Tj`, nil, 0, 3, "e´cafe"},
	} {
		t.Run(c.name, func(t *testing.T) {
			pdf := pdfPageFont(c.font, "/MediaBox [0 0 300 200]", "BT /F1 12 Tf 20 100 Td "+c.show+" ET", c.extra...)
			d, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			p, err := d.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			gs := pageGlyphs(t, p)
			if err := p.AddRedactionQuads(quadsOf(gs[c.from:c.to]), RedactOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := p.ApplyRedactions(ApplyOptions{}); err != nil {
				t.Errorf("chars %q: ApplyRedactions: %v", runesOf(gs), err)
			}
			if s := runesOf(pageGlyphs(t, p)); s != c.want {
				t.Errorf("chars %q, quads on %d..%d: left %q, want %q", runesOf(gs), c.from, c.to, s, c.want)
			}
		})
	}
}
