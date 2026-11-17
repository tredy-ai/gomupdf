package gomupdf

import (
	"fmt"
	"strings"
	"testing"
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
