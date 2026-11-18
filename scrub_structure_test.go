package gomupdf

import (
	"bytes"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"
)

// Tokens of scrubStructPDF, each in exactly one place of the structure tree.
const (
	stAlt     = "GomupdfStructAlt1a2b"
	stActual  = "GomupdfStructActual3c4d"
	stExp     = "GomupdfStructExp5e6f"
	stTitle   = "GomupdfStructTitle7a8b"
	stID      = "GomupdfStructID9c0d"
	stSummary = "GomupdfStructSummary2e3f"
	stHeaders = "GomupdfStructHeaders4a5b"
	stClass   = "GomupdfStructClass6c7d"
	stRole    = "GomupdfStructRole8e9f"
	stLang    = "x-GomupdfStructLang0a1b"
	stIndir   = "GomupdfStructIndirect2c3d"
	stLinkT   = "GomupdfStructLinkTitle4e5f"
	stFormT   = "GomupdfStructFormTitle6a7b"
)

var stTokens = []string{stAlt, stActual, stExp, stTitle, stID, stSummary, stHeaders, stClass, stRole, stLang,
	stIndir, stLinkT, stFormT}

// stKeys are names only the structure tree uses.
var stKeys = []string{"/StructTreeRoot", "/MarkInfo", "/ParentTree", "/IDTree", "/RoleMap", "/ClassMap", "/OBJR",
	"/MCR", "/StructElem"}

// scrubStructPDF is a tagged two-page form. Page 1 (3) has marked content
// MCID 0 (text) and 1 (a red square) and draws the form XObject 10, whose own
// marked content an MCR /Stm names; page 2 (4) has MCID 0. A structure
// element (31) points at the Link 8 through an OBJR, another (32) at the
// widget 9. The elements carry /Alt, /ActualText, /E, /T (one an indirect
// string), /ID (also an /IDTree key) and /Lang; a table has an indirect
// attribute object with /Summary, a class from the /ClassMap, and a header
// cell with /Headers; the /RoleMap maps a custom type. The catalog's /Lang 35
// is an indirect string a structure element shares. cat, link and widget are
// spliced into the catalog, the Link and the widget; more objects are
// numbered from 36.
func scrubStructPDF(cat, link, widget string, more ...string) []byte {
	return scrubBuildPDF(append([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 20 0 R /MarkInfo << /Marked true >> /Lang 35 0 R
   /AcroForm << /Fields [9 0 R] /DA (/Helv 0 Tf 0 g) >> ` + cat + ` >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> /XObject << /Fm1 10 0 R >> >>
   /Contents 6 0 R /Annots [8 0 R 9 0 R] /StructParents 0 >>`,
		/* 4 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R /StructParents 1 >>`,
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 6 */ "\nstream\n/P << /MCID 0 >> BDC BT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET EMC" +
			" /Figure << /MCID 1 >> BDC 1 0 0 rg 200 100 40 40 re f EMC /Fm1 Do",
		/* 7 */ "\nstream\n/P << /MCID 0 >> BDC BT /F1 12 Tf 20 150 Td (Tagged page two) Tj ET EMC",
		/* 8 */ `<< /Type /Annot /Subtype /Link /Rect [20 20 120 40] /A << /S /URI /URI (https://example.com/) >> /StructParent 3 ` + link + ` >>`,
		/* 9 */ `<< /Type /Annot /Subtype /Widget /FT /Tx /T (field) /V (fieldvalue) /Rect [20 60 200 80] /F 2 /P 3 0 R /StructParent 4` + widget + ` >>`,
		/* 10 */ "/Type /XObject /Subtype /Form /BBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /StructParents 2" +
			"\nstream\n/Span << /MCID 0 >> BDC BT /F1 12 Tf 20 120 Td (Form text) Tj ET EMC",
		/* 11 */ `<< >>`,
		/* 12 */ `<< >>`,
		/* 13 */ `<< >>`,
		/* 14 */ `<< >>`,
		/* 15 */ `<< >>`,
		/* 16 */ `<< >>`,
		/* 17 */ `<< >>`,
		/* 18 */ `<< >>`,
		/* 19 */ `<< >>`,
		/* 20 */ `<< /Type /StructTreeRoot /K 21 0 R /ParentTree 22 0 R /IDTree 23 0 R /RoleMap << /` + stRole + ` /P >>
   /ClassMap << /TableClass 24 0 R >> /ParentTreeNextKey 5 >>`,
		/* 21 */ `<< /Type /StructElem /S /Document /P 20 0 R /K [25 0 R 26 0 R 27 0 R 28 0 R 29 0 R 31 0 R 32 0 R] >>`,
		/* 22 */ `<< /Nums [0 [25 0 R 26 0 R] 1 [29 0 R] 2 [28 0 R] 3 31 0 R 4 32 0 R] >>`,
		/* 23 */ `<< /Names [(` + stID + `) 25 0 R] >>`,
		/* 24 */ `<< /O /Table /Summary (` + stClass + `) >>`,
		/* 25 */ `<< /Type /StructElem /S /` + stRole + ` /P 21 0 R /Pg 3 0 R /K 0 /ID (` + stID + `) /T 30 0 R /Lang (` + stLang + `) >>`,
		/* 26 */ `<< /Type /StructElem /S /Figure /P 21 0 R /Pg 3 0 R /K 1 /Alt (` + stAlt + `) /E (` + stExp + `) >>`,
		/* 27 */ `<< /Type /StructElem /S /Table /P 21 0 R /A 33 0 R /C /TableClass /T (` + stTitle + `) /K [34 0 R] >>`,
		/* 28 */ `<< /Type /StructElem /S /Span /P 21 0 R /Pg 3 0 R /K << /Type /MCR /Pg 3 0 R /Stm 10 0 R /MCID 0 >> /ActualText (` + stActual + `) >>`,
		/* 29 */ `<< /Type /StructElem /S /P /P 21 0 R /Pg 4 0 R /K 0 /Lang 35 0 R >>`,
		/* 30 */ `(` + stIndir + `)`,
		/* 31 */ `<< /Type /StructElem /S /Link /P 21 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 8 0 R >> /T (` + stLinkT + `) >>`,
		/* 32 */ `<< /Type /StructElem /S /Form /P 21 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 9 0 R >> /T (` + stFormT + `) >>`,
		/* 33 */ `<< /O /Table /Summary (` + stSummary + `) >>`,
		/* 34 */ `<< /Type /StructElem /S /TH /P 27 0 R /A << /O /Table /Headers [(` + stHeaders + `)] >> >>`,
		/* 35 */ `(en-US)`,
	}, more...), "")
}

// scrubStructPages returns the text and rendering of every page of d.
func scrubStructPages(t *testing.T, d *Document) ([]string, [][]byte) {
	t.Helper()
	var texts []string
	var pix [][]byte
	for i := range d.PageCount() {
		p, err := d.LoadPage(i)
		if err != nil {
			t.Fatal(err)
		}
		s, err := p.GetText()
		if err != nil {
			t.Fatal(err)
		}
		pm, err := p.Pixmap()
		if err != nil {
			t.Fatal(err)
		}
		texts, pix = append(texts, s), append(pix, pm.Samples)
	}
	return texts, pix
}

