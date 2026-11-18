package gomupdf

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// Tokens of layersPDF, each drawn once, at its own height (layersRows).
const (
	layerVisible = "LayerVisibleText"
	layerOff     = "LayerHiddenOffQx"
	layerUsage   = "LayerHiddenUsageQx"
	layerPolicy  = "LayerHiddenPolicyQx"
	layerForm    = "LayerHiddenFormQx"
	layerNested  = "LayerHiddenNestedQx"
)

var layerHidden = []string{layerOff, layerUsage, layerPolicy, layerForm, layerNested}

// layersRows maps each hidden token to the pixel rows (y down, 72 dpi) of the
// line it is drawn on.
var layersRows = map[string][2]int{
	layerOff: {38, 52}, layerUsage: {58, 72}, layerPolicy: {78, 92}, layerForm: {98, 112}, layerNested: {118, 132},
}

// layersPDF is a one-page PDF whose page shows layerVisible and hides, with
// its default optional-content configuration, text marked with the group 6
// (in /OFF), with the group 7 (on, but /Usage /View /ViewState /OFF), with
// the membership dictionary 9 (/AllOff of the group 8, which is on), the
// form 10 (its own /OC is the group 6) and the form 11 (drawn inside a
// marked-content span of the group 6).
func layersPDF() []byte {
	line := func(y int, s string) string { return fmt.Sprintf("BT /F1 12 Tf 20 %d Td (%s) Tj ET", y, s) }
	form := func(s string, oc string) string {
		return "/Type /XObject /Subtype /Form /BBox [0 0 300 200] " + oc + " /Resources << /Font << /F1 5 0 R >> >>\nstream\n" +
			line(200-110, s)
	}
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [6 0 R 7 0 R 8 0 R] /D << /Order [6 0 R 7 0 R 8 0 R] /OFF [6 0 R] >> >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >>
   /Properties << /oc1 6 0 R /oc2 7 0 R /oc3 9 0 R >> /XObject << /Fm1 10 0 R /Fm2 11 0 R >> >> >>`,
		/* 4 */ "\nstream\n" + line(180, layerVisible) +
			" /OC /oc1 BDC " + line(200-50, layerOff) + " EMC" +
			" /OC /oc2 BDC " + line(200-70, layerUsage) + " EMC" +
			" /OC /oc3 BDC " + line(200-90, layerPolicy) + " EMC" +
			" /Fm1 Do" +
			" /OC /oc1 BDC q 1 0 0 1 0 -20 cm /Fm2 Do Q EMC",
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 6 */ `<< /Type /OCG /Name (Off layer) >>`,
		/* 7 */ `<< /Type /OCG /Name (Usage layer) /Usage << /View << /ViewState /OFF >> >> >>`,
		/* 8 */ `<< /Type /OCG /Name (On layer) >>`,
		/* 9 */ `<< /Type /OCMD /OCGs [8 0 R] /P /AllOff >>`,
		/* 10 */ form(layerForm, "/OC 6 0 R"),
		/* 11 */ form(layerNested, ""),
	}, "")
}

// layersDark counts the dark pixels in rows [r[0], r[1]) of a gray pixmap.
func layersDark(pm *Pixmap, r [2]int) int {
	n := 0
	for y := r[0]; y < r[1] && y < pm.Height; y++ {
		for x := 15; x < 250 && x < pm.Width; x++ {
			if pm.Samples[y*pm.Stride+x*pm.N] < 128 {
				n++
			}
		}
	}
	return n
}

// layersCheck checks page 0 of d: layerVisible is extracted, and each hidden
// token is extracted, found by search and drawn exactly when shown says so.
func layersCheck(t *testing.T, where string, d *Document, shown map[string]bool) {
	t.Helper()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	text, err := p.GetText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, layerVisible) {
		t.Errorf("%s: visible text missing from %q", where, text)
	}
	pm, err := p.Pixmap(PixmapOptions{Gray: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range layerHidden {
		q, err := p.Search(tok)
		if err != nil {
			t.Fatal(err)
		}
		dark := layersDark(pm, layersRows[tok])
		if got := strings.Contains(text, tok); got != shown[tok] {
			t.Errorf("%s: %q extracted = %v, want %v", where, tok, got, shown[tok])
		}
		if got := len(q) > 0; got != shown[tok] {
			t.Errorf("%s: %q found by search = %v, want %v", where, tok, got, shown[tok])
		}
		if got := dark > 0; got != shown[tok] {
			t.Errorf("%s: %q drawn = %v (%d dark pixels), want %v", where, tok, got, dark, shown[tok])
		}
	}
}

func layersAll(v bool) map[string]bool {
	m := map[string]bool{}
	for _, tok := range layerHidden {
		m[tok] = v
	}
	return m
}

// Content that the default optional-content configuration hides (an /OFF
// group, a group whose usage hides it, a membership policy that evaluates to
// hidden, a form with /OC and a form drawn inside a hidden span) is neither
// extracted, found nor drawn, and HiddenContent reports the layers. After
// ShowAllLayers it is ordinary content, in the same document and after a save
// and reopen: extracted, found, drawn and removable by a quad redaction.
func TestShowAllLayers(t *testing.T) {
	pdf := layersPDF()
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	layersCheck(t, "before", d, layersAll(false))
	if h := scrubHidden(t, d); h != (HiddenContent{Layers: 3, HiddenLayers: 3}) {
		t.Errorf("before: %+v, want Layers 3 and HiddenLayers 3", h)
	}

	if err := d.ShowAllLayers(); err != nil {
		t.Fatal(err)
	}
	layersCheck(t, "after", d, layersAll(true))
	if h := scrubHidden(t, d); h != (HiddenContent{}) {
		t.Errorf("after: %+v, want none", h)
	}

	// A quad redaction of one formerly hidden word removes it (without the
	// black box, so that the check below sees the word gone, not covered).
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	q, err := p.Search(layerUsage)
	if err != nil || len(q) != 1 {
		t.Fatalf("search: %v, %v", q, err)
	}
	if err := p.AddRedactionQuads(q, RedactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyRedactions(ApplyOptions{NoBox: true}); err != nil {
		t.Fatal(err)
	}
	shown := layersAll(true)
	shown[layerUsage] = false
	layersCheck(t, "redacted", d, shown)
	out, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte(layerUsage)) {
		t.Error("the redacted word is still in the saved file")
	}
	for _, name := range []string{"Off layer", "Usage layer", "On layer", "/OCProperties", "/OCMD"} {
		if bytes.Contains(out, []byte(name)) {
			t.Errorf("%q is still in the saved file", name)
		}
	}
	re, err := OpenStream(out)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	layersCheck(t, "saved", re, shown)
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("saved: %+v, want none", h)
	}
}

// MuPDF caches the optional-content configuration, and pdf_drop_ocg frees
// it without clearing the pointer: ShowAllLayers must clear it, or the next
// layer lookup, and closing the document, use freed memory. Extraction and
// rendering many times and from several goroutines, then a second
// ShowAllLayers and Close, neither crash nor bring the layers back.
func TestShowAllLayersRepeated(t *testing.T) {
	d, err := OpenStream(layersPDF())
	if err != nil {
		t.Fatal(err)
	}
	// Read the configuration first, so MuPDF has cached it.
	layersCheck(t, "before", d, layersAll(false))
	if err := d.ShowAllLayers(); err != nil {
		t.Fatal(err)
	}
	scrubConcurrently(func() {
		for range 20 {
			p, err := d.LoadPage(0)
			if err != nil {
				t.Error(err)
				return
			}
			s, err := p.GetText()
			if err != nil || !strings.Contains(s, layerOff) {
				t.Errorf("text %q, %v", s, err)
				return
			}
			if _, err := p.Pixmap(); err != nil {
				t.Error(err)
				return
			}
			if _, err := d.HiddenContent(); err != nil {
				t.Error(err)
				return
			}
		}
	})
	if err := d.ShowAllLayers(); err != nil {
		t.Fatal(err)
	}
	layersCheck(t, "again", d, layersAll(true))
	d.Close()
	if err := d.ShowAllLayers(); err == nil {
		t.Error("ShowAllLayers on a closed document succeeded")
	}
}

// Layer dictionaries that InsertPDF copied in (objects created since the
// document was opened) are destroyed too, and the document still saves:
// MuPDF 1.25 could not save one deleted outright (1.26 can).
func TestShowAllLayersInsertedObjects(t *testing.T) {
	d, err := NewPDF()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.InsertPDF(layersPDF(), ""); err != nil {
		t.Fatal(err)
	}
	if err := d.ShowAllLayers(); err != nil {
		t.Fatal(err)
	}
	out, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	for _, name := range []string{"Off layer", "Usage layer", "/OCMD"} {
		if bytes.Contains(out, []byte(name)) {
			t.Errorf("%q is still in the saved file", name)
		}
	}
	re, err := OpenStream(out)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	layersCheck(t, "inserted", re, layersAll(true))
}

// layersObjStmPDF is scrubBuildPDF with a cross-reference stream, and with
// the objects whose numbers inStm lists (none a stream) stored in one object
// stream, numbered len(objs)+1; the xref stream is len(objs)+2.
func layersObjStmPDF(objs []string, inStm ...int) []byte {
	var b, hdr, body bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+3)
	idx := map[int]int{}
	for i, o := range objs {
		n := i + 1
		if slices.Contains(inStm, n) {
			idx[n] = len(idx)
			fmt.Fprintf(&hdr, "%d %d ", n, body.Len())
			body.WriteString(o + "\n")
			continue
		}
		if dict, data, ok := strings.Cut(o, "\nstream\n"); ok {
			o = fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
		}
		offs[n] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", n, o)
	}
	stm, xref := len(objs)+1, len(objs)+2
	offs[stm] = b.Len()
	fmt.Fprintf(&b, "%d 0 obj\n<< /Type /ObjStm /N %d /First %d /Length %d >>\nstream\n%s%s\nendstream\nendobj\n",
		stm, len(idx), hdr.Len(), hdr.Len()+body.Len(), hdr.String(), body.String())
	offs[xref] = b.Len()
	var rows bytes.Buffer
	row := func(typ byte, f2 uint32, f3 uint16) {
		rows.WriteByte(typ)
		binary.Write(&rows, binary.BigEndian, f2)
		binary.Write(&rows, binary.BigEndian, f3)
	}
	row(0, 0, 65535)
	for n := 1; n <= xref; n++ {
		if i, ok := idx[n]; ok {
			row(2, uint32(stm), uint16(i))
		} else {
			row(1, uint32(offs[n]), 0)
		}
	}
	fmt.Fprintf(&b, "%d 0 obj\n<< /Type /XRef /Size %d /W [1 4 2] /Root 1 0 R /Length %d >>\nstream\n", xref, xref+1, rows.Len())
	b.Write(rows.Bytes())
	fmt.Fprintf(&b, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", offs[xref])
	return b.Bytes()
}

// layersHiddenCase checks that page 0 of pdf extracts layerVisible and none
// of hidden, that HiddenContent reads layers and HiddenLayers >= minHidden,
// and that after ShowAllLayers every token is extracted and both read 0.
func layersHiddenCase(t *testing.T, pdf []byte, layers, minHidden int, hidden ...string) {
	t.Helper()
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	text := scrubText(t, d)
	if !strings.Contains(text, layerVisible) {
		t.Fatalf("visible text missing from %q", text)
	}
	for _, tok := range hidden {
		if strings.Contains(text, tok) {
			t.Fatalf("%q is not hidden: %q", tok, text)
		}
	}
	if h := scrubHidden(t, d); h.Layers != layers || h.HiddenLayers < minHidden {
		t.Errorf("before: Layers %d, HiddenLayers %d; want %d and at least %d", h.Layers, h.HiddenLayers, layers, minHidden)
	}
	if err := d.ShowAllLayers(); err != nil {
		t.Fatal(err)
	}
	text = scrubText(t, d)
	for _, tok := range hidden {
		if !strings.Contains(text, tok) {
			t.Errorf("after ShowAllLayers: %q missing from %q", tok, text)
		}
	}
	if h := scrubHidden(t, d); h.Layers != 0 || h.HiddenLayers != 0 {
		t.Errorf("after ShowAllLayers: Layers %d, HiddenLayers %d", h.Layers, h.HiddenLayers)
	}
}

// A hidden group stored in an object stream counts: MuPDF keeps the object's
// index in the stream where an xref entry's generation would be, and the
// configuration lists the group as "N 0 R". (Reduced from pdf.js
// issue18823.pdf, a LaTeX file whose groups are all in object streams.)
func TestHiddenLayersObjectStream(t *testing.T) {
	objs := []string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [5 0 R 6 0 R 8 0 R] /D << /OFF [6 0 R 8 0 R] >> >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 7 0 R >>
   /Properties << /a 5 0 R /b 6 0 R /c 8 0 R >> >> /Contents 4 0 R >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + layerVisible + ") Tj ET /OC /a BDC BT /F1 12 Tf 20 120 Td (OnLayerQx) Tj ET EMC" +
			" /OC /b BDC BT /F1 12 Tf 20 90 Td (" + layerOff + ") Tj ET EMC /OC /c BDC BT /F1 12 Tf 20 60 Td (" + layerUsage + ") Tj ET EMC",
		/* 5 */ `<< /Type /OCG /Name (on) >>`,
		/* 6 */ `<< /Type /OCG /Name (off) >>`,
		/* 7 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 8 */ `<< /Type /OCG /Name (off too) >>`,
	}
	layersHiddenCase(t, layersObjStmPDF(objs, 5, 6, 8), 3, 2, layerOff, layerUsage)
	d, err := OpenStream(layersObjStmPDF(objs, 5, 6, 8))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if h := scrubHidden(t, d); h.HiddenLayers != 2 {
		t.Errorf("HiddenLayers %d, want 2", h.HiddenLayers)
	}
}

// Groups and membership dictionaries written inline (a /Properties entry of
// a page's direct or indirect resources, of an indirect /Properties
// dictionary, an annotation's or form's /OC) hide content too, and count. A
// membership dictionary, a group with /Usage or an /Intent other than /View,
// or an inline one counts as hidden even when it shows (fail closed); with
// no group listed in /OCProperties MuPDF hides nothing, and nothing counts.
func TestHiddenLayersInline(t *testing.T) {
	const (
		cat  = `<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [6 0 R] /D << /Order [6 0 R] >> >> >>`
		ocmd = `<< /Type /OCMD /OCGs [6 0 R] /P /AllOff >>`
		ocg  = `<< /Type /OCG /Name (design) /Intent /Design >>`
	)
	content := "\nstream\nBT /F1 12 Tf 20 180 Td (" + layerVisible + ") Tj ET /OC /m BDC BT /F1 12 Tf 20 150 Td (" + layerPolicy +
		") Tj ET EMC /OC /g BDC BT /F1 12 Tf 20 120 Td (" + layerUsage + ") Tj ET EMC /Fm1 Do"
	build := func(res, form string, extra ...string) []byte {
		return scrubBuildPDF(append([]string{
			/* 1 */ cat,
			/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
			/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources ` + res + ` /Contents 4 0 R >>`,
			/* 4 */ content,
			/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
			/* 6 */ `<< /Type /OCG /Name (on) >>`,
			/* 7 */ "/Type /XObject /Subtype /Form /BBox [0 0 300 200] " + form + " /Resources << /Font << /F1 5 0 R >> >>\nstream\nBT /F1 12 Tf 20 90 Td (" + layerForm + ") Tj ET",
		}, extra...), "")
	}
	props := `/Properties << /m ` + ocmd + ` /g ` + ocg + ` >>`
	direct := `<< /Font << /F1 5 0 R >> /XObject << /Fm1 7 0 R >> ` + props + ` >>`
	all := []string{layerPolicy, layerUsage, layerForm}
	cases := map[string][]byte{
		"page resources":      build(direct, "/OC "+ocg),
		"indirect resources":  build("8 0 R", "/OC "+ocmd, `<< /Font << /F1 5 0 R >> /XObject << /Fm1 7 0 R >> `+props+` >>`),
		"indirect properties": build(`<< /Font << /F1 5 0 R >> /XObject << /Fm1 7 0 R >> /Properties 8 0 R >>`, "/OC "+ocg, `<< /m `+ocmd+` /g `+ocg+` >>`),
		"object stream groups": layersObjStmPDF([]string{cat, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
			`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources ` + direct + ` /Contents 4 0 R >>`, content,
			`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`, `<< /Type /OCG /Name (on) >>`,
			"/Type /XObject /Subtype /Form /BBox [0 0 300 200] /OC " + ocg + " /Resources << /Font << /F1 5 0 R >> >>\nstream\nBT /F1 12 Tf 20 90 Td (" + layerForm + ") Tj ET"},
			1, 2, 3, 5, 6),
	}
	for name, pdf := range cases {
		t.Run(name, func(t *testing.T) { layersHiddenCase(t, pdf, 1, 3, all...) })
	}

	// An annotation with an inline /OC whose policy hides it.
	annot := scrubBuildPDF([]string{
		cat, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R /Annots [7 0 R] >>`,
		"\nstream\nBT /F1 12 Tf 20 180 Td (" + layerVisible + ") Tj ET",
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`, `<< /Type /OCG /Name (on) >>`,
		`<< /Type /Annot /Subtype /Square /Rect [10 10 50 50] /OC ` + ocmd + ` >>`,
	}, "")
	t.Run("annotation", func(t *testing.T) { layersHiddenCase(t, annot, 1, 1) })

	// Fail closed: an indirect membership dictionary that shows, and an
	// indirect group with /Usage that shows, count as hidden.
	for name, obj := range map[string]string{
		"visible OCMD":  `<< /Type /OCMD /OCGs [6 0 R] /P /AnyOn >>`,
		"usage ON":      `<< /Type /OCG /Name (u) /Usage << /View << /ViewState /ON >> >> >>`,
		"intent array":  `<< /Type /OCG /Name (i) /Intent [/View /Design] >>`,
		"inline shows ": `<< /Type /Annot /Subtype /Square /Rect [0 0 1 1] /OC << /Type /OCG /Name (x) >> >>`,
	} {
		t.Run(name, func(t *testing.T) {
			d, err := OpenStream(build(`<< /Font << /F1 5 0 R >> >>`, "", obj))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if h := scrubHidden(t, d); h.Layers != 1 || h.HiddenLayers != 1 {
				t.Errorf("Layers %d, HiddenLayers %d; want 1 and 1", h.Layers, h.HiddenLayers)
			}
		})
	}

	// No group listed: MuPDF hides nothing, inline or not.
	none := bytes.Replace(build(direct, "/OC "+ocg), []byte("/OCGs [6 0 R] /D << /Order [6 0 R] >>"), []byte("/OCGs [] /D << >>                    "), 1)
	d, err := OpenStream(none)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	text := scrubText(t, d)
	for _, tok := range all {
		if !strings.Contains(text, tok) {
			t.Errorf("no listed group: %q hidden", tok)
		}
	}
	if h := scrubHidden(t, d); h.Layers != 0 || h.HiddenLayers != 0 {
		t.Errorf("no listed group: Layers %d, HiddenLayers %d", h.Layers, h.HiddenLayers)
	}
}

