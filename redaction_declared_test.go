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
			elems:      []string{"<< /Type /StructElem /S /P /P 9 0 R /Pg 3 0 R /K << /Type /MCR /MCID 0 /Stm 8 0 R /Pg 3 0 R >> /ActualText (aaa Hello bbb) >>"},
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

// formStructDoc draws form 8 (with /StructParents 1, "aaa Hello bbb" as
// MCID 0) on page 1 by page1, and on page 2 by page2. Element 11 owns that
// content for page 1 through an MCR naming the form's stream; element 12
// does the same for page 2.
func formStructDoc(page1, page2 string) declaredDoc {
	c := declaredDoc{
		page1:      page1,
		page2:      page2,
		form:       "/P << /MCID 0 >> BDC " + line("", "") + " EMC",
		formDict:   "/StructParents 1",
		structTree: "<< /Type /StructTreeRoot /K [11 0 R] /ParentTree << /Nums [0 [] 1 [11 0 R]] >> >>",
		elems: []string{
			"<< /Type /StructElem /S /P /P 9 0 R /Pg 3 0 R /K << /Type /MCR /MCID 0 /Stm 8 0 R /Pg 3 0 R >> >>",
		},
	}
	if page2 != "" {
		c.shared = true
		c.structTree = strings.Replace(c.structTree, "/K [11 0 R]", "/K [11 0 R 12 0 R]", 1)
		c.elems = append(c.elems, "<< /Type /StructElem /S /P /P 9 0 R /Pg 4 0 R /K << /Type /MCR /MCID 0 /Stm 8 0 R /Pg 4 0 R >> >>")
	}
	return c
}

// redactAllHello redacts every "Hello" page 1 shows and saves uncompressed
// with Garbage 1.
func redactAllHello(t *testing.T, pdf []byte) (*Document, []byte) {
	t.Helper()
	d := applyAllHello(t, pdf)
	defer d.Close()
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

// applyAllHello opens pdf and redacts every "Hello" page 1 shows; the
// caller closes the document.
func applyAllHello(t *testing.T, pdf []byte) *Document {
	t.Helper()
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	p, _ := d.LoadPage(0)
	gs := pageGlyphs(t, p)
	s, n := runesOf(gs), 0
	for i := strings.Index(s, "Hello"); i >= 0; i = strings.Index(s, "Hello") {
		if err := p.AddRedactionQuads(quadsOf(gs[i:i+5]), RedactOptions{}); err != nil {
			t.Fatal(err)
		}
		gs, s, n = gs[i+5:], s[i+5:], n+1
	}
	if got, err := p.ApplyRedactions(ApplyOptions{}); err != nil || got != n || n == 0 {
		d.Close()
		t.Fatalf("ApplyRedactions = %d, %v; want %d", got, err, n)
	}
	return d
}

// savesClean saves d uncompressed at Garbage 1 and 3 and fails if either
// save contains one of words. (At Garbage 0 the page's original streams stay
// as unreferenced objects, as in MuPDF.)
func savesClean(t *testing.T, label string, d *Document, words ...string) {
	t.Helper()
	for _, g := range []int{1, 3} {
		out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: g})
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range words {
			if i := bytes.Index(out, []byte(w)); i >= 0 {
				t.Errorf("%s, Garbage %d: saved file has %q: %q", label, g, w, out[max(0, i-80):min(len(out), i+40)])
			}
		}
	}
}