// scrubStructSave scrubs pdf with opts, checks that every page's text and
// rendering match an unscrubbed control, saves with scrubSaveOpts and
// returns the saved bytes and the reopened document.
func scrubStructSave(t *testing.T, pdf []byte, opts ScrubOptions) ([]byte, *Document) {
	t.Helper()
	ctl, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer ctl.Close()
	texts, pix := scrubStructPages(t, ctl)
	out, re := scrubRoundTrip(t, pdf, opts)
	if re.PageCount() != len(texts) {
		t.Fatalf("%+v: %d pages, want %d", opts, re.PageCount(), len(texts))
	}
	gotT, gotP := scrubStructPages(t, re)
	for i := range texts {
		if gotT[i] != texts[i] {
			t.Errorf("%+v: page %d text %q, want %q", opts, i, gotT[i], texts[i])
		}
		if !bytes.Equal(gotP[i], pix[i]) {
			t.Errorf("%+v: page %d rendering differs from the unscrubbed control", opts, i)
		}
	}
	return out, re
}

// A default Scrub removes the structure tree and every text it held, the
// claim that the file is tagged and the stale /StructParent(s) of pages and
// annotations, while the pages read and render as before, the catalog /Lang
// stays, the Link works and, with KeepFormFields, the widget is still the
// form's field and can be filled. The form XObject, which is page content,
// keeps its /StructParents integer.
func TestScrubRemovesStructure(t *testing.T) {
	pdf := scrubStructPDF("", "", "")
	scrubControl(t, pdf, stTokens...)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if h, want := scrubHidden(t, d), (HiddenContent{AcroForm: true, Widgets: 1, StructTree: true}); h != want {
		t.Errorf("before scrub: %+v, want %+v", h, want)
	}
	for _, opts := range []ScrubOptions{{}, {KeepFormFields: true}, {KeepAnnotations: true, KeepOutlines: true}} {
		t.Run(fmt.Sprintf("%+v", opts), func(t *testing.T) {
			out, re := scrubStructSave(t, pdf, opts)
			scrubNoTokens(t, "scrubbed", out, append(stTokens, stKeys...)...)
			if !bytes.Contains(out, []byte("(en-US)")) {
				t.Error("the catalog /Lang is gone")
			}
			if n := len(regexp.MustCompile(`/StructParents?\b`).FindAll(out, -1)); n != 1 {
				t.Errorf("%d /StructParent(s) keys in the save, want 1 (the form XObject's)", n)
			}
			want := HiddenContent{}
			if opts.KeepFormFields || opts.KeepAnnotations {
				want.AcroForm, want.Widgets = true, 1
			}
			if h := scrubHidden(t, re); h != want {
				t.Errorf("after scrub: %+v, want %+v", h, want)
			}
			p, err := re.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			if l, err := p.Links(); err != nil || len(l) != 1 || l[0].URI != "https://example.com/" {
				t.Errorf("links after scrub = %+v, %v; want the URI link", l, err)
			}
			if !opts.KeepFormFields {
				return
			}
			w, err := p.Widgets()
			if err != nil || len(w) != 1 || w[0].Value != "fieldvalue" {
				t.Fatalf("widgets %+v, %v; want the one field", w, err)
			}
			const newValue = "GomupdfStructNewValue8b9c"
			if err := p.SetTextField(0, newValue); err != nil {
				t.Fatal(err)
			}
			set, err := re.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(set, []byte("fieldvalue")) {
				t.Error("old field value survives SetTextField")
			}
			if n := len(regexp.MustCompile(`/V\s*\(`+newValue+`\)`).FindAll(set, -1)); n != 1 {
				t.Errorf("new field value set %d times, want 1", n)
			}
		})
	}
}

// KeepStructure keeps the tree, every text in it and /MarkInfo, while the
// rest of what Scrub removes (here /Info, the widget and /AcroForm) goes.
func TestScrubKeepStructure(t *testing.T) {
	const infoTok = "GomupdfStructInfo3d4e"
	pdf := scrubStructPDF("", "", "")
	pdf = bytes.Replace(pdf, []byte("/Root 1 0 R"), []byte("/Root 1 0 R /Info << /Title ("+infoTok+") >>"), 1)
	scrubControl(t, pdf, append(stTokens, infoTok)...)
	out, re := scrubStructSave(t, pdf, ScrubOptions{KeepStructure: true})
	for _, tok := range append(stTokens, "/StructTreeRoot", "/MarkInfo", "/StructParents 0", "/StructParent 3") {
		if !bytes.Contains(out, []byte(tok)) {
			t.Errorf("%q removed under KeepStructure", tok)
		}
	}
	scrubNoTokens(t, "KeepStructure", out, infoTok, "fieldvalue")
	if h := scrubHidden(t, re); h != (HiddenContent{StructTree: true}) {
		t.Errorf("after scrub: %+v, want only StructTree", h)
	}
}

// The structure tree's objects (the root, every element, the /ParentTree and
// /IDTree, the attribute objects) that something Scrub keeps also
// references (a kept Link's and a kept outline item's unknown keys, a kept
// widget's, a /Names/Dests entry) are destroyed all the same: the references
// do not keep them, nor the tree through them.
func TestScrubStructureReferencedFromKept(t *testing.T) {
	pdf := scrubStructPDF(`/Outlines 36 0 R /Names << /Dests << /Names [(d) 27 0 R] >> >>`,
		`/GomupdfRef [20 0 R 21 0 R 26 0 R 25 0 R 22 0 R 23 0 R]`, `/GomupdfRef [32 0 R 31 0 R 29 0 R 34 0 R 24 0 R]`,
		/* 36 */ `<< /Type /Outlines /First 37 0 R /Last 37 0 R /Count 1 >>`,
		/* 37 */ `<< /Title (Item) /Parent 36 0 R /Dest [3 0 R /Fit] /GomupdfRef [28 0 R 33 0 R] >>`)
	scrubControl(t, pdf, stTokens...)
	for _, opts := range []ScrubOptions{{}, {KeepOutlines: true, KeepFormFields: true}, {KeepAnnotations: true, KeepOutlines: true}} {
		t.Run(fmt.Sprintf("%+v", opts), func(t *testing.T) {
			out, re := scrubStructSave(t, pdf, opts)
			scrubNoTokens(t, "scrubbed", out, append(stTokens, stKeys...)...)
			if scrubHidden(t, re).StructTree {
				t.Error("StructTree after scrub")
			}
			if opts.KeepOutlines {
				if toc, err := re.TOC(); err != nil || len(toc) != 1 {
					t.Errorf("TOC after scrub = %+v, %v", toc, err)
				}
			}
		})
	}
}

