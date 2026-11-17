package gomupdf

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// declaredDoc is a two-page document for the declared-text tests. Object 7
// is a named marked-content property (/P1 on page 1, and on page 2 when shared), 8 a form XObject
// (/Fm0 on page 1, and on page 2 when shared), 9 the structure tree root when structTree is set,
// 11 and up extra objects (structure elements).
type declaredDoc struct {
	page1, page2 string // content streams
	prop         string // object 7
	form         string // object 8's content
	formDict     string // extra entries for object 8
	structTree   string // object 9, e.g. "<< /Type /StructTreeRoot ... >>"
	elems        []string
	shared       bool // page 2's resources name /P1 and /Fm0 too
}

func (c declaredDoc) pdf() []byte {
	stream := func(dict, s string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(s), s)
	}
	res := "/Resources << /Font << /F1 5 0 R >> /Properties << /P1 7 0 R >> /XObject << /Fm0 8 0 R >> >>"
	res2 := "/Resources << /Font << /F1 5 0 R >> >>"
	if c.shared {
		res2 = res
	}
	catalog := "<< /Type /Catalog /Pages 2 0 R >>"
	sp := ""
	if c.structTree != "" {
		catalog = "<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 9 0 R /MarkInfo << /Marked true >> >>"
		sp = "/StructParents 0"
	}
	prop, root := c.prop, c.structTree
	if prop == "" {
		prop = "<< >>"
	}
	if root == "" {
		root = "<< >>"
	}
	objs := []string{
		catalog,
		"<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] " + sp + " " + res + " /Contents 6 0 R >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] " + res2 + " /Contents 10 0 R >>",
		helvetica,
		stream("", c.page1),
		prop,
		stream("/Type /XObject /Subtype /Form /BBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> /Properties << /P1 7 0 R >> >> "+c.formDict, c.form),
		root,
		stream("", c.page2),
	}
	return buildPDF(append(objs, c.elems...)...)
}

