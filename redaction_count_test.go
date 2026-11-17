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

// Ligatures, kerning and zero-advance marks keep the count: redacting a word
// next to them leaves no excess, and only the word goes.
func TestApplyRedactionsCountLigaturesKerning(t *testing.T) {
	for _, c := range []struct {
		name, font, show string
		from, to         int // glyphs (spaces dropped) redacted by their quads
		want             string
		// Known false refusal of the near-hit rule (not the count): a kept
		// ligature's trailing component lies on or in the redacted glyph's
		// quad, so it was "under a redaction" before and still is.
		remains bool
	}{
		// The ligature's trailing i sits at the redacted S's origin.
		{"ligature before", stdHelvetica, `(of\256SECRET) Tj`, 4, 10, "offi", true},
		{"ligature before space", stdHelvetica, `(of\256 SECRET) Tj`, 4, 10, "offi", false},
		// SECRET's T ends where the fl ligature starts.
		{"ligature after", stdHelvetica, `(SECRET\257ow) Tj`, 0, 6, "flow", false},
		{"kerning", stdHelvetica, `[(W) 80 (a) -40 (\256) 30 (SECRET) -60 (AV) 90 (o\257)] TJ`, 4, 10, "WafiAVofl", true},
		{"kerning apart", stdHelvetica, `[(W) 80 (a) -40 (\256) -30 (SECRET) -60 (AV) 90 (o\257)] TJ`, 4, 10, "WafiAVofl", false},
		// The zero-width acute sits at the redacted x's origin, on the edge
		// of its box, and goes with it.
		{"mark before", zeroAcuteHelvetica(), `(cafe\302xyz) Tj`, 5, 8, "cafe", false},
		{"mark after", zeroAcuteHelvetica(), `(xyze\302cafe) Tj`, 0, 3, "e´cafe", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			pdf := pdfPageFont(c.font, "/MediaBox [0 0 300 200]", "BT /F1 12 Tf 20 100 Td "+c.show+" ET")
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
			_, err = p.ApplyRedactions(ApplyOptions{})
			var ce *RedactionCheckError
			if c.remains {
				if !errors.As(err, &ce) || ce.Excess != 0 || ce.Moved != 0 || ce.Remaining != 1 {
					t.Errorf("ApplyRedactions: %v; want 1 remaining, no excess", err)
				}
			} else if err != nil {
				t.Errorf("ApplyRedactions: %v", err)
			}
			if s := runesOf(pageGlyphs(t, p)); s != c.want {
				t.Errorf("chars %q, quads on %d..%d: left %q, want %q", runesOf(gs), c.from, c.to, s, c.want)
			}
		})
	}
}