// scrubStructAliasPDF is a one-page tagged PDF whose page draws text in the
// font 9, a square under the graphics state 10 and the image 11 through the
// indirect /Resources 5; its /MediaBox (14), /Annots (15, holding the Link
// 6, whose URI action is 7) and the page tree's /Kids (13) are indirect.
// root is the catalog's /StructTreeRoot, rootBody spliced into the root 8;
// pages (the page tree root's /Type, "/Type /Pages" if empty), font and gs
// are spliced into objects 2, 9 and 10; extra objects are numbered from 16.
func scrubStructAliasPDF(root, rootBody, pages, font, gs string, extra ...string) []byte {
	if root == "" {
		root = "8 0 R"
	}
	if pages == "" {
		pages = "/Type /Pages"
	}
	return scrubBuildPDF(append([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot ` + root + ` >>`,
		/* 2 */ `<< ` + pages + ` /Kids 13 0 R /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox 14 0 R /Resources 5 0 R /Contents 4 0 R /Annots 15 0 R /StructParents 0 >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET q /G1 gs 1 0 0 rg 10 10 80 80 re f Q q 40 0 0 40 120 20 cm /Im1 Do Q",
		/* 5 */ `<< /Font << /F1 9 0 R >> /ExtGState << /G1 10 0 R >> /XObject << /Im1 11 0 R >> >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A 7 0 R /StructParent 1 >>`,
		/* 7 */ `<< /S /URI /URI (https://example.com/) >>`,
		/* 8 */ `<< /Type /StructTreeRoot ` + rootBody + ` >>`,
		/* 9 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Courier ` + font + ` >>`,
		/* 10 */ `<< /ca 0.5 ` + gs + ` >>`,
		/* 11 */ "/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8\nstream\n\x00\xff\xff\x00",
		/* 12 */ `<< /Type /StructElem /S /Figure /P 8 0 R /K 0 /Alt (GomupdfStructAliasAlt5f6a) >>`,
		/* 13 */ `[3 0 R]`,
		/* 14 */ `[0 0 200 200]`,
		/* 15 */ `[6 0 R]`,
	}, extra...), "")
}

// A crafted structure tree that names page content, the page tree, the
// page's geometry and annotations, or the kept Link's action never damages
// them: the page reads and renders as the unscrubbed control, the Link keeps
// its target, and Scrub succeeds.
func TestScrubStructureAliasing(t *testing.T) {
	cases := []struct {
		name, root, rootBody, pages, font, gs string
		extra                                 []string
	}{
		{name: "K is the page resources", rootBody: "/K 5 0 R"},
		{name: "ParentTree lists the content stream", rootBody: "/ParentTree << /Nums [0 4 0 R 1 [4 0 R 12 0 R]] >>"},
		{name: "K is a font with /S and /P", rootBody: "/K [9 0 R 12 0 R]", font: "/S /P /P 8 0 R"},
		{name: "attribute object is the ExtGState", rootBody: "/K 16 0 R", gs: "/O /Layout",
			extra: []string{`<< /Type /StructElem /S /Figure /P 8 0 R /A 10 0 R >>`}},
		{name: "attribute object is the resources", rootBody: "/K 16 0 R",
			extra: []string{`<< /Type /StructElem /S /Figure /P 8 0 R /A [5 0 R 10 0 R] /C /X >>`}},
		{name: "root is the catalog", root: "1 0 R"},
		{name: "root is the page tree root", root: "2 0 R"},
		{name: "IDTree leaf is the served page", rootBody: "/K 12 0 R /IDTree << /Kids [3 0 R] >>"},
		{name: "IDTree value is the served page", rootBody: "/K 12 0 R /IDTree << /Names [(a) 3 0 R] >>"},
		{name: "K is the page tree's Kids", rootBody: "/K 13 0 R"},
		{name: "ParentTree is the untyped page tree root", rootBody: "/ParentTree 2 0 R", pages: "/GomupdfUntyped true"},
		{name: "ParentTree kid is the untyped page tree root", rootBody: "/ParentTree << /Kids [2 0 R] >>", pages: "/GomupdfUntyped true"},
		{name: "K and BBox are the MediaBox", rootBody: "/K [14 0 R 16 0 R]",
			extra: []string{`<< /Type /StructElem /S /Figure /P 8 0 R /A << /O /Layout /BBox 14 0 R >> >>`}},
		{name: "K is the Annots array", rootBody: "/K 15 0 R"},
		{name: "K is the Link", rootBody: "/K [6 0 R 12 0 R]"},
		{name: "K is the Link's action", rootBody: "/K [7 0 R 12 0 R]"},
		{name: "untyped root is the Link's action", root: "7 0 R"},
		{name: "RoleMap is the Link's action", rootBody: "/K 12 0 R /RoleMap 7 0 R"},
		{name: "ClassMap is the Link's action", rootBody: "/K 12 0 R /ClassMap 7 0 R"},
		{name: "K is the page", rootBody: "/K [3 0 R 12 0 R] /ParentTree << /Nums [0 3 0 R] >>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pdf := scrubStructAliasPDF(c.root, c.rootBody, c.pages, c.font, c.gs, c.extra...)
			re := scrubCheckKept(t, pdf, ScrubOptions{})
			p, err := re.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			if l, err := p.Links(); err != nil || len(l) != 1 || l[0].URI != "https://example.com/" {
				t.Errorf("links after scrub = %+v, %v; want the URI link", l, err)
			}
			if h := scrubHidden(t, re); h != (HiddenContent{}) {
				t.Errorf("after scrub: %+v", h)
			}
		})
	}
}

// scrubStructHostile runs HiddenContent, Scrub and HiddenContent again on pdf
// from several goroutines, within limit, and checks that the tree is
// reported before and gone after.
func scrubStructHostile(t *testing.T, pdf []byte, limit time.Duration) {
	t.Helper()
	scrubConcurrently(func() {
		d, err := OpenStream(pdf)
		if err != nil {
			t.Error(err)
			return
		}
		defer d.Close()
		start := time.Now()
		if h, err := d.HiddenContent(); err != nil || h != (HiddenContent{StructTree: true}) {
			t.Errorf("before scrub: %+v, %v", h, err)
		}
		if err := d.Scrub(ScrubOptions{}); err != nil {
			t.Error(err)
		}
		if h, err := d.HiddenContent(); err != nil || h != (HiddenContent{}) {
			t.Errorf("after scrub: %+v, %v", h, err)
		}
		// Capped at 5x: the shared-array bugs this guards took 15-25 s
		// against a 2 s base.
		if el := time.Since(start); el > timeLimit(limit, 5) {
			t.Errorf("HiddenContent and Scrub took %v", el)
		}
	})
}

// scrubStructTreePDF is a one-page PDF whose catalog names the structure
// tree root 4 (body rootBody); extra objects are numbered from 5.
func scrubStructTreePDF(rootBody string, extra ...string) []byte {
	return scrubBuildPDF(append([]string{
		`<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`,
		`<< /Type /StructTreeRoot ` + rootBody + ` >>`,
	}, extra...), "")
}