// redactHello redacts the quads of the first "Hello" page 1 shows, with
// opts, saves uncompressed with Garbage 1 and reopens.
func redactHello(t *testing.T, pdf []byte, opts ApplyOptions) (*Document, []byte) {
	t.Helper()
	gs := glyphsOf(pdfChars(t, pdf))
	i := strings.Index(runesOf(gs), "Hello")
	if i < 0 {
		t.Fatalf("no Hello in %q", runesOf(gs))
	}
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, _ := d.LoadPage(0)
	if err := p.AddRedactionQuads(quadsOf(gs[i:i+5]), RedactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyRedactions(opts); err != nil {
		t.Fatal(err)
	}
	out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
	if err != nil {
		t.Fatal(err)
	}
	re, err := OpenStream(out)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(re.Close)
	return re, out
}

func extract(t *testing.T, d *Document, page int, actual bool) string {
	t.Helper()
	p, err := d.LoadPage(page)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.ExtractText(TextOptions{UseActualText: actual})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// line is "aaa Hello bbb" with begin and end around "Hello".
func line(begin, end string) string {
	return fmt.Sprintf("BT /F1 12 Tf 20 100 Td (aaa ) Tj %s (Hello) Tj %s ( bbb) Tj ET", begin, end)
}

// Declared text (/ActualText, /Alt, /E) carrying the redacted word is gone
// from the saved file and from extraction, wherever the page declares it.
func TestRedactionStripsDeclaredText(t *testing.T) {
	span := "/Span << /ActualText (aaa Hello bbb) >> BDC"
	for name, c := range map[string]declaredDoc{
		"inline":         {page1: span + " " + line("", "") + " EMC"},
		"inline DP":      {page1: "/Span << /ActualText (Hello dp) >> DP " + line("", "")},
		"inline alt e":   {page1: line("/Span << /Alt (Hello alt) /E (Hello exp) >> BDC", "EMC")},
		"named":          {page1: "/Span /P1 BDC " + line("", "") + " EMC", prop: "<< /ActualText (aaa Hello bbb) /Alt (Hello alt) /E (Hello exp) >>"},
		"named indirect": {page1: "/Span /P1 BDC " + line("", "") + " EMC", prop: "<< /ActualText 11 0 R >>", elems: []string{"(aaa Hello bbb)"}},
		"nested":         {page1: span + " " + line("/Span << /ActualText (Hello) /Alt (Hello alt) >> BDC", "EMC") + " EMC"},
		"form":           {page1: "q /Fm0 Do Q", form: span + " " + line("", "") + " EMC"},
		"form named":     {page1: "q /Fm0 Do Q", form: "/Span /P1 BDC " + line("", "") + " EMC", prop: "<< /ActualText (aaa Hello bbb) >>"},
		"struct exact": {
			page1:      "/P << /MCID 0 >> BDC " + line("", "") + " EMC",
			structTree: "<< /Type /StructTreeRoot /K 12 0 R /ParentTree << /Nums [0 [11 0 R]] >> >>",
			elems: []string{
				"<< /Type /StructElem /S /P /P 12 0 R /Pg 3 0 R /K 0 /ActualText (aaa Hello bbb) /E (Hello exp) >>",
				"<< /Type /StructElem /S /Sect /P 9 0 R /K [11 0 R] /Alt (A section about Hello) >>",
			},
		},
		"form struct": {
			page1:      "q /Fm0 Do Q",
			form:       "/P << /MCID 0 >> BDC " + line("", "") + " EMC",
			formDict:   "/StructParents 1",
			structTree: "<< /Type /StructTreeRoot /K 11 0 R /ParentTree << /Nums [0 [] 1 [11 0 R]] >> >>",
			elems:      []string{"<< /Type /StructElem /S /P /P 9 0 R /Pg 3 0 R /K 0 /ActualText (aaa Hello bbb) >>"},
		},
		"struct word": {
			page1:      line("/Span << /MCID 0 >> BDC", "EMC"),
			structTree: "<< /Type /StructTreeRoot /K 11 0 R /ParentTree << /Nums [0 [11 0 R]] >> >>",
			elems:      []string{"<< /Type /StructElem /S /Span /P 9 0 R /Pg 3 0 R /K 0 /ActualText (Hello) /Alt (Hello alt) >>"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			pdf := c.pdf()
			if !bytes.Contains(pdf, []byte("Hello")) {
				t.Fatal("fixture has no Hello")
			}
			d, out := redactHello(t, pdf, ApplyOptions{})
			if i := bytes.Index(out, []byte("Hello")); i >= 0 {
				t.Errorf("saved file still has Hello: %q", out[max(0, i-80):min(len(out), i+40)])
			}
			for _, actual := range []bool{false, true} {
				s := extract(t, d, 0, actual)
				if strings.Contains(s, "Hello") || !strings.Contains(s, "aaa") || !strings.Contains(s, "bbb") {
					t.Errorf("UseActualText %v: extracted %q", actual, s)
				}
			}
		})
	}
}

// Another page drawing the same named property keeps its declared text, and
// its own inline /ActualText and /Alt. TextNone leaves declared text alone.
func TestRedactionDeclaredTextOtherPage(t *testing.T) {
	c := declaredDoc{
		page1:  "/Span /P1 BDC " + line("", "") + " EMC",
		page2:  "/Span /P1 BDC BT /F1 12 Tf 20 100 Td (zzz) Tj ET EMC /Span << /ActualText (page two) /Alt (two alt) >> BDC BT /F1 12 Tf 20 50 Td (yyy) Tj ET EMC",
		prop:   "<< /ActualText (shared words) /Alt (shared alt) >>",
		shared: true,
	}
	d, out := redactHello(t, c.pdf(), ApplyOptions{})
	if s := extract(t, d, 0, true); strings.Contains(s, "shared words") {
		t.Errorf("page 1 still declares the shared text: %q", s)
	}
	if s := extract(t, d, 1, true); !strings.Contains(s, "shared words") || !strings.Contains(s, "page two") {
		t.Errorf("page 2 lost its declared text: %q", s)
	}
	for _, s := range []string{"shared alt", "two alt"} {
		if !bytes.Contains(out, []byte(s)) {
			t.Errorf("page 2's /Alt %q is gone", s)
		}
	}

	c = declaredDoc{page1: "/Span << /ActualText (aaa Hello bbb) /Alt (Hello alt) >> BDC " + line("", "") + " EMC"}
	_, out = redactHello(t, c.pdf(), ApplyOptions{Text: TextNone})
	if !bytes.Contains(out, []byte("(aaa Hello bbb)")) || !bytes.Contains(out, []byte("(Hello alt)")) {
		t.Error("TextNone stripped declared text")
	}
}

// A span whose remaining glyphs match the end of its declared text, as the
// page's first text: MuPDF 1.26.11's extraction crashed on what redaction
// left of it when honouring /ActualText. With the declared text stripped it
// extracts.
func TestRedactionDeclaredTextFirstSpan(t *testing.T) {
	c := declaredDoc{page1: "BT /F1 12 Tf 20 100 Td /Span << /ActualText (Hello bbb) >> BDC (Hello bbb) Tj EMC ET"}
	d, _ := redactHello(t, c.pdf(), ApplyOptions{})
	if s := extract(t, d, 0, true); strings.Contains(s, "Hello") || !strings.Contains(s, "bbb") {
		t.Errorf("extracted %q", s)
	}
}