// A structure element whose MCR names a form's stream (/Stm) must not keep
// the original, unredacted form alive once the page draws a redacted copy:
// /Stm follows the copy when the page drew the form once, and is dropped
// when it drew it more than once. Another page that draws the original keeps
// it and its own reference to it.
func TestRedactionFormStructRefs(t *testing.T) {
	for _, c := range []struct {
		name, page1 string
		stm         bool // page 1's MCR keeps a /Stm
	}{
		{"once", "q /Fm0 Do Q", true},
		{"twice", "q /Fm0 Do Q q 1 0 0 1 0 -50 cm /Fm0 Do Q", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, out := redactAllHello(t, formStructDoc(c.page1, "").pdf())
			if i := bytes.Index(out, []byte("Hello")); i >= 0 {
				t.Errorf("saved file still has Hello: %q", out[max(0, i-80):min(len(out), i+40)])
			}
			for _, actual := range []bool{false, true} {
				if s := extract(t, d, 0, actual); strings.Contains(s, "Hello") || !strings.Contains(s, "bbb") {
					t.Errorf("UseActualText %v: extracted %q", actual, s)
				}
			}
			if got := bytes.Contains(out, []byte("/Stm ")); got != c.stm {
				t.Errorf("an MCR /Stm is present: %v, want %v", got, c.stm)
			}
			if bytes.Contains(out, []byte("/Stm 8 0 R")) {
				t.Error("page 1's MCR still names the original form")
			}
			if bytes.Contains(out, []byte("GomupdfRedactSrc")) {
				t.Error("the private form tag reached the saved file")
			}
		})
	}

	t.Run("other page", func(t *testing.T) {
		d, out := redactAllHello(t, formStructDoc("q /Fm0 Do Q", "q /Fm0 Do Q").pdf())
		if s := extract(t, d, 0, false); strings.Contains(s, "Hello") {
			t.Errorf("page 1 extracted %q", s)
		}
		if s := extract(t, d, 1, false); !strings.Contains(s, "Hello") {
			t.Errorf("page 2 lost the word: %q", s)
		}
		// Page 2's MCR still names form 8, which is still in the file.
		if n := bytes.Count(out, []byte("/Stm 8 0 R")); n != 1 || !bytes.Contains(out, []byte("\n8 0 obj")) {
			t.Errorf("%d MCRs name form 8 (want 1, page 2's); form 8 present: %v", n, bytes.Contains(out, []byte("\n8 0 obj")))
		}
	})
}

// formObjDoc is one page drawing form 8 once. The form is a structure
// object itself: its singular /StructParent (key 2) leads to element 12, a
// Figure whose /K holds an OBJR naming the form. With mcid the form also has
// /StructParents (key 1) for its marked content, owned by element 11 through
// an MCR naming its stream. inTree puts the Figure under the root; pg adds
// /Pg (page 1) to the elements and references.
func formObjDoc(mcid, inTree, pg bool) declaredDoc {
	page := ""
	if pg {
		page = "/Pg 3 0 R"
	}
	formDict := "/StructParent 2"
	figKids := "<< /Type /OBJR /Obj 8 0 R " + page + " >>"
	nums := "0 [] 2 12 0 R"
	if mcid {
		formDict += " /StructParents 1"
		figKids = "11 0 R " + figKids
		nums = "0 [] 1 [11 0 R] 2 12 0 R"
	}
	rootKids := "[]"
	if inTree {
		rootKids = "[12 0 R]"
	}
	return declaredDoc{
		page1:      "q /Fm0 Do Q",
		form:       "/P << /MCID 0 >> BDC " + line("", "") + " EMC",
		formDict:   formDict,
		structTree: "<< /Type /StructTreeRoot /K " + rootKids + " /ParentTree << /Nums [" + nums + "] >> >>",
		elems: []string{
			"<< /Type /StructElem /S /P /P 12 0 R " + page + " /K << /Type /MCR /MCID 0 /Stm 8 0 R " + page + " >> >>",
			"<< /Type /StructElem /S /Figure /P 9 0 R " + page + " /K [" + figKids + "] >>",
		},
	}
}

// Object references (OBJR) to a form that is a structure object itself, and
// MCRs to a form with no /StructParents, are followed too: the saved file
// keeps no unredacted copy of the form.
func TestRedactionFormObjectRefs(t *testing.T) {
	for name, c := range map[string]declaredDoc{
		"structparent and structparents": formObjDoc(true, true, true),
		"structparent only":              formObjDoc(false, true, true),
		"no pg":                          formObjDoc(true, true, false),
		"not under the root":             formObjDoc(true, false, true),
		"mcr, form without keys": {
			page1:      "q /Fm0 Do Q",
			form:       "/P << /MCID 0 >> BDC " + line("", "") + " EMC",
			structTree: "<< /Type /StructTreeRoot /K [11 0 R] /ParentTree << /Nums [0 []] >> >>",
			elems:      []string{"<< /Type /StructElem /S /P /P 9 0 R /Pg 3 0 R /K << /Type /MCR /MCID 0 /Stm 8 0 R >> >>"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := applyAllHello(t, c.pdf())
			defer d.Close()
			savesClean(t, name, d, "Hello")
		})
	}
}