// Hostile trees (a /K cycle, a 200,000-deep /K chain, a /ParentTree of
// 100,000 entries naming one element, a /Nums array of 2,000,000 nulls)
// neither crash nor take long, from several goroutines, and the tree is gone
// after Scrub. Within the walk's budget the element at the bottom of the
// deep chain is destroyed although a kept Link references it.
func TestScrubStructureHostile(t *testing.T) {
	const tok = "GomupdfStructDeep8d9e"
	t.Run("cycle", func(t *testing.T) {
		scrubStructHostile(t, scrubStructTreePDF("/K 5 0 R",
			`<< /Type /StructElem /S /P /P 4 0 R /K [6 0 R 5 0 R] >>`,
			`<< /Type /StructElem /S /P /P 5 0 R /K 5 0 R /Ref [5 0 R 6 0 R] >>`), 2*time.Second)
	})
	t.Run("deep", func(t *testing.T) {
		const n = 200000
		objs := []string{
			`<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>`,
			`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
			fmt.Sprintf(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [%d 0 R] >>`, 5+n),
			`<< /Type /StructTreeRoot /K 5 0 R >>`,
		}
		for i := range n - 1 {
			objs = append(objs, fmt.Sprintf(`<< /Type /StructElem /S /Div /P %d 0 R /K %d 0 R >>`, 4+i, 6+i))
		}
		objs = append(objs, fmt.Sprintf(`<< /Type /StructElem /S /Span /P %d 0 R /Alt (%s) >>`, 3+n, tok),
			fmt.Sprintf(`<< /Type /Annot /Subtype /Link /Rect [0 0 1 1] /Dest [3 0 R /Fit] /GomupdfRef %d 0 R >>`, 4+n))
		pdf := scrubBuildPDF(objs, "")
		scrubStructHostile(t, pdf, 5*time.Second)
		out, _ := scrubRoundTrip(t, pdf, ScrubOptions{})
		scrubNoTokens(t, "deep", out, tok)
	})
	t.Run("shared parent tree", func(t *testing.T) {
		scrubStructHostile(t, scrubStructTreePDF("/K 5 0 R /ParentTree << /Nums ["+strings.Repeat("0 5 0 R ", 100000)+"] >>",
			`<< /Type /StructElem /S /P /P 4 0 R /K 0 >>`), 2*time.Second)
	})
	t.Run("null nums", func(t *testing.T) {
		scrubStructHostile(t, scrubStructTreePDF("/ParentTree << /Nums ["+strings.Repeat("0 null ", 1000000)+"] >>"),
			2*time.Second)
	})
}

// scrubStructBudgetPDF is a one-page tagged PDF with an outline item (8)
// whose /SE names the Figure 6, which holds tok and which a kept Link (7)
// also references through a stray key; the Link's direct GoTo and the
// item's indirect one (10) name it as their /SD structure destination too.
// The tree's first element 5 is walked first and has the /K k.
func scrubStructBudgetPDF(k, tok string) []byte {
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Outlines 9 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [7 0 R] >>`,
		/* 4 */ `<< /Type /StructTreeRoot /K [6 0 R 5 0 R] >>`,
		/* 5 */ `<< /Type /StructElem /S /P /P 4 0 R /Pg 3 0 R /K ` + k + ` >>`,
		/* 6 */ `<< /Type /StructElem /S /Figure /P 4 0 R /Alt (` + tok + `) >>`,
		/* 7 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 1 1] /A << /S /GoTo /D [3 0 R /Fit] /SD [6 0 R /Fit] >> /GomupdfRef 6 0 R >>`,
		/* 8 */ `<< /Title (Figure) /Parent 9 0 R /A 10 0 R /SE 6 0 R >>`,
		/* 9 */ `<< /Type /Outlines /First 8 0 R /Last 8 0 R /Count 1 >>`,
		/* 10 */ `<< /S /GoTo /D [3 0 R /Fit] /SD [6 0 R /Fit] >>`,
	}, "")
}

// The walk budget is charged per object: an element whose /K holds 70,000
// MCIDs (integers, which lead nowhere) does not exhaust it, so the Figure
// walked after it is destroyed, and a kept outline item loses the /SE that
// named it, and the kept GoTo actions their /SD. 70,000 direct MCR
// dictionaries in a file of 10 objects do exhaust it: Scrub then returns an
// error, as the tree may stay reachable (here through the kept Link's stray
// key), and still leaves no /SE or /SD on what it keeps; with 10 of them it
// succeeds.
// The walk's work stays proportional to the file when many elements or tree
// nodes share one indirect array: 20,000 elements whose /X is the same
// 100,000-entry array, and 20,000 /ParentTree kids sharing their /Nums and
// /Limits arrays, scrub in well under a second (the elements took 26.7 s,
// growing quadratically, when each element scanned the array again).
func TestScrubStructureSharedArrays(t *testing.T) {
	var nums strings.Builder
	nums.WriteString("[")
	for i := range 100000 {
		fmt.Fprintf(&nums, "%d ", i)
	}
	nums.WriteString("]")
	const n = 20000
	refs := func(first int) string {
		var b strings.Builder
		for i := range n {
			fmt.Fprintf(&b, "%d 0 R ", first+i)
		}
		return b.String()
	}
	elems := []string{
		`<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R >>`, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`, `<< >>`, `<< >>`,
		`<< /Type /StructTreeRoot /K [` + refs(8) + `] >>`, nums.String()}
	nodes := []string{
		`<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R >>`, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`, `[0 99999]`, `<< >>`,
		`<< /Type /StructTreeRoot /ParentTree << /Kids [` + refs(8) + `] >> >>`, nums.String()}
	for range n {
		elems = append(elems, `<< /Type /StructElem /S /Span /P 6 0 R /X 7 0 R >>`)
		nodes = append(nodes, `<< /Nums 7 0 R /Limits 4 0 R >>`)
	}
	for name, objs := range map[string][]string{"elements": elems, "tree nodes": nodes} {
		t.Run(name, func(t *testing.T) {
			d, err := OpenStream(scrubBuildPDF(objs, ""))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			start := time.Now()
			if err := d.Scrub(ScrubOptions{}); err != nil {
				t.Fatal(err)
			}
			if el := time.Since(start); el > timeLimit(time.Second) {
				t.Errorf("Scrub took %v", el)
			}
		})
	}
}

func TestScrubStructureBudget(t *testing.T) {
	const tok = "GomupdfStructBudget1f2a"
	mcids := "[" + strings.Repeat("0 ", 70000) + "]"
	mcrs := func(n int) string { return "[" + strings.Repeat("<< /Type /MCR /MCID 0 >> ", n) + "]" }
	for _, c := range []struct {
		name, k string
		fails   bool
	}{{"70,000 MCIDs", mcids, false}, {"10 MCRs", mcrs(10), false}, {"70,000 MCRs", mcrs(70000), true}} {
		t.Run(c.name, func(t *testing.T) {
			pdf := scrubStructBudgetPDF(c.k, tok)
			scrubControl(t, pdf, tok)
			d, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			start := time.Now()
			err = d.Scrub(ScrubOptions{KeepOutlines: true})
			// Capped at 5x like scrubStructHostile (the history records no
			// regression time for this budget test).
			if el := time.Since(start); el > timeLimit(5*time.Second, 5) {
				t.Errorf("Scrub took %v", el)
			}
			if (err != nil) != c.fails {
				t.Fatalf("Scrub error %v, want an error %v", err, c.fails)
			}
			out, err := d.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"/SE", "/SD"} {
				if bytes.Contains(out, []byte(key)) {
					t.Errorf("%s survives on what Scrub keeps", key)
				}
			}
			if got := bytes.Contains(out, []byte(tok)); got != c.fails {
				t.Errorf("Figure in the save = %v, want %v", got, c.fails)
			}
			re, err := OpenStream(out)
			if err != nil {
				t.Fatal(err)
			}
			defer re.Close()
			if toc, err := re.TOC(); err != nil || len(toc) != 1 || toc[0].Page != 0 {
				t.Errorf("TOC after scrub = %+v, %v", toc, err)
			}
		})
	}
}