// Pins a documented limit: a membership dictionary written inline in a
// content stream hides its content, HiddenLayers does not see it (0), Layers
// is non-zero, and ShowAllLayers reveals the content.
func TestHiddenLayersContentStreamInline(t *testing.T) {
	pdf := scrubBuildPDF([]string{
		`<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [6 0 R] /D << /Order [6 0 R] >> >> >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>`,
		"\nstream\nBT /F1 12 Tf 20 150 Td (" + layerVisible + ") Tj ET /OC << /Type /OCMD /OCGs [6 0 R] /P /AllOff >> BDC" +
			" BT /F1 12 Tf 20 100 Td (" + layerPolicy + ") Tj ET EMC",
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		`<< /Type /OCG /Name (on) >>`,
	}, "")
	layersHiddenCase(t, pdf, 1, 0, layerPolicy)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if h := scrubHidden(t, d); h.HiddenLayers != 0 {
		t.Errorf("HiddenLayers %d; the documented limit changed, update the HiddenLayers doc", h.HiddenLayers)
	}
}

// A document without optional content is left as it is.
func TestShowAllLayersNoLayers(t *testing.T) {
	pdf := scrubFixture()
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	before, text := scrubHidden(t, d), scrubText(t, d)
	if err := d.ShowAllLayers(); err != nil {
		t.Fatal(err)
	}
	if h := scrubHidden(t, d); h != before {
		t.Errorf("HiddenContent changed: %+v -> %+v", before, h)
	}
	if s := scrubText(t, d); s != text {
		t.Errorf("text changed: %q -> %q", text, s)
	}

	img, err := OpenAnyStream(makePNG(t, 8, 8), "png")
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if err := img.ShowAllLayers(); err == nil || !strings.Contains(err.Error(), "not a PDF") {
		t.Errorf("ShowAllLayers on PNG: %v", err)
	}
}
