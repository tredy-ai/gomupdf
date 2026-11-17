package gomupdf

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// unreachablePDF is a page whose content is content, with resources for the
// cases redaction cannot reach: /P1 a tiling pattern showing "op", /GS1 a
// luminosity soft mask whose group shows "AB", and /Fm1 a form showing
// "WWWWWWi" in Courier with no resources of its own (it uses the page's /F2).
func unreachablePDF(content string) []byte {
	stream := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	return buildPDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R /F2 10 0 R >> "+
			"/Pattern << /P1 6 0 R >> /ExtGState << /GS1 << /SMask 8 0 R >> >> /XObject << /Fm1 9 0 R >> >> /Contents 5 0 R >>",
		helvetica,
		stream("", content),
		stream("/Type /Pattern /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 300 200] /XStep 300 /YStep 200 "+
			"/Resources << /Font << /F1 4 0 R >> >>", "BT /F1 24 Tf 20 100 Td (op) Tj ET"),
		stream("/Type /XObject /Subtype /Form /BBox [0 0 300 200] /Group << /S /Transparency /CS /DeviceGray >> "+
			"/Resources << /Font << /F1 4 0 R >> >>", "1 g BT /F1 24 Tf 20 100 Td (AB) Tj ET"),
		"<< /Type /Mask /S /Luminosity /G 7 0 R >>",
		stream("/Type /XObject /Subtype /Form /BBox [0 0 300 200]", "BT /F2 24 Tf 20 100 Td (WWWWWWi) Tj ET"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>",
	)
}

// Text that redaction cannot remove but extraction still finds makes
// ApplyRedactions fail instead of reporting success.
func TestApplyRedactionsVerifiesPage(t *testing.T) {
	for _, c := range []struct {
		name, content, text string
		last                bool  // redact only the last glyph
		want                error // the sentinel the error matches
	}{
		{"tiling pattern", "/Pattern cs /P1 scn 0 0 300 200 re f", "op", false, ErrTextRemains},
		{"soft mask", "/GS1 gs 0 0 1 rg 0 0 300 200 re f", "AB", false, ErrTextRemains},
		// MuPDF's filtered copy of the form cannot find /F2 and
		// measures the glyphs with a fallback font, so the i moves.
		{"form using the page's font", "/Fm1 Do", "WWWWWWi", true, ErrTextMoved},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, err := OpenStream(unreachablePDF(c.content))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			p, err := d.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			cs := pageGlyphs(t, p)
			if runesOf(cs) != c.text {
				t.Fatalf("extracted %q, want %q", runesOf(cs), c.text)
			}
			if c.last {
				cs = cs[len(cs)-1:]
			}
			var qs []geometry.Quad
			for _, ch := range cs {
				qs = append(qs, ch.Quad)
			}
			if err := p.AddRedactionQuads(qs, RedactOptions{}); err != nil {
				t.Fatal(err)
			}
			n, err := p.ApplyRedactions(ApplyOptions{})
			var ce *RedactionCheckError
			if !errors.Is(err, c.want) || !errors.As(err, &ce) || ce.Page != 0 ||
				!strings.Contains(err.Error(), "must not be treated as redacted") {
				t.Fatalf("ApplyRedactions = %d, %v; want the characters-remain error (text now %q)",
					n, err, runesOf(pageGlyphs(t, p)))
			}
			// The private form marker is gone even so.
			out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(out), "GomupdfRedactSrc") {
				t.Error("saved file keeps the private form marker")
			}
			// TextNone keeps text on purpose: no check.
			d2, err := OpenStream(unreachablePDF(c.content))
			if err != nil {
				t.Fatal(err)
			}
			defer d2.Close()
			p2, err := d2.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			if err := p2.AddRedactionQuads(qs, RedactOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := p2.ApplyRedactions(ApplyOptions{Text: TextNone}); err != nil {
				t.Errorf("TextNone: %v", err)
			}
		})
	}
}

// A space extraction synthesises for a gap has no glyph to remove: a
// redaction over the gap alone is not reported as leaving text.
func TestApplyRedactionsSynthesisedSpace(t *testing.T) {
	pdf := pdfPage("", "", "BT /F1 24 Tf 20 100 Td (aaa) Tj 52 0 Td (bbb) Tj ET")
	var space *Char
	cs := pdfChars(t, pdf)
	for i := range cs {
		if cs[i].Rune == ' ' {
			space = &cs[i]
		}
	}
	if space == nil {
		t.Fatalf("no synthesised space in %q", runesOf(cs))
	}
	b := space.BBox
	mid := geometry.Rect{X0: b.X0 + b.Width()*0.3, Y0: b.Y0, X1: b.X1 - b.Width()*0.3, Y1: b.Y1}
	got, _ := applySaveReopen(t, pdf, ApplyOptions{}, addRect(mid))
	if s := runesOf(got); s != "aaabbb" {
		t.Errorf("left %q, want aaabbb", s)
	}
}