// The catalog /Lang shields only a string, and only from the structure
// tree's removal, and so do the catalog values Scrub keeps for their own
// sake (/ViewerPreferences, /PageLabels, /Dests, /Threads): one naming the
// trailer /Info, the /EmbeddedFiles tree or an embedded file, the XMP stream
// or a document JavaScript's /JS string keeps none of them, with or without a
// structure tree.
func TestScrubCatalogValuesKeepNothingElse(t *testing.T) {
	const infoTok, efTok, xmpTok, jsTok = "GomupdfLangInfo1a2b", "GomupdfLangFile3c4d", "GomupdfLangXMP5e6f",
		"GomupdfLangJS7a8b"
	pdf := func(key, val, tree string) []byte {
		return scrubBuildPDF([]string{
			/* 1 */ `<< /Type /Catalog /Pages 2 0 R /` + key + ` ` + val + ` /Metadata 4 0 R ` + tree + `
   /Names << /EmbeddedFiles 12 0 R /JavaScript << /Names [(js) 7 0 R] >> >> >>`,
			/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
			/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`,
			/* 4 */ "/Type /Metadata /Subtype /XML\nstream\n<x:xmpmeta>" + xmpTok + "</x:xmpmeta>",
			/* 5 */ `<< /Type /Filespec /F (a.txt) /EF << /F 6 0 R >> >>`,
			/* 6 */ "/Type /EmbeddedFile\nstream\n" + efTok,
			/* 7 */ `<< /S /JavaScript /JS 8 0 R >>`,
			/* 8 */ `(var ` + jsTok + ` = 1;)`,
			/* 9 */ `<< /Title (` + infoTok + `) >>`,
			/* 10 */ `<< /Type /StructTreeRoot /K 11 0 R >>`,
			/* 11 */ `<< /Type /StructElem /S /P /P 10 0 R /T 8 0 R /Lang 9 0 R >>`,
			/* 12 */ `<< /Names [(a.txt) 5 0 R] >>`,
		}, "/Info 9 0 R")
	}
	for _, tree := range []string{"", "/StructTreeRoot 10 0 R"} {
		for _, key := range []string{"Lang", "ViewerPreferences", "PageLabels", "Dests", "Threads"} {
			for _, val := range []string{"9 0 R", "12 0 R", "6 0 R", "4 0 R", "8 0 R", "[9 0 R 5 0 R 4 0 R 7 0 R]"} {
				t.Run(fmt.Sprintf("%s %s %s", key, val, tree), func(t *testing.T) {
					p := pdf(key, val, tree)
					scrubControl(t, p, infoTok, efTok, xmpTok, jsTok)
					out, re := scrubRoundTrip(t, p, ScrubOptions{})
					scrubNoTokens(t, "scrubbed", out, infoTok, efTok, xmpTok, jsTok)
					if h := scrubHidden(t, re); h != (HiddenContent{}) {
						t.Errorf("after scrub: %+v", h)
					}
				})
			}
		}
	}
}

// Tokens of scrubCatalogPDF.
const (
	ctAlt  = "GomupdfCatAlt1b2c"
	ctEF   = "GomupdfCatFile3d4e"
	ctJS   = "GomupdfCatJS5f6a"
	ctInfo = "GomupdfCatInfo7b8c"
	ctXMP  = "GomupdfCatXMP9d0e"
	ctVP   = "/DuplexFlipLongEdge" // in /ViewerPreferences
	ctOA   = "/FitH 777"           // in the /OpenAction
	ctDest = "/FitV 555"           // in /Names/Dests
)

// scrubCatalogPDF is a tagged two-page document whose catalog holds what a
// structure tree must not take with it: /PageLabels (10: page 0 "pre-i",
// page 1 "sec-5", whose prefix 21 is an indirect string), /Names (11) with
// /Dests (12, naming "chap" for page 1), /EmbeddedFiles (22) and /JavaScript
// (23), /ViewerPreferences (13), a GoTo /OpenAction (14), /Outlines (15, an
// item with a GoTo /A 24 and one with the named destination) and a form
// whose widget 9 is drawn from its /MK (26) and /BS (27). Page 0 has a URI
// Link (6, /A 17) and a Link to the named destination (7, /A 18). Every
// action, the /MK and the /BS carry a stray /P naming the structure root,
// and the /MK an /O name, so each passes for a structure object. root (the
// catalog /StructTreeRoot, "20 0 R" if empty) and rootBody (spliced into the
// typed root 20) say how the tree aliases them; the element 31 holds ctAlt
// and the element 33 has the label prefix 21 as its /T. With stray, the
// /Dests tree, /ViewerPreferences and /PageLabels also reference the
// embedded file, the document JavaScript, /Info and the XMP stream.
func scrubCatalogPDF(root, rootBody string, stray bool) []byte {
	if root == "" {
		root = "20 0 R"
	}
	var dests, vp, labels string
	if stray {
		dests, vp, labels = "/GomupdfRef [22 0 R 28 0 R 29 0 R 23 0 R 30 0 R]", "/GomupdfRef [36 0 R 37 0 R]", "/GomupdfRef 36 0 R"
	}
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /StructTreeRoot ` + root + ` /MarkInfo << /Marked true >> /PageLabels 10 0 R
   /Names 11 0 R /ViewerPreferences 13 0 R /OpenAction 14 0 R /Outlines 15 0 R /Metadata 37 0 R
   /AcroForm << /Fields [9 0 R] /DA (/Helv 0 Tf 0 g) >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 8 0 R
   /Annots [6 0 R 7 0 R 9 0 R] >>`,
		/* 4 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] >>`,
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Link /Rect [20 20 120 40] /A 17 0 R >>`,
		/* 7 */ `<< /Type /Annot /Subtype /Link /Rect [130 20 230 40] /A 18 0 R >>`,
		/* 8 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
		/* 9 */ `<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /V (val) /Rect [20 60 200 100] /P 3 0 R /MK 26 0 R /BS 27 0 R >>`,
		/* 10 */ `<< /Nums [0 19 0 R 1 32 0 R] ` + labels + ` >>`,
		/* 11 */ `<< /Dests 12 0 R /EmbeddedFiles 22 0 R /JavaScript 23 0 R >>`,
		/* 12 */ `<< /Names [(chap) [4 0 R ` + ctDest + `]] ` + dests + ` >>`,
		/* 13 */ `<< /HideToolbar true /Duplex ` + ctVP + ` ` + vp + ` >>`,
		/* 14 */ `<< /S /GoTo /D [4 0 R ` + ctOA + `] /P 20 0 R >>`,
		/* 15 */ `<< /Type /Outlines /First 16 0 R /Last 25 0 R /Count 2 >>`,
		/* 16 */ `<< /Title (Item) /Parent 15 0 R /Next 25 0 R /A 24 0 R >>`,
		/* 17 */ `<< /S /URI /URI (https://example.com/) /P 20 0 R >>`,
		/* 18 */ `<< /S /GoTo /D (chap) /P 20 0 R >>`,
		/* 19 */ `<< /S /r /P (pre-) >>`,
		/* 20 */ `<< /Type /StructTreeRoot ` + rootBody + ` >>`,
		/* 21 */ `(sec-)`,
		/* 22 */ `<< /Names [(a.txt) 28 0 R] >>`,
		/* 23 */ `<< /Names [(js) 30 0 R] >>`,
		/* 24 */ `<< /S /GoTo /D [4 0 R /Fit] /P 20 0 R >>`,
		/* 25 */ `<< /Title (Named) /Parent 15 0 R /Prev 16 0 R /Dest (chap) >>`,
		/* 26 */ `<< /BG [1 0 0] /BC [0 0 1] /O /Layout >>`,
		/* 27 */ `<< /W 4 /S /D /D [3 2] /P 20 0 R >>`,
		/* 28 */ `<< /Type /Filespec /F (a.txt) /EF << /F 29 0 R >> >>`,
		/* 29 */ "/Type /EmbeddedFile\nstream\n" + ctEF,
		/* 30 */ `<< /S /JavaScript /JS (var ` + ctJS + ` = 1;) >>`,
		/* 31 */ `<< /Type /StructElem /S /Figure /P 20 0 R /Alt (` + ctAlt + `) >>`,
		/* 32 */ `<< /S /D /P 21 0 R /St 5 >>`,
		/* 33 */ `<< /Type /StructElem /S /P /P 20 0 R /T 21 0 R >>`,
		/* 34 */ `<< /Type /StructElem /S /P /P 24 0 R >>`,
		/* 35 */ `<< /Type /StructElem /S /Figure /P 20 0 R /A 26 0 R >>`,
		/* 36 */ `<< /Title (` + ctInfo + `) >>`,
		/* 37 */ "/Type /Metadata /Subtype /XML\nstream\n<x:xmpmeta>" + ctXMP + "</x:xmpmeta>",
	}, "/Info 36 0 R")
}

// scrubCatalogView is what the tests compare with an unscrubbed control.
type scrubCatalogView struct {
	labels     []PageLabel
	pageLabels []string
	links      [][]Link
	toc        []TOCEntry
	pix        [][]byte
}

func scrubCatalogLook(t *testing.T, d *Document) scrubCatalogView {
	t.Helper()
	var v scrubCatalogView
	var err error
	if v.labels, err = d.PageLabels(); err != nil {
		t.Fatal(err)
	}
	if v.toc, err = d.TOC(); err != nil {
		t.Fatal(err)
	}
	for i := range d.PageCount() {
		p, err := d.LoadPage(i)
		if err != nil {
			t.Fatal(err)
		}
		l, err := p.Label()
		if err != nil {
			t.Fatal(err)
		}
		links, err := p.Links()
		if err != nil {
			t.Fatal(err)
		}
		pm, err := p.Pixmap()
		if err != nil {
			t.Fatal(err)
		}
		v.pageLabels, v.links, v.pix = append(v.pageLabels, l), append(v.links, links), append(v.pix, pm.Samples)
	}
	return v
}

// What the catalog and the kept annotations, fields and outline items use
// stays when a crafted structure tree also names it, under every option that
// keeps them: page labels, named destinations (through the kept Link and the
// outline), viewer preferences, the /OpenAction, Link targets, the outline's
// pages and the widget's drawing all match an unscrubbed control, while the
// tree's text, /Info, XMP, the embedded file and the document JavaScript go.
func TestScrubStructureKeepsCatalogUses(t *testing.T) {
	cases := []struct{ name, root, rootBody string }{
		{name: "no aliasing", rootBody: "/K 31 0 R"},
		{name: "ParentTree is the PageLabels tree", rootBody: "/K 31 0 R /ParentTree 10 0 R"},
		{name: "RoleMap is the PageLabels tree", rootBody: "/K 31 0 R /RoleMap 10 0 R"},
		{name: "untyped root is the PageLabels tree", root: "10 0 R"},
		{name: "K holds a page-label dictionary", rootBody: "/K [31 0 R 19 0 R 32 0 R]"},
		{name: "label prefix is an element's T", rootBody: "/K [31 0 R 33 0 R]"},
		{name: "IDTree is Names/Dests", rootBody: "/K 31 0 R /IDTree 12 0 R"},
		{name: "ParentTree Kids hold Names/Dests", rootBody: "/K 31 0 R /ParentTree << /Kids [12 0 R] >>"},
		{name: "IDTree Kids hold Names/Dests", rootBody: "/K 31 0 R /IDTree << /Kids [12 0 R] >>"},
		{name: "RoleMap is the Names dictionary", rootBody: "/K 31 0 R /RoleMap 11 0 R"},
		{name: "untyped root is the Names dictionary", root: "11 0 R"},
		{name: "ClassMap is ViewerPreferences", rootBody: "/K 31 0 R /ClassMap 13 0 R"},
		{name: "untyped root is ViewerPreferences", root: "13 0 R"},
		{name: "K holds the Link's URI action", rootBody: "/K [31 0 R 17 0 R 18 0 R]"},
		{name: "K holds the OpenAction", rootBody: "/K [31 0 R 14 0 R]"},
		{name: "K holds the outline item's action", rootBody: "/K [31 0 R 24 0 R]"},
		{name: "an element's P is the outline item's action", rootBody: "/K [31 0 R 34 0 R]"},
		{name: "an element's A is the widget's MK", rootBody: "/K [31 0 R 35 0 R]"},
		{name: "K holds the widget's BS", rootBody: "/K [31 0 R 27 0 R]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pdf := scrubCatalogPDF(c.root, c.rootBody, false)
			scrubControl(t, pdf, ctEF, ctJS, ctInfo, ctXMP, ctVP, ctOA, ctDest)
			if c.root == "" {
				scrubControl(t, pdf, ctAlt)
			}
			ctlDoc, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer ctlDoc.Close()
			ctl := scrubCatalogLook(t, ctlDoc)
			if len(ctl.labels) != 2 || ctl.pageLabels[0] != "pre-i" || ctl.pageLabels[1] != "sec-5" || len(ctl.links[0]) != 2 ||
				len(ctl.toc) != 2 || ctl.toc[0].Page != 1 || ctl.toc[1].Page != 1 {
				t.Fatalf("control: %+v %q %+v %+v", ctl.labels, ctl.pageLabels, ctl.links, ctl.toc)
			}
			for _, opts := range []ScrubOptions{{}, {KeepFormFields: true}, {KeepOutlines: true},
				{KeepAnnotations: true, KeepOutlines: true}, {KeepStructure: true, KeepFormFields: true, KeepOutlines: true}} {
				out, re := scrubRoundTrip(t, pdf, opts)
				got := scrubCatalogLook(t, re)
				if !slices.Equal(got.labels, ctl.labels) || !slices.Equal(got.pageLabels, ctl.pageLabels) {
					t.Errorf("%+v: labels %+v %q, want %+v %q", opts, got.labels, got.pageLabels, ctl.labels, ctl.pageLabels)
				}
				for i := range ctl.links {
					if !slices.Equal(got.links[i], ctl.links[i]) {
						t.Errorf("%+v: page %d links %+v, want %+v", opts, i, got.links[i], ctl.links[i])
					}
				}
				if opts.KeepOutlines && !slices.Equal(got.toc, ctl.toc) {
					t.Errorf("%+v: TOC %+v, want %+v", opts, got.toc, ctl.toc)
				}
				if (opts.KeepFormFields || opts.KeepAnnotations) && !bytes.Equal(got.pix[0], ctl.pix[0]) {
					t.Errorf("%+v: page 0 (the widget) renders differently from the control", opts)
				}
				for _, tok := range []string{ctVP, ctOA, ctDest} {
					if !bytes.Contains(out, []byte(tok)) {
						t.Errorf("%+v: %q is gone", opts, tok)
					}
				}
				gone := []string{ctEF, ctJS, ctInfo, ctXMP}
				if !opts.KeepStructure {
					gone = append(gone, ctAlt)
				}
				scrubNoTokens(t, fmt.Sprintf("%+v", opts), out, gone...)
			}
		})
	}
}

// The catalog's /Names keeps /Dests while /EmbeddedFiles and /JavaScript in
// the same dictionary go and are destroyed although the kept /Dests tree,
// /ViewerPreferences and /PageLabels reference them (and the /Info and XMP
// stream) too: shielding what the catalog uses from the structure tree's
// removal keeps nothing another removal takes.
func TestScrubCatalogUsesKeepNothingRemoved(t *testing.T) {
	pdf := scrubCatalogPDF("", "/K 31 0 R", true)
	scrubControl(t, pdf, ctEF, ctJS, ctInfo, ctXMP)
	for _, opts := range []ScrubOptions{{}, {KeepAnnotations: true, KeepOutlines: true}} {
		out, re := scrubRoundTrip(t, pdf, opts)
		scrubNoTokens(t, fmt.Sprintf("%+v", opts), out, ctEF, ctJS, ctInfo, ctXMP, ctAlt)
		if !bytes.Contains(out, []byte(ctDest)) {
			t.Errorf("%+v: /Names/Dests is gone", opts)
		}
		want := HiddenContent{}
		if opts.KeepAnnotations {
			want = HiddenContent{AcroForm: true, Widgets: 1, Outlines: true}
		}
		if h := scrubHidden(t, re); h != want {
			t.Errorf("%+v: after scrub %+v, want %+v", opts, h, want)
		}
	}
}

// StructTree reads false for a /StructTreeRoot that is null or names a
// missing object, true for any other value.
func TestHiddenContentStructTree(t *testing.T) {
	for _, c := range []struct {
		val  string
		want bool
	}{{"null", false}, {"99 0 R", false}, {"<< >>", true}, {"4 0 R", true}} {
		pdf := scrubBuildPDF([]string{
			`<< /Type /Catalog /Pages 2 0 R /StructTreeRoot ` + c.val + ` >>`,
			`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
			`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`,
			`<< /Type /StructTreeRoot >>`,
		}, "")
		d, err := OpenStream(pdf)
		if err != nil {
			t.Fatal(err)
		}
		if got := scrubHidden(t, d).StructTree; got != c.want {
			t.Errorf("/StructTreeRoot %s: StructTree = %v, want %v", c.val, got, c.want)
		}
		d.Close()
	}
}

// scrubStructRedactPDF is a tagged page whose text holds word, under an
// inline /ActualText that repeats it, and which draws a form (with its own
// /StructParents) whose text holds word too and that an MCR /Stm names; the
// structure elements of both carry /ActualText with word as well.
func scrubStructRedactPDF(word string) []byte {
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 8 0 R /MarkInfo << /Marked true >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> /XObject << /Fm1 6 0 R >> >>
   /Contents 4 0 R /StructParents 0 >>`,
		/* 4 */ "\nstream\n/Span << /MCID 0 /ActualText (" + word + ") >> BDC BT /F1 12 Tf 20 150 Td (Visible " + word + ") Tj ET EMC /Fm1 Do",
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 6 */ "/Type /XObject /Subtype /Form /BBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /StructParents 1" +
			"\nstream\n/Span << /MCID 0 >> BDC BT /F1 12 Tf 20 100 Td (Form keeps this " + word + ") Tj ET EMC",
		/* 7 */ `<< /Type /StructElem /S /Document /P 8 0 R /K [9 0 R 10 0 R] /ActualText (` + word + `) >>`,
		/* 8 */ `<< /Type /StructTreeRoot /K 7 0 R /ParentTree << /Nums [0 [9 0 R] 1 [10 0 R]] >> >>`,
		/* 9 */ `<< /Type /StructElem /S /Span /P 7 0 R /Pg 3 0 R /K 0 /ActualText (` + word + `) >>`,
		/* 10 */ `<< /Type /StructElem /S /Span /P 7 0 R /Pg 3 0 R /K << /Type /MCR /Stm 6 0 R /MCID 0 >> /ActualText (` + word + `) >>`,
	}, "")
}

// Redaction and the structure removal work in either order on the same
// Document: ApplyRedactions then Scrub, and Scrub then ApplyRedactions, then
// a save. The redacted word is gone from extraction, with and without
// /ActualText, and from the saved bytes, and nothing of the tree is left.
func TestScrubStructureAndRedaction(t *testing.T) {
	const word = "GomupdfStructSecret7c8d"
	pdf := scrubStructRedactPDF(word)
	scrubControl(t, pdf, word)
	for _, scrubFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("scrub first %v", scrubFirst), func(t *testing.T) {
			d, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if scrubFirst {
				if err := d.Scrub(ScrubOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			scrubRedactWord(t, d, word)
			if !scrubFirst {
				if err := d.Scrub(ScrubOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			out, err := d.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			scrubNoTokens(t, "saved", out, append(stKeys, word)...)
			re, err := OpenStream(out)
			if err != nil {
				t.Fatal(err)
			}
			defer re.Close()
			if h := scrubHidden(t, re); h != (HiddenContent{}) {
				t.Errorf("after: %+v", h)
			}
			p, err := re.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			for _, actual := range []bool{false, true} {
				s, err := p.ExtractText(TextOptions{UseActualText: actual})
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(s, word) || !strings.Contains(s, "Visible") || !strings.Contains(s, "Form keeps this") {
					t.Errorf("UseActualText %v: text %q", actual, s)
				}
			}
		})
	}
}

// scrubStructDestPDF is a two-page tagged PDF whose kept objects use
// structure destinations (PDF 2.0: a destination whose first entry is a
// structure element). Elements 11 (/Pg 4), 12 (no /Pg; its parent 14 has
// /Pg 4), 16 (no /Pg; its first kid 17 has /Pg 4), 13 and 15 (no page
// anywhere) hold the secrets. On page 3: Link 5 has /Dest [16 /Fit], Link 6
// a GoTo /D [12 /Fit], Link 7 the named
// destination (nd) from /Names/Dests, [12 /Fit], Link 8 the /Dests entry
// /old, [13 /Fit], Link 9 a GoTo to page 3 with /SD [13 /Fit]. Outline item
// 21 has /Dest [11 /Fit], item 22 /Dest [15 /Fit], item 23 /Dest (nd).
func scrubStructDestPDF() []byte {
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 10 0 R /Outlines 20 0 R
   /Names << /Dests << /Names [(nd) [12 0 R /Fit]] >> >> /Dests << /old [13 0 R /Fit] >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [5 0 R 6 0 R 7 0 R 8 0 R 9 0 R] >>`,
		/* 4 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 20 20] /Dest [16 0 R /Fit] >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Link /Rect [30 0 50 20] /A << /S /GoTo /D [12 0 R /Fit] >> >>`,
		/* 7 */ `<< /Type /Annot /Subtype /Link /Rect [60 0 80 20] /Dest (nd) >>`,
		/* 8 */ `<< /Type /Annot /Subtype /Link /Rect [90 0 110 20] /Dest /old >>`,
		/* 9 */ `<< /Type /Annot /Subtype /Link /Rect [120 0 140 20] /A << /S /GoTo /D [3 0 R /Fit] /SD [13 0 R /Fit] >> >>`,
		/* 10 */ `<< /Type /StructTreeRoot /K [11 0 R 14 0 R 13 0 R 15 0 R 16 0 R] >>`,
		/* 11 */ `<< /Type /StructElem /S /P /P 10 0 R /Pg 4 0 R /Alt (GomupdfDestSecA) /T (GomupdfDestTsecA) >>`,
		/* 12 */ `<< /Type /StructElem /S /P /P 14 0 R /Alt (GomupdfDestSecB) >>`,
		/* 13 */ `<< /Type /StructElem /S /P /P 10 0 R /Alt (GomupdfDestSecC) >>`,
		/* 14 */ `<< /Type /StructElem /S /Sect /P 10 0 R /Pg 4 0 R /K [12 0 R] >>`,
		/* 15 */ `<< /Type /StructElem /S /P /P 10 0 R /Alt (GomupdfDestSecD) >>`,
		/* 16 */ `<< /Type /StructElem /S /Sect /P 10 0 R /K [17 0 R] /Alt (GomupdfDestSecE) >>`,
		/* 17 */ `<< /Type /StructElem /S /Span /P 16 0 R /Pg 4 0 R /K 0 >>` /* 18 */, `<< >>` /* 19 */, `<< >>`,
		/* 20 */ `<< /Type /Outlines /First 21 0 R /Last 23 0 R /Count 3 >>`,
		/* 21 */ `<< /Title (a) /Parent 20 0 R /Next 22 0 R /Dest [11 0 R /Fit] >>`,
		/* 22 */ `<< /Title (b) /Parent 20 0 R /Prev 21 0 R /Next 23 0 R /Dest [15 0 R /Fit] >>`,
		/* 23 */ `<< /Title (c) /Parent 20 0 R /Prev 22 0 R /Dest (nd) >>`,
	}, "")
}

// A structure destination does not keep its element when Scrub removes the
// tree: in a kept Link's or outline item's /Dest, a GoTo action's /D and the
// /Dests and /Names/Dests entries kept links resolve through, the element is
// replaced by its page (its /Pg, its first content item's or its nearest
// ancestor's), and a
// destination whose element has no page is dropped (the Link or outline item
// stays, as with a removed unsafe action); /SD goes from kept GoTo actions
// as before. The elements and their text are gone, the Links and the outline
// item go to page 2, and HiddenContent reads clean. With KeepStructure
// nothing changes.
func TestScrubStructureDestinations(t *testing.T) {
	pdf := scrubStructDestPDF()
	secrets := []string{"GomupdfDestSecA", "GomupdfDestTsecA", "GomupdfDestSecB", "GomupdfDestSecC", "GomupdfDestSecD", "GomupdfDestSecE"}
	scrubControl(t, pdf, secrets...)
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{KeepOutlines: true})
	scrubNoTokens(t, "scrubbed", out, append(secrets, "/StructElem", "/SD")...)
	if h := scrubHidden(t, re); h != (HiddenContent{Outlines: true}) {
		t.Errorf("after scrub %+v", h)
	}
	p, err := re.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	links, err := p.Links()
	if err != nil || len(links) < 3 {
		t.Fatalf("links %+v, %v", links, err)
	}
	for _, l := range links[:2] {
		if !strings.HasPrefix(l.URI, "#page=2") {
			t.Errorf("link at %v goes to %q, want page 2", l.Rect, l.URI)
		}
	}
	if links[2].URI != "#nameddest=nd" {
		t.Errorf("named link goes to %q", links[2].URI)
	}
	// MuPDF resolves neither (nd) here (a catalog /Dests shadows
	// /Names/Dests) nor the outline item whose destination was dropped, so
	// the rewritten name-tree entry is read from the bytes (a Garbage 1
	// save keeps object numbers: 4 is page 2).
	if !bytes.Contains(out, []byte("(nd)[4 0 R/Fit]")) {
		t.Error("the (nd) destination does not name page 2")
	}
	toc, err := re.TOC()
	if err != nil || len(toc) != 3 || toc[0].Page != 1 {
		t.Errorf("TOC %+v, %v", toc, err)
	}
	out, _ = scrubRoundTrip(t, pdf, ScrubOptions{KeepOutlines: true, KeepStructure: true})
	for _, s := range secrets {
		if !bytes.Contains(out, []byte(s)) {
			t.Errorf("KeepStructure: %s is gone", s)
		}
	}
}

// A structure element that is directly the value of a catalog key Scrub
// keeps for its own sake (/Dests, /ViewerPreferences, /Threads) is not
// shielded by it: the element and its /Alt go with the tree.
func TestScrubCatalogValueIsElement(t *testing.T) {
	const tok = "GomupdfCatElemAlt5c6d"
	for _, val := range []string{"/Dests 5 0 R", "/ViewerPreferences 5 0 R", "/Threads [5 0 R]"} {
		t.Run(val, func(t *testing.T) {
			pdf := scrubBuildPDF([]string{
				/* 1 */ `<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R ` + val + ` >>`,
				/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
				/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`,
				/* 4 */ `<< /Type /StructTreeRoot /K [5 0 R] >>`,
				/* 5 */ `<< /Type /StructElem /S /Figure /P 4 0 R /Alt (` + tok + `) >>`,
			}, "")
			scrubControl(t, pdf, tok)
			out, _ := scrubRoundTrip(t, pdf, ScrubOptions{})
			scrubNoTokens(t, val, out, tok)
		})
	}
}

// A kept form whose /Fields, a field's /Kids or /CO is an indirect array
// scrubs and keeps its widget: removing the structure tree's references from
// kept objects (/SE, /SD) skips the arrays kept with them (it threw "not a
// dict (array)" on them, so KeepFormFields and KeepAnnotations failed on
// ordinary forms).
func TestScrubKeptIndirectArrays(t *testing.T) {
	page := `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>`
	pages := `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`
	for name, objs := range map[string][]string{
		"Fields": {`<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields 5 0 R >> >>`, pages, page,
			`<< /Type /Annot /Subtype /Widget /FT /Tx /T (a) /Rect [0 0 50 50] /P 3 0 R >>`, `[4 0 R]`},
		"field Kids": {`<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [6 0 R] >> >>`, pages, page,
			`<< /Type /Annot /Subtype /Widget /Rect [0 0 50 50] /P 3 0 R /Parent 6 0 R >>`, `[4 0 R]`,
			`<< /FT /Tx /T (a) /Kids 5 0 R >>`},
		"CO": {`<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /CO 5 0 R >> >>`, pages, page,
			`<< /Type /Annot /Subtype /Widget /FT /Tx /T (a) /Rect [0 0 50 50] /P 3 0 R >>`, `[4 0 R]`},
	} {
		for _, opts := range []ScrubOptions{{KeepFormFields: true}, {KeepAnnotations: true, KeepOutlines: true}} {
			t.Run(fmt.Sprintf("%s %+v", name, opts), func(t *testing.T) {
				_, re := scrubRoundTrip(t, scrubBuildPDF(objs, ""), opts)
				if h := scrubHidden(t, re); !h.AcroForm || h.Widgets != 1 {
					t.Errorf("after scrub %+v", h)
				}
			})
		}
	}
}
