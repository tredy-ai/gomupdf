package gomupdf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tredy-ai/gomupdf/geometry"
)

// Distinctive tokens stored as plain (uncompressed, literal) strings in the
// scrub fixture, so a raw byte search for them is meaningful.
const (
	scrubTitle  = "GomupdfScrubTitle7f3a"
	scrubAttach = "gomupdf-scrub-attachment-91c2.txt"
	scrubScript = "gomupdfScrubScript4d8e"
)

// scrubSaveOpts garbage-collects but neither deflates streams nor encrypts, so
// anything that survives the save is visible to a raw byte search.
var scrubSaveOpts = SaveOptions{Garbage: 1}

// scrubBuildPDF writes a PDF whose objects are numbered 1..len(objs) in order, with
// a correct xref table. Object 1 must be the catalog. A body of the form
// "<dict entries>\nstream\n<data>" becomes a stream object (/Length added).
func scrubBuildPDF(objs []string, trailerExtra string) []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, body := range objs {
		offsets[i] = b.Len()
		if dict, data, ok := strings.Cut(body, "\nstream\n"); ok {
			body = fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
		}
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R %s >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, trailerExtra, xref)
	return b.Bytes()
}

const scrubPageText = "Scrub fixture page text"

// scrubFixture is a one-page PDF with every kind of content HiddenContent
// reports: /Info, XMP, /AcroForm with a widget, /Outlines, two embedded files
// in a /Kids name tree, catalog and page /AF, document JavaScript, a Launch
// action in the /OpenAction /Next chain, catalog, page and widget /AA, a
// Square annotation, a Popup the Square does not point to (MuPDF lists no
// Popups, so only Scrub's raw /Annots sweep removes it), an annotation of an
// unknown subtype, a Link, /Thumb, page /Metadata and catalog and page
// /PieceInfo.
func scrubFixture() []byte {
	xmp := `/Type /Metadata /Subtype /XML
stream
<x:xmpmeta xmlns:x="adobe:ns:meta/"><dc:title>` + scrubTitle + `</dc:title></x:xmpmeta>`
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /Metadata 5 0 R /AcroForm << /Fields [10 0 R] >>
   /Outlines 11 0 R /PageMode /UseOutlines /Names << /EmbeddedFiles 13 0 R /JavaScript 17 0 R >>
   /OpenAction 19 0 R /AA << /WC 20 0 R >> /AF [15 0 R] /PieceInfo << /GoMuPDF << /Private (catalog piece) >> >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 6 0 R >> >>
   /Contents 4 0 R /Annots [8 0 R 9 0 R 10 0 R 21 0 R 27 0 R] /Thumb 22 0 R /AA << /O 23 0 R >> /AF [15 0 R]
   /Metadata 24 0 R /PieceInfo << /GoMuPDF << /Private (page piece) >> >> >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 100 Td (" + scrubPageText + ") Tj ET",
		/* 5 */ xmp,
		/* 6 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 7 */ `<< /Title (` + scrubTitle + `) /Author (Scrub Author) >>`,
		/* 8 */ `<< /Type /Annot /Subtype /Link /Rect [20 20 120 40] /Border [0 0 0] /A << /S /URI /URI (https://example.com/) >> >>`,
		/* 9 */ `<< /Type /Annot /Subtype /Square /Rect [150 20 250 60] /C [1 0 0] >>`,
		/* 10 */ `<< /Type /Annot /Subtype /Widget /FT /Tx /T (field1) /Rect [20 150 200 170] /P 3 0 R /AA << /K 25 0 R >> >>`,
		/* 11 */ `<< /Type /Outlines /First 12 0 R /Last 12 0 R /Count 1 >>`,
		/* 12 */ `<< /Title (Chapter one) /Parent 11 0 R /Dest [3 0 R /Fit] >>`,
		/* 13 */ `<< /Kids [14 0 R 26 0 R] >>`,
		/* 14 */ `<< /Limits [(` + scrubAttach + `) (` + scrubAttach + `)] /Names [(` + scrubAttach + `) 15 0 R] >>`,
		/* 15 */ `<< /Type /Filespec /F (` + scrubAttach + `) /UF (` + scrubAttach + `) /EF << /F 16 0 R >> /AFRelationship /Data >>`,
		/* 16 */ "/Type /EmbeddedFile\nstream\nattachment body",
		/* 17 */ `<< /Names [(docjs) 18 0 R] >>`,
		/* 18 */ `<< /S /JavaScript /JS (var ` + scrubScript + ` = 1;) >>`,
		/* 19 */ `<< /S /GoTo /D [3 0 R /Fit] /Next [<< /S /Launch /F (calc.exe) >>] >>`,
		/* 20 */ `<< /S /JavaScript /JS (var willClose = 1;) >>`,
		/* 21 */ `<< /Type /Annot /Subtype /Popup /Rect [250 60 300 100] /Parent 9 0 R >>`,
		/* 22 */ "/Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8\nstream\nx",
		/* 23 */ `<< /S /JavaScript /JS (var pageOpen = 1;) >>`,
		/* 24 */ "/Type /Metadata /Subtype /XML\nstream\n<x:xmpmeta xmlns:x=\"adobe:ns:meta/\"/>",
		/* 25 */ `<< /S /JavaScript /JS (var keystroke = 1;) >>`,
		/* 26 */ `<< /Limits [(zz-second.txt) (zz-second.txt)] /Names [(zz-second.txt) 15 0 R] >>`,
		/* 27 */ `<< /Type /Annot /Subtype /GomupdfUnknown /Rect [200 150 250 170] >>`,
	}, "/Info 7 0 R")
}

func scrubHidden(t *testing.T, d *Document) HiddenContent {
	t.Helper()
	h, err := d.HiddenContent()
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func scrubText(t *testing.T, d *Document) string {
	t.Helper()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.GetText()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// scrubRoundTrip opens pdf, scrubs it with opts, saves with scrubSaveOpts and
// returns the saved bytes and the reopened document.
func scrubRoundTrip(t *testing.T, pdf []byte, opts ScrubOptions) ([]byte, *Document) {
	t.Helper()
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Scrub(opts); err != nil {
		t.Fatal(err)
	}
	out, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	re, err := OpenStream(out)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(re.Close)
	return out, re
}

var scrubTokens = []string{scrubTitle, scrubAttach, scrubScript}

func TestScrubRemovesEverything(t *testing.T) {
	pdf := scrubFixture()
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	want := HiddenContent{
		Info: true, XMP: true, AcroForm: true, Outlines: true,
		EmbeddedFiles: 2, AssociatedFiles: 2, JavaScript: true,
		NonLinkAnnotations: 3, Widgets: 1, Thumbnails: 1, PageMetadata: 1, PieceInfo: 2,
	}
	if got := scrubHidden(t, d); got != want {
		t.Fatalf("before scrub:\n got %+v\nwant %+v", got, want)
	}
	text := scrubText(t, d)
	if !strings.Contains(text, scrubPageText) {
		t.Fatalf("fixture page text = %q", text)
	}

	// Control: the same save without Scrub keeps every token in plain bytes,
	// so their absence below is due to Scrub, not to the save options.
	control, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range scrubTokens {
		if !bytes.Contains(pdf, []byte(tok)) || !bytes.Contains(control, []byte(tok)) {
			t.Fatalf("token %q missing from the fixture or the unscrubbed save", tok)
		}
	}

	out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
	if got := scrubHidden(t, re); got != (HiddenContent{}) {
		t.Errorf("after scrub: %+v, want all zero", got)
	}
	if got := scrubText(t, re); got != text {
		t.Errorf("page text changed: %q -> %q", text, got)
	}
	for _, tok := range scrubTokens {
		if bytes.Contains(out, []byte(tok)) {
			t.Errorf("scrubbed save still contains %q", tok)
		}
	}
	if bytes.Contains(out, []byte("UseOutlines")) || !bytes.Contains(out, []byte("UseNone")) {
		t.Error("/PageMode /UseOutlines not reset to /UseNone")
	}

	// The Link survives the default scrub.
	p, _ := re.LoadPage(0)
	links, err := p.Links()
	if err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 || links[0].URI != "https://example.com/" {
		t.Errorf("links after scrub = %+v, want the one URI link", links)
	}
}

func TestScrubKeepAnnotations(t *testing.T) {
	_, re := scrubRoundTrip(t, scrubFixture(), ScrubOptions{KeepAnnotations: true})
	// Annotations, widgets and /AcroForm stay; their /AA is still removed.
	want := HiddenContent{AcroForm: true, NonLinkAnnotations: 3, Widgets: 1}
	if got := scrubHidden(t, re); got != want {
		t.Errorf("got %+v\nwant %+v", got, want)
	}
	p, _ := re.LoadPage(0)
	if w, err := p.Widgets(); err != nil || len(w) != 1 {
		t.Errorf("widgets = %v, %v; want 1", w, err)
	}
}

func TestScrubKeepOutlines(t *testing.T) {
	out, re := scrubRoundTrip(t, scrubFixture(), ScrubOptions{KeepOutlines: true})
	if got := scrubHidden(t, re); got != (HiddenContent{Outlines: true}) {
		t.Errorf("got %+v, want only Outlines", got)
	}
	toc, err := re.TOC()
	if err != nil || len(toc) != 1 || toc[0].Title != "Chapter one" {
		t.Errorf("TOC = %+v, %v", toc, err)
	}
	if !bytes.Contains(out, []byte("UseOutlines")) {
		t.Error("/PageMode changed although outlines were kept")
	}

	// A kept outline item that /Info also points at is unlinked as /Info,
	// not destroyed.
	_, re = scrubRoundTrip(t, scrubBuildPDF([]string{
		`<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`,
		`<< /Type /Outlines /First 5 0 R /Last 5 0 R /Count 1 >>`,
		`<< /Title (Chapter one) /Parent 4 0 R /Dest [3 0 R /Fit] >>`,
	}, "/Info 5 0 R"), ScrubOptions{KeepOutlines: true})
	toc, err = re.TOC()
	if err != nil || len(toc) != 1 || toc[0].Title != "Chapter one" {
		t.Errorf("TOC with /Info on the outline item = %+v, %v", toc, err)
	}
}

// scrubMiniPDF is a one-page PDF with extra catalog, page and Link annotation
// entries, plus extra objects numbered from 5.
func scrubMiniPDF(catalog, page, link string, extra ...string) []byte {
	return scrubBuildPDF(append([]string{
		`<< /Type /Catalog /Pages 2 0 R ` + catalog + ` >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] ` + page + ` >>`,
		`<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A << /S /URI /URI (https://example.com/) >> ` + link + ` >>`,
	}, extra...), "")
}

// Each JavaScript location is detected on its own and removed by Scrub. The
// Link keeps existing but loses its /AA.
func TestScrubJavaScriptLocations(t *testing.T) {
	js := `<< /S /JavaScript /JS (1) >>`
	cases := map[string][]byte{
		"names":      scrubMiniPDF(`/Names << /JavaScript << /Names [(a) `+js+`] >> >>`, "", ""),
		"catalog AA": scrubMiniPDF(`/AA << /WC `+js+` >>`, "", ""),
		"page AA":    scrubMiniPDF("", `/AA << /C `+js+` >>`, ""),
		"link AA":    scrubMiniPDF("", "", `/AA << /E `+js+` >>`),
		"launch":     scrubMiniPDF("", `/AA << /O << /S /Launch /F (x) >> >>`, ""),
		"AA next":    scrubMiniPDF("", `/AA << /O << /S /GoTo /D [3 0 R /Fit] /Next `+js+` >> >>`, ""),
	}
	for name, pdf := range cases {
		t.Run(name, func(t *testing.T) {
			d, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if !scrubHidden(t, d).JavaScript {
				t.Fatal("JavaScript not detected")
			}
			_, re := scrubRoundTrip(t, pdf, ScrubOptions{})
			if got := scrubHidden(t, re); got != (HiddenContent{}) {
				t.Errorf("after scrub: %+v", got)
			}
			p, _ := re.LoadPage(0)
			if l, _ := p.Links(); len(l) != 1 {
				t.Errorf("links after scrub = %d, want 1", len(l))
			}
		})
	}
}

func TestScrubOpenAction(t *testing.T) {
	cases := []struct {
		name     string
		pdf      []byte
		unsafe   bool // detected as JavaScript and removed
		hasValue bool // /OpenAction present after scrub
	}{
		{"destination", scrubMiniPDF(`/OpenAction [3 0 R /Fit]`, "", ""), false, true},
		{"goto", scrubMiniPDF(`/OpenAction << /S /GoTo /D [3 0 R /Fit] >>`, "", ""), false, true},
		{"javascript", scrubMiniPDF(`/OpenAction << /S /JavaScript /JS (1) >>`, "", ""), true, false},
		{"next dict launch", scrubMiniPDF(`/OpenAction << /S /GoTo /D [3 0 R /Fit] /Next << /S /Launch /F (x) >> >>`, "", ""), true, false},
		{"next array javascript", scrubMiniPDF(`/OpenAction << /S /GoTo /D [3 0 R /Fit] /Next [<< /S /GoTo /D [3 0 R /Fit] >> << /S /JavaScript /JS (1) >>] >>`, "", ""), true, false},
		// A cyclic /Next chain terminates and fails closed.
		{"cycle", scrubMiniPDF(`/OpenAction 5 0 R`, "", "", `<< /S /GoTo /D [3 0 R /Fit] /Next 5 0 R >>`), true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := OpenStream(c.pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if got := scrubHidden(t, d).JavaScript; got != c.unsafe {
				t.Errorf("JavaScript = %v, want %v", got, c.unsafe)
			}
			out, re := scrubRoundTrip(t, c.pdf, ScrubOptions{})
			if got := bytes.Contains(out, []byte("/OpenAction")); got != c.hasValue {
				t.Errorf("/OpenAction present after scrub = %v, want %v", got, c.hasValue)
			}
			if scrubHidden(t, re).JavaScript {
				t.Error("JavaScript after scrub")
			}
		})
	}
}

// scrubConcurrently runs f from several goroutines at once. Deep C recursion
// overflows the small stacks of the threads that run cgo calls for ordinary
// goroutines (128 KiB on musl), while a single call from the test goroutine
// may land on the main thread and survive.
func scrubConcurrently(f func()) {
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f()
		}()
	}
	wg.Wait()
}

// scrubRefs returns "a 0 R b 0 R ..." for the object numbers in nums.
func scrubRefs(nums ...int) string {
	var b strings.Builder
	for _, n := range nums {
		fmt.Fprintf(&b, "%d 0 R ", n)
	}
	return b.String()
}

// Name trees that cannot be walked in full (cyclic, deeper than the depth cap,
// more nodes than the budget) neither crash nor report zero embedded files.
func TestScrubNameTreeLimits(t *testing.T) {
	const filespec = `<< /Type /Filespec /F (real.txt) >>`
	names := func(extra ...string) []byte {
		return scrubMiniPDF(`/Names << /EmbeddedFiles 5 0 R >>`, "", "", extra...)
	}

	// A chain 5 -> 6 -> ... of 1<<16 + 10 nodes ending in one real entry.
	var chain []string
	const chainLen = 1<<16 + 10
	for i := range chainLen - 1 {
		chain = append(chain, `<< /Kids [`+scrubRefs(i+6)+`] >>`)
	}
	chain = append(chain, `<< /Names [(real.txt) `+filespec+`] >>`)

	// 70,000 references to one empty node, then the real leaf.
	wide := make([]int, 70000)
	for i := range wide {
		wide[i] = 6
	}

	cases := map[string][]byte{
		"cycle": names(`<< /Kids [5 0 R] >>`),
		"deep":  names(chain...),
		"wide": names(`<< /Kids [`+scrubRefs(append(wide, 7)...)+`] >>`,
			`<< /Names [] >>`, `<< /Names [(real.txt) `+filespec+`] >>`),
	}
	for name, pdf := range cases {
		t.Run(name, func(t *testing.T) {
			scrubConcurrently(func() {
				d, err := OpenStream(pdf)
				if err != nil {
					t.Error(err)
					return
				}
				defer d.Close()
				h, err := d.HiddenContent()
				if err != nil || h.EmbeddedFiles < 1 {
					t.Errorf("HiddenContent = %+v, %v; want EmbeddedFiles >= 1", h, err)
				}
				if err := d.Scrub(ScrubOptions{}); err != nil {
					t.Error(err)
				}
				if h, err := d.HiddenContent(); err != nil || h != (HiddenContent{}) {
					t.Errorf("after scrub: %+v, %v", h, err)
				}
			})
		})
	}
}

// Field trees that are cyclic or deeper than any limit, with a widget at the
// bottom, neither crash Scrub (MuPDF's own widget removal walks /Fields) nor
// leave the widget.
func TestScrubFieldTreeLimits(t *testing.T) {
	// fieldPDF: catalog, pages, page with the widget, then fields from 4.
	fieldPDF := func(widget int, fields ...string) []byte {
		objs := append([]string{
			`<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>`,
			`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
			`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [` + scrubRefs(widget) + `] >>`,
		}, fields...)
		return scrubBuildPDF(objs, "")
	}
	const n = 1<<16 + 10
	var chain []string
	for i := range n - 1 {
		chain = append(chain, `<< /T (f) /Kids [`+scrubRefs(i+5)+`] >>`)
	}
	w := 4 + n - 1
	chain = append(chain, fmt.Sprintf(`<< /Type /Annot /Subtype /Widget /FT /Tx /Parent %d 0 R /Rect [0 0 50 20] /V (x) >>`, w-1))

	cases := map[string][]byte{
		"cycle": fieldPDF(5, `<< /T (f) /Kids [4 0 R 5 0 R] >>`,
			`<< /Type /Annot /Subtype /Widget /FT /Tx /Parent 4 0 R /Rect [0 0 50 20] /V (x) >>`),
		"deep": fieldPDF(w, chain...),
	}
	for name, pdf := range cases {
		t.Run(name, func(t *testing.T) {
			scrubConcurrently(func() {
				d, err := OpenStream(pdf)
				if err != nil {
					t.Error(err)
					return
				}
				defer d.Close()
				if h, err := d.HiddenContent(); err != nil || h.Widgets != 1 || !h.AcroForm {
					t.Errorf("before scrub: %+v, %v", h, err)
				}
				if err := d.Scrub(ScrubOptions{}); err != nil {
					t.Error(err)
				}
				if h, err := d.HiddenContent(); err != nil || h != (HiddenContent{}) {
					t.Errorf("after scrub: %+v, %v", h, err)
				}
			})
		})
	}
}

// Scrub destroys only objects of the kind it removes: a /Popup that points at
// a Link or at the page content, or a /Thumb that is the page's content
// stream, is unlinked but not destroyed. A /Thumb typed /XObject is destroyed.
func TestScrubDestroysOnlyRemovedKinds(t *testing.T) {
	const thumbTok = "GomupdfThumbXObject4e2a"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /GomupdfRefs [7 0 R] >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R 8 0 R] /Count 2 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 6 0 R >> >>
   /Contents 4 0 R /Annots [5 0 R 9 0 R 10 0 R] /Thumb 7 0 R >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 100 Td (" + scrubPageText + ") Tj ET",
		/* 5 */ `<< /Subtype /Link /Rect [20 20 120 40] /A << /S /URI /URI (https://example.com/) >> >>`,
		/* 6 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 7 */ fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8\nstream\n%s", len(thumbTok), thumbTok),
		/* 8 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Thumb 4 0 R >>`,
		/* 9 */ `<< /Subtype /Square /Rect [150 20 250 60] /Popup 5 0 R >>`,
		/* 10 */ `<< /Subtype /Square /Rect [150 80 250 120] /Popup 4 0 R >>`,
	}, "")
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
	if got := scrubText(t, re); !strings.Contains(got, scrubPageText) {
		t.Errorf("page text after scrub = %q", got)
	}
	p, _ := re.LoadPage(0)
	if l, err := p.Links(); err != nil || len(l) != 1 {
		t.Errorf("links after scrub = %+v, %v; want 1", l, err)
	}
	if bytes.Contains(out, []byte(thumbTok)) {
		t.Error("/Thumb typed /XObject survived through another reference")
	}
}

// Pins a documented limit: a filespec Scrub's walk never reaches (here, past
// the 65,536-node budget) is only unlinked, so a second reference keeps it in
// the saved file. Within the budget the same filespec is destroyed.
func TestScrubBeyondLimitOnlyUnlinks(t *testing.T) {
	const tok = "gomupdf-beyond-limit-6d3b.txt"
	pdf := func(empties int) []byte {
		kids := make([]int, empties)
		for i := range kids {
			kids[i] = 6
		}
		return scrubMiniPDF(`/Names << /EmbeddedFiles 5 0 R >> /GomupdfRefs [8 0 R]`, "", "",
			`<< /Kids [`+scrubRefs(append(kids, 7)...)+`] >>`, `<< /Names [] >>`,
			`<< /Names [(x) 8 0 R] >>`, `<< /Type /Filespec /F (`+tok+`) >>`)
	}
	for _, c := range []struct {
		empties int
		leaks   bool
	}{{10, false}, {70000, true}} {
		out, _ := scrubRoundTrip(t, pdf(c.empties), ScrubOptions{})
		if got := bytes.Contains(out, []byte(tok)); got != c.leaks {
			t.Errorf("%d empty kids: filespec in save = %v, want %v", c.empties, got, c.leaks)
		}
	}
}

// A /Next chain longer than any limit fails closed without crashing.
func TestScrubDeepActionChain(t *testing.T) {
	const n = 5000
	var acts []string
	for i := range n - 1 {
		acts = append(acts, `<< /S /GoTo /D [3 0 R /Fit] /Next `+scrubRefs(i+6)+`>>`)
	}
	acts = append(acts, `<< /S /GoTo /D [3 0 R /Fit] >>`)
	pdf := scrubMiniPDF(`/OpenAction 5 0 R`, "", "", acts...)
	scrubConcurrently(func() {
		d, err := OpenStream(pdf)
		if err != nil {
			t.Error(err)
			return
		}
		defer d.Close()
		if h, err := d.HiddenContent(); err != nil || !h.JavaScript {
			t.Errorf("HiddenContent = %+v, %v; want JavaScript", h, err)
		}
	})
}

// Tokens of the tagged fixture, each in exactly one object Scrub removes.
const (
	tagAnnot      = "GomupdfTagAnnotContents3c1d"
	tagPopup      = "GomupdfTagPopupContents8a2f"
	tagWidget     = "GomupdfTagWidgetValue6e4b"
	tagField      = "GomupdfTagFieldValue1f9c"
	tagInfo       = "GomupdfTagInfoTitle0d7a"
	tagXMP        = "GomupdfTagXMPTitle5c3e"
	tagPageMeta   = "GomupdfTagPageMeta2b8d"
	tagThumb      = "GomupdfTagThumb9e1a"
	tagAttach     = "gomupdf-tag-attachment-4f6b.txt"
	tagAttachBody = "GomupdfTagAttachBody7d2c"
	tagAF         = "gomupdf-tag-af-8c5e.txt"
	tagAFBody     = "GomupdfTagAFBody3a9f"
	tagJS         = "GomupdfTagJSString6b1e"
	tagJSStream   = "GomupdfTagJSStream2e7d"
)

var tagAnnotTokens = []string{tagAnnot, tagPopup, tagWidget, tagField}

var tagTokens = append([]string{tagInfo, tagXMP, tagPageMeta, tagThumb, tagAttach,
	tagAttachBody, tagAF, tagAFBody, tagJS, tagJSStream}, tagAnnotTokens...)

// scrubTaggedFixture is a tagged PDF in which everything Scrub removes is also
// reachable from the structure tree: StructElems point at the Square
// annotation and both widgets through /OBJR (the shape Word exports), a
// StructElem /AF lists the embedded file's filespec and the page's associated
// file, and a StructElem carries references to every other removed object
// (the parent field, Info, XMP, page /Metadata, /Thumb, the popup, /EF
// streams, the JavaScript actions and script stream). Unlinking alone leaves
// all of these in a garbage-collected save.
func scrubTaggedFixture() []byte {
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 20 0 R
   /Metadata 7 0 R /AcroForm 13 0 R /Names << /EmbeddedFiles 15 0 R /JavaScript 18 0 R >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 6 0 R >> >>
   /Contents 4 0 R /Annots [5 0 R 9 0 R 10 0 R 12 0 R 27 0 R] /Metadata 8 0 R /Thumb 11 0 R /AF [26 0 R] >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 100 Td (" + scrubPageText + ") Tj ET",
		/* 5 */ `<< /Type /Annot /Subtype /Link /Rect [20 20 120 40] /A << /S /URI /URI (https://example.com/) >> >>`,
		/* 6 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 7 */ "/Type /Metadata /Subtype /XML\nstream\n<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">" + tagXMP + "</x:xmpmeta>",
		/* 8 */ "/Type /Metadata /Subtype /XML\nstream\n<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">" + tagPageMeta + "</x:xmpmeta>",
		/* 9 */ `<< /Type /Annot /Subtype /Square /Rect [150 20 250 60] /Contents (` + tagAnnot + `) /Popup 10 0 R /StructParent 1 >>`,
		/* 10 */ `<< /Type /Annot /Subtype /Popup /Rect [250 60 300 100] /Parent 9 0 R /Contents (` + tagPopup + `) >>`,
		/* 11 */ fmt.Sprintf("/Width %d /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8\nstream\n%s", len(tagThumb), tagThumb),
		/* 12 */ `<< /Type /Annot /Subtype /Widget /FT /Tx /T (merged) /V (` + tagWidget + `) /Rect [20 150 200 170] /P 3 0 R /StructParent 2 >>`,
		/* 13 */ `<< /Fields [12 0 R 14 0 R] >>`,
		/* 14 */ `<< /FT /Tx /T (parent) /V (` + tagField + `) /Kids [27 0 R] >>`,
		/* 15 */ `<< /Names [(` + tagAttach + `) 16 0 R] >>`,
		/* 16 */ `<< /Type /Filespec /F (` + tagAttach + `) /UF (` + tagAttach + `) /EF << /F 17 0 R >> /AFRelationship /Data >>`,
		/* 17 */ "/Type /EmbeddedFile\nstream\n" + tagAttachBody,
		/* 18 */ `<< /Names [(a) 19 0 R (b) 28 0 R] >>`,
		/* 19 */ `<< /S /JavaScript /JS (var ` + tagJS + ` = 1;) >>`,
		/* 20 */ `<< /Type /StructTreeRoot /K [21 0 R] >>`,
		/* 21 */ `<< /Type /StructElem /S /Document /P 20 0 R /K [22 0 R 23 0 R 24 0 R 25 0 R] >>`,
		/* 22 */ `<< /Type /StructElem /S /Annot /P 21 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 9 0 R >> >>`,
		/* 23 */ `<< /Type /StructElem /S /Form /P 21 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 12 0 R >> >>`,
		/* 24 */ `<< /Type /StructElem /S /Form /P 21 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 27 0 R >> >>`,
		/* 25 */ `<< /Type /StructElem /S /Figure /P 21 0 R /AF [16 0 R 26 0 R]
   /GomupdfRefs [7 0 R 8 0 R 10 0 R 11 0 R 13 0 R 14 0 R 15 0 R 17 0 R 18 0 R 19 0 R 28 0 R 29 0 R 30 0 R 31 0 R] >>`,
		/* 26 */ `<< /Type /Filespec /F (` + tagAF + `) /EF << /F 31 0 R >> /AFRelationship /Source >>`,
		/* 27 */ `<< /Type /Annot /Subtype /Widget /Parent 14 0 R /Rect [20 120 200 140] /P 3 0 R /StructParent 3 >>`,
		/* 28 */ `<< /S /JavaScript /JS 29 0 R >>`,
		/* 29 */ "\nstream\nvar " + tagJSStream + " = 1;",
		/* 30 */ `<< /Title (` + tagInfo + `) >>`,
		/* 31 */ "/Type /EmbeddedFile\nstream\n" + tagAFBody,
	}, "/Info 30 0 R")
}

// Scrub destroys what it removes, so nothing survives through a reference
// from the structure tree, at every garbage level and from several goroutines.
func TestScrubTaggedPDF(t *testing.T) {
	pdf := scrubTaggedFixture()
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	// AssociatedFiles: the page's /AF and the StructElem 25 /AF.
	want := HiddenContent{
		Info: true, XMP: true, AcroForm: true, EmbeddedFiles: 1, AssociatedFiles: 2, JavaScript: true,
		NonLinkAnnotations: 2, Widgets: 2, Thumbnails: 1, PageMetadata: 1,
	}
	if got := scrubHidden(t, d); got != want {
		t.Fatalf("before scrub:\n got %+v\nwant %+v", got, want)
	}
	text := scrubText(t, d)
	control, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tagTokens {
		if !bytes.Contains(control, []byte(tok)) {
			t.Fatalf("token %q missing from the unscrubbed save", tok)
		}
	}

	var wg sync.WaitGroup
	for garbage := range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := OpenStream(pdf)
			if err != nil {
				t.Error(err)
				return
			}
			defer d.Close()
			// Load and render first, so MuPDF has touched the objects that
			// Scrub then destroys.
			p, _ := d.LoadPage(0)
			if _, err := p.Pixmap(); err != nil {
				t.Error(err)
			}
			if a, err := p.Annotations(); err != nil || len(a) == 0 {
				t.Errorf("annotations before scrub: %v, %v", a, err)
			}
			if err := d.Scrub(ScrubOptions{}); err != nil {
				t.Error(err)
				return
			}
			if _, err := p.Pixmap(); err != nil {
				t.Errorf("render after scrub: %v", err)
			}
			out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: garbage})
			if err != nil {
				t.Error(err)
				return
			}
			for _, tok := range tagTokens {
				if bytes.Contains(out, []byte(tok)) {
					t.Errorf("garbage %d: scrubbed save still contains %q", garbage, tok)
				}
			}
			re, err := OpenStream(out)
			if err != nil {
				t.Error(err)
				return
			}
			defer re.Close()
			if h, err := re.HiddenContent(); err != nil || h != (HiddenContent{}) {
				t.Errorf("garbage %d: after scrub %+v, %v", garbage, h, err)
			}
			p, _ = re.LoadPage(0)
			if s, err := p.GetText(); err != nil || s != text {
				t.Errorf("garbage %d: page text %q, %v; want %q", garbage, s, err, text)
			}
			if l, err := p.Links(); err != nil || len(l) != 1 {
				t.Errorf("garbage %d: links %+v, %v; want 1", garbage, l, err)
			}
		}()
	}
	wg.Wait()

	// KeepAnnotations destroys nothing annotation-related.
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{KeepAnnotations: true})
	for _, tok := range tagTokens {
		keep := slices.Contains(tagAnnotTokens, tok)
		if got := bytes.Contains(out, []byte(tok)); got != keep {
			t.Errorf("KeepAnnotations: %q in save = %v, want %v", tok, got, keep)
		}
	}
	p, _ := re.LoadPage(0)
	if w, err := p.Widgets(); err != nil || len(w) != 2 {
		t.Errorf("KeepAnnotations: widgets %+v, %v; want 2", w, err)
	}
}

func TestScrubErrors(t *testing.T) {
	img, err := OpenAnyStream(makePNG(t, 8, 8), "png")
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	if err := img.Scrub(ScrubOptions{}); err == nil || !strings.Contains(err.Error(), "not a PDF") {
		t.Errorf("Scrub on PNG: %v", err)
	}
	if _, err := img.HiddenContent(); err == nil || !strings.Contains(err.Error(), "not a PDF") {
		t.Errorf("HiddenContent on PNG: %v", err)
	}

	d, err := OpenStream(scrubFixture())
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	if err := d.Scrub(ScrubOptions{}); err == nil {
		t.Error("Scrub on closed document succeeded")
	}
	if _, err := d.HiddenContent(); err == nil {
		t.Error("HiddenContent on closed document succeeded")
	}
	if _, err := hiddenContentFromCounts(nil); err == nil {
		t.Error("hiddenContentFromCounts(nil) succeeded")
	}
}

// scrubKeptPDF is a one-page PDF whose page draws text in font 6, the form
// XObject 7 and the image 10 through the indirect /Resources 5 and the
// indirect /Contents array 9 holding the content stream 4. Object 8 is a
// hidden widget whose /Parent is the page's /Resources; a case puts it in
// /Annots. The form is in the optional-content group 11, which the
// configuration 12 turns off when a case lists it in the catalog
// /OCProperties, and drawn through the graphics state 13, a resource named
// /P. cat, pages and page are spliced into objects 1, 2 and 3 (cat and
// pages carry their own /Type, so a case can leave it out).
func scrubKeptPDF(cat, pages, page, trailer string) []byte {
	return scrubBuildPDF([]string{
		/* 1 */ `<< ` + cat + ` /Pages 2 0 R >>`,
		/* 2 */ `<< ` + pages + ` /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources 5 0 R /Contents 9 0 R ` + page + ` >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET q /P gs /Fm1 Do Q q 40 0 0 40 120 20 cm /Im1 Do Q",
		/* 5 */ `<< /Font << /F1 6 0 R >> /XObject << /Fm1 7 0 R /Im1 10 0 R >> /ExtGState << /P 13 0 R >> >>`,
		/* 6 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 7 */ "/Type /XObject /Subtype /Form /BBox [0 0 200 200] /OC 11 0 R\nstream\n0 0 1 rg 10 10 50 50 re f",
		/* 8 */ `<< /Type /Annot /Subtype /Widget /FT /Tx /T (w) /Rect [0 0 0 0] /F 2 /Parent 5 0 R >>`,
		/* 9 */ `[4 0 R]`,
		/* 10 */ "/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8\nstream\n\x00\xff\xff\x00",
		/* 11 */ `<< /Type /OCG /Name (layer) >>`,
		/* 12 */ `<< /OFF [11 0 R] >>`,
		/* 13 */ `<< /ca 0.5 >>`,
	}, trailer)
}

// scrubPage returns page 0's text and rendering.
func scrubPage(t *testing.T, d *Document) (string, *Pixmap) {
	t.Helper()
	p, err := d.LoadPage(0)
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
	return s, pm
}

// A hidden-content slot that points at something the page draws (its content
// stream or /Contents array, /Resources, form XObject, image, or the catalog
// or /Pages node themselves) is unlinked, never destroyed: the page renders
// exactly as before Scrub, in the same document and after a Garbage 1 save.
func TestScrubKeepsPageContent(t *testing.T) {
	const catT, pagesT = "/Type /Catalog", "/Type /Pages"
	efOf := func(n int) string { return fmt.Sprintf(` /AF [<< /Type /Filespec /EF << /F %d 0 R >> >>]`, n) }
	jsOf := func(n int) string {
		return fmt.Sprintf(` /Names << /JavaScript << /Names [(a) << /S /JavaScript /JS %d 0 R >>] >> >>`, n)
	}
	cases := []struct {
		name                      string
		cat, pages, page, trailer string
	}{
		{name: "names JS is the content stream", cat: catT + jsOf(4), pages: pagesT},
		{name: "catalog Metadata is the content stream", cat: catT + " /Metadata 4 0 R", pages: pagesT},
		{name: "page Metadata is the content stream", cat: catT, pages: pagesT, page: "/Metadata 4 0 R"},
		{name: "catalog AF embeds the content stream", cat: catT + efOf(4), pages: pagesT},
		{name: "page AF embeds the content stream", cat: catT, pages: pagesT, page: efOf(4)},
		{name: "Metadata is the form XObject", cat: catT + " /Metadata 7 0 R", pages: pagesT},
		{name: "JS is the form XObject", cat: catT + jsOf(7), pages: pagesT},
		{name: "AF embeds the image", cat: catT + efOf(10), pages: pagesT},
		{name: "Info is the resources", cat: catT, pages: pagesT, trailer: "/Info 5 0 R"},
		{name: "field is the resources", cat: catT + " /AcroForm << /Fields [5 0 R] >>", pages: pagesT},
		{name: "name tree leaf is the resources", cat: catT + " /Names << /EmbeddedFiles << /Kids [5 0 R] >> >>", pages: pagesT},
		{name: "widget parent is the resources", cat: catT, pages: pagesT, page: "/Annots [8 0 R]"},
		{name: "Info is the contents array", cat: catT, pages: pagesT, trailer: "/Info 9 0 R"},
		{name: "Info is the untyped catalog", pages: pagesT, trailer: "/Info 1 0 R"},
		{name: "Info is the untyped pages node", cat: catT, trailer: "/Info 2 0 R"},
		// The default optional-content configuration hides the form XObject.
		{name: "Info is the optional-content config", cat: catT + " /OCProperties << /OCGs [11 0 R] /D 12 0 R >>",
			pages: pagesT, trailer: "/Info 12 0 R"},
		// A resource name is not a dictionary key with a meaning: /P here is
		// an /ExtGState entry, not an annotation's page.
		{name: "Info is a graphics state named /P", cat: catT, pages: pagesT, trailer: "/Info 13 0 R"},
		// A /Thumb that the page also draws stays: it is unlinked as the
		// thumbnail, not destroyed.
		{name: "Thumb is the page image", cat: catT, pages: pagesT, page: "/Thumb 10 0 R"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pdf := scrubKeptPDF(c.cat, c.pages, c.page, c.trailer)
			ctl, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer ctl.Close()
			text, pix := scrubPage(t, ctl)
			if !strings.Contains(text, scrubPageText) {
				t.Fatalf("control text = %q", text)
			}
			same := func(where string, d *Document) {
				t.Helper()
				if n := d.PageCount(); n != 1 {
					t.Fatalf("%s: %d pages", where, n)
				}
				s, pm := scrubPage(t, d)
				if s != text {
					t.Errorf("%s: text %q, want %q", where, s, text)
				}
				if pm.Width != pix.Width || pm.Height != pix.Height || !bytes.Equal(pm.Samples, pix.Samples) {
					t.Errorf("%s: rendering differs from the unscrubbed control", where)
				}
			}

			d, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if err := d.Scrub(ScrubOptions{}); err != nil {
				t.Fatal(err)
			}
			same("scrubbed document", d)
			out, err := d.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			re, err := OpenStream(out)
			if err != nil {
				t.Fatal(err)
			}
			defer re.Close()
			same("saved", re)
			// Scrub leaves optional content alone; the /OFF group stays.
			want := HiddenContent{}
			if strings.Contains(c.cat, "/OCProperties") {
				want.Layers, want.HiddenLayers = 1, 1
			}
			if h := scrubHidden(t, re); h != want {
				t.Errorf("after scrub: %+v, want %+v", h, want)
			}
		})
	}
}

// scrubSharedNamesPDF is an /EmbeddedFiles name tree whose root (5) has kids
// references to node 6; both share the /Names array 7 of pairs entries, each
// pointing at the filespec 8 of a real embedded file (9).
func scrubSharedNamesPDF(kids, pairs int) []byte {
	return scrubMiniPDF(`/Names << /EmbeddedFiles 5 0 R >>`, "", "",
		/* 5 */ `<< /Kids [`+strings.Repeat("6 0 R ", kids)+`] /Names 7 0 R >>`,
		/* 6 */ `<< /Names 7 0 R >>`,
		/* 7 */ `[`+strings.Repeat("(a) 8 0 R ", pairs)+`]`,
		/* 8 */ `<< /Type /Filespec /F (a.txt) /EF << /F 9 0 R >> >>`,
		/* 9 */ "/Type /EmbeddedFile\nstream\nshared names body")
}

// The work at each name-tree node (its /Names pairs) counts against the walk
// budget, and the count saturates: 65,535 kids sharing one 65,536-pair
// /Names array is neither slow nor a wrapped-around zero.
func TestScrubNameTreeSharedNames(t *testing.T) {
	d, err := OpenStream(scrubSharedNamesPDF(65535, 65536))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	start := time.Now()
	h := scrubHidden(t, d)
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("HiddenContent took %v", el)
	}
	if h.EmbeddedFiles < 1 {
		t.Errorf("EmbeddedFiles = %d, want >= 1", h.EmbeddedFiles)
	}
}

// Scrub on a shared /Names array finishes quickly, without an object list
// that grows with the number of pairs walked.
func TestScrubNameTreeSharedNamesScrub(t *testing.T) {
	d, err := OpenStream(scrubSharedNamesPDF(65535, 1024))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if h := scrubHidden(t, d); h.EmbeddedFiles < 1 {
		t.Errorf("before scrub: EmbeddedFiles = %d", h.EmbeddedFiles)
	}
	start := time.Now()
	if err := d.Scrub(ScrubOptions{}); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("Scrub took %v", el)
	}
	if h := scrubHidden(t, d); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
}

// Many Link annotations sharing one /AA with many keys, each key a large safe
// action array: one action budget covers the whole HiddenContent call, so the
// walk is cut short (failing closed as JavaScript) instead of repeating per
// key and per annotation.
func TestScrubSharedAAActions(t *testing.T) {
	const links, keys, acts = 200, 200, 500
	var aa strings.Builder
	for i := range keys {
		fmt.Fprintf(&aa, "/K%d 6 0 R ", i)
	}
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [` + strings.Repeat("4 0 R ", links) + `] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /AA 5 0 R >>`,
		/* 5 */ `<< ` + aa.String() + `>>`,
		/* 6 */ `[` + strings.Repeat(`<< /S /GoTo /D [3 0 R /Fit] >> `, acts) + `]`,
	}, "")
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	start := time.Now()
	h := scrubHidden(t, d)
	if el := time.Since(start); el > time.Second {
		t.Errorf("HiddenContent took %v", el)
	}
	if !h.JavaScript {
		t.Error("JavaScript = false; an action walk cut short must fail closed")
	}
	if err := d.Scrub(ScrubOptions{}); err != nil {
		t.Fatal(err)
	}
	if h := scrubHidden(t, d); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
}

// A removed FileAttachment annotation takes its /FS filespec and its /AF
// filespecs with it, even when a StructElem /AF still points at them.
func TestScrubFileAttachmentFS(t *testing.T) {
	const fsBody, afBody = "GomupdfFSBody5a1c", "GomupdfAnnotAFBody7b3e"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 5 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 10 10] /FS 7 0 R /AF [9 0 R] /StructParent 0 >>`,
		/* 5 */ `<< /Type /StructTreeRoot /K [6 0 R] >>`,
		/* 6 */ `<< /Type /StructElem /S /Annot /P 5 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 4 0 R >> /AF [7 0 R 9 0 R] >>`,
		/* 7 */ `<< /Type /Filespec /F (fs.txt) /EF << /F 8 0 R >> >>`,
		/* 8 */ "/Type /EmbeddedFile\nstream\n" + fsBody,
		/* 9 */ `<< /Type /Filespec /F (af.txt) /EF << /F 10 0 R >> >>`,
		/* 10 */ "/Type /EmbeddedFile\nstream\n" + afBody,
	}, "")
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	control, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
	for _, tok := range []string{fsBody, afBody} {
		if !bytes.Contains(control, []byte(tok)) {
			t.Fatalf("%q missing from the unscrubbed save", tok)
		}
		if bytes.Contains(out, []byte(tok)) {
			t.Errorf("scrubbed save still contains %q", tok)
		}
	}
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
}

// Scrub destroys an annotation created since the document was opened (a
// Circle added through the API) and the file still saves: MuPDF 1.25.6 marks
// such an object, once deleted, as never having existed, and every later
// save then fails on it ("cannot find object in xref"), so Scrub replaces it
// with an empty dictionary instead. Run on MuPDF 1.25 to see the failure;
// 1.26 tolerates it.
func TestScrubDestroysCreatedObjects(t *testing.T) {
	d, err := OpenStream(scrubMiniPDF("", "", ""))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.AddCircle(geometry.Rect{X0: 60, Y0: 60, X1: 120, Y1: 120}, AnnotStyle{}); err != nil {
		t.Fatal(err)
	}
	if h := scrubHidden(t, d); h.NonLinkAnnotations != 1 {
		t.Fatalf("before scrub: %+v, want the Circle", h)
	}
	if err := d.Scrub(ScrubOptions{}); err != nil {
		t.Fatal(err)
	}
	out, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatalf("save after scrub: %v", err)
	}
	if regexp.MustCompile(`/Subtype\s*/Circle`).Match(out) {
		t.Error("the Circle is still in the saved file")
	}
	re, err := OpenStream(out)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
}

// A Scrub that fails partway (here on a /Kids entry that is not a page)
// returns an error and still destroys what it had already unlinked, so that
// does not survive through a structure-tree reference.
func TestScrubFailureStillDestroys(t *testing.T) {
	const annotTok, infoTok = "GomupdfFailAnnot2d6f", "GomupdfFailInfo8c4a"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R 99 0 R] /Count 2 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /Square /Rect [0 0 50 50] /Contents (` + annotTok + `) >>`,
		/* 5 */ `<< /Title (` + infoTok + `) >>`,
		/* 6 */ `<< /Type /StructTreeRoot /K [7 0 R] >>`,
		/* 7 */ `<< /Type /StructElem /S /Annot /P 6 0 R /K << /Type /OBJR /Obj 4 0 R >> /GomupdfRefs [5 0 R] >>`,
	}, "/Info 5 0 R")
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Scrub(ScrubOptions{}); err == nil {
		t.Fatal("Scrub succeeded on a page tree with a missing page")
	}
	out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 4})
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{annotTok, infoTok} {
		if !bytes.Contains(pdf, []byte(tok)) {
			t.Fatalf("%q missing from the fixture", tok)
		}
		if bytes.Contains(out, []byte(tok)) {
			t.Errorf("save after the failed scrub still contains %q", tok)
		}
	}
}

// Annotations that stay are kept content: /Info pointing at a Link's action,
// or (with KeepAnnotations) at /AcroForm, only unlinks /Info.
func TestScrubKeepsAnnotationRoots(t *testing.T) {
	_, re := scrubRoundTrip(t, scrubBuildPDF([]string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>`,
		`<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A 5 0 R >>`,
		`<< /S /URI /URI (https://example.com/) >>`,
	}, "/Info 5 0 R"), ScrubOptions{})
	p, _ := re.LoadPage(0)
	if l, err := p.Links(); err != nil || len(l) != 1 || l[0].URI != "https://example.com/" {
		t.Errorf("links = %+v, %v; want the URI link", l, err)
	}

	_, re = scrubRoundTrip(t, scrubBuildPDF([]string{
		`<< /Type /Catalog /Pages 2 0 R /AcroForm 5 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>`,
		`<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /Rect [0 0 50 20] /P 3 0 R >>`,
		`<< /Fields [4 0 R] >>`,
	}, "/Info 5 0 R"), ScrubOptions{KeepAnnotations: true})
	if h := scrubHidden(t, re); h != (HiddenContent{AcroForm: true, Widgets: 1}) {
		t.Errorf("KeepAnnotations: %+v, want AcroForm and one widget", h)
	}
}

// A removed slot destroys only an object recognisably of its kind: catalog
// /Metadata that is a plain stream, or /Info that is a typed dictionary, is
// unlinked and survives through another reference.
func TestScrubKindGuard(t *testing.T) {
	const streamTok, dictTok = "GomupdfPlainStream4c2e", "GomupdfTypedDict9a7b"
	out, _ := scrubRoundTrip(t, scrubBuildPDF([]string{
		`<< /Type /Catalog /Pages 2 0 R /Metadata 4 0 R /GomupdfRefs [4 0 R 5 0 R] >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`,
		"\nstream\n" + streamTok,
		`<< /Type /GomupdfThing /Name (` + dictTok + `) >>`,
	}, "/Info 5 0 R"), ScrubOptions{})
	for _, tok := range []string{streamTok, dictTok} {
		if !bytes.Contains(out, []byte(tok)) {
			t.Errorf("%q destroyed although it is not of the removed kind", tok)
		}
	}
}

// scrubRoles describes scrubRolePDF: a one-page PDF whose page draws text in
// the font 9, a square under the graphics state 10 and the image 11, through
// the resources 5 (res, "5 0 R" if empty) and its indirect subdictionaries:
// /Font 6, /ExtGState 7 (where the state is named gs, "P" if empty) and
// /XObject 8. cat, pages, page and gsSub are spliced into objects 1, 2, 3 and
// 7, trailer into the trailer; extra objects are numbered from 12. Scrub runs
// with opts.
type scrubRoles struct {
	gs, res, cat, pages, page, gsSub, trailer string
	extra                                     []string
	opts                                      ScrubOptions
}

func scrubRolePDF(r scrubRoles) []byte {
	if r.gs == "" {
		r.gs = "P"
	}
	if r.res == "" {
		r.res = "5 0 R"
	}
	return scrubBuildPDF(append([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R ` + r.cat + ` >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 ` + r.pages + ` >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources ` + r.res + ` /Contents 4 0 R ` + r.page + ` >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET q /" + r.gs +
			" gs 1 0 0 rg 10 10 80 80 re f Q q 40 0 0 40 120 20 cm /Im1 Do Q",
		/* 5 */ `<< /Font 6 0 R /ExtGState 7 0 R /XObject 8 0 R >>`,
		/* 6 */ `<< /F1 9 0 R >>`,
		/* 7 */ `<< /` + r.gs + ` 10 0 R ` + r.gsSub + ` >>`,
		/* 8 */ `<< /Im1 11 0 R >>`,
		/* 9 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>`,
		/* 10 */ `<< /ca 0.5 >>`,
		/* 11 */ "/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8\nstream\n\x00\xff\xff\x00",
	}, r.extra...), r.trailer)
}

// scrubCheckKept scrubs pdf with opts and checks that the page count, and
// page 0's text and rendering, match an unscrubbed control, in the scrubbed
// document and after a Garbage 1 save and reopen. It returns the reopened
// document.
func scrubCheckKept(t *testing.T, pdf []byte, opts ScrubOptions) *Document {
	t.Helper()
	ctl, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(ctl.Close)
	text, pix := scrubPage(t, ctl)
	if !strings.Contains(text, scrubPageText) {
		t.Fatalf("control text = %q", text)
	}
	pages := ctl.PageCount()
	same := func(where string, d *Document) {
		t.Helper()
		if n := d.PageCount(); n != pages {
			t.Fatalf("%s: %d pages, want %d", where, n, pages)
		}
		s, pm := scrubPage(t, d)
		if s != text {
			t.Errorf("%s: text %q, want %q", where, s, text)
		}
		if pm.Width != pix.Width || pm.Height != pix.Height || !bytes.Equal(pm.Samples, pix.Samples) {
			t.Errorf("%s: rendering differs from the unscrubbed control", where)
		}
	}
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Close)
	if err := d.Scrub(opts); err != nil {
		t.Fatal(err)
	}
	same("scrubbed document", d)
	out, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	re, err := OpenStream(out)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(re.Close)
	same("saved", re)
	return re
}

// An object that is kept content and also has another role (a page-tree node
// through /Kids or /Parent, a page or the catalog, a kept Link annotation or
// outline item) is still walked as content: a removed slot pointing at
// something it leads to only unlinks that. Each /Info, /Thumb or kept-role
// key below names an object the page draws only through such an object.
func TestScrubKeepsContentWithOtherRoles(t *testing.T) {
	const link = `<< /Subtype /Link /Rect [0 0 50 50] /A 13 0 R >>`
	const uri = `<< /S /URI /URI (https://example.com/) >>`
	cases := []struct {
		name string
		r    scrubRoles
		link bool // page 0 keeps the URI link
	}{
		{name: "resources is a page kid", r: scrubRoles{page: "/Kids [5 0 R]", trailer: "/Info 6 0 R"}},
		{name: "drawn image is the thumb, through a page kid", r: scrubRoles{page: "/Kids [5 0 R] /Thumb 11 0 R"}},
		{name: "resources is the pages parent", r: scrubRoles{pages: "/Parent 5 0 R", trailer: "/Info 6 0 R"}},
		// In a resource subdictionary every key is a resource name, even one
		// Scrub removes from pages.
		{name: "graphics state named /Thumb, through a page kid",
			r: scrubRoles{gs: "Thumb", page: "/Kids [7 0 R]", trailer: "/Info 10 0 R"}},
		{name: "resources is the page", r: scrubRoles{res: "3 0 R",
			page: "/Font 6 0 R /ExtGState 7 0 R /XObject 8 0 R", trailer: "/Info 6 0 R"}},
		{name: "resources is the catalog", r: scrubRoles{res: "1 0 R",
			cat: "/Font 6 0 R /ExtGState 7 0 R /XObject 8 0 R", trailer: "/Info 6 0 R"}},
		{name: "Link is a page kid", link: true, r: scrubRoles{page: "/Kids [12 0 R] /Annots [12 0 R]",
			trailer: "/Info 13 0 R", extra: []string{link, uri}}},
		// /P is an annotation's page, but here also a graphics state name.
		{name: "Link is the ExtGState subdictionary",
			r: scrubRoles{gsSub: "/Subtype /Link /Rect [0 0 0 0]", page: "/Annots [7 0 R]", trailer: "/Info 10 0 R"}},
		{name: "outline item is the ExtGState subdictionary", r: scrubRoles{
			cat: "/Outlines 12 0 R", gsSub: "/Title (x) /Parent 12 0 R", trailer: "/Info 10 0 R",
			extra: []string{`<< /Type /Outlines /First 7 0 R /Last 7 0 R /Count 1 >>`}, opts: ScrubOptions{KeepOutlines: true}}},
		// Scrub edits these in place (a removed annotation loses /Popup, a
		// kept one /AA, the /Names dictionary its trees): kept content that is
		// the same object keeps the key.
		{name: "removed annotation is the ExtGState subdictionary, graphics state named /Popup",
			r: scrubRoles{gs: "Popup", gsSub: "/Subtype /Square /Rect [0 0 10 10]", page: "/Annots [7 0 R]"}},
		{name: "Link is the ExtGState subdictionary, graphics state named /AA",
			r: scrubRoles{gs: "AA", gsSub: "/Subtype /Link /Rect [0 0 0 0]", page: "/Annots [7 0 R]"}},
		{name: "Names is the ExtGState subdictionary, graphics state named /JavaScript",
			r: scrubRoles{gs: "JavaScript", cat: "/Names 7 0 R"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			re := scrubCheckKept(t, scrubRolePDF(c.r), c.r.opts)
			if !c.link {
				return
			}
			p, err := re.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			if l, err := p.Links(); err != nil || len(l) != 1 || l[0].URI != "https://example.com/" {
				t.Errorf("links after scrub = %+v, %v; want the URI link", l, err)
			}
		})
	}
}

// An xref entry that points at the wrong object makes MuPDF repair the xref
// when it first loads that object, here the /Info that only Scrub's removal
// reads. Scrub loads every object before marking, so the repair happens
// before it changes anything: Scrub succeeds and destroys what it removes,
// although the structure tree still references it.
func TestScrubRepairsBeforeMarking(t *testing.T) {
	const annotTok, infoTok = "GomupdfRepairAnnot5e2b", "GomupdfRepairInfo3f7d"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 6 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /Square /Rect [0 0 50 50] /Contents (` + annotTok + `) /StructParent 0 >>`,
		/* 5 */ `<< /Title (` + infoTok + `) >>`,
		/* 6 */ `<< /Type /StructTreeRoot /K [7 0 R] >>`,
		/* 7 */ `<< /Type /StructElem /S /Annot /P 6 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 4 0 R >> /GomupdfRefs [5 0 R] >>`,
	}, "/Info 5 0 R")
	// Point the xref entry of object 5 at object 1. Each entry is 20 bytes,
	// after the "xref" and "0 <size>" lines.
	x := bytes.LastIndex(pdf, []byte("\nxref\n")) + len("\nxref\n")
	x += bytes.IndexByte(pdf[x:], '\n') + 1
	copy(pdf[x+20*5:x+20*6], pdf[x+20*1:x+20*2])

	// With the page open, rendered and its annotations listed first, as a
	// redaction does, the repair must not leave Scrub working on a stale page.
	for _, open := range []bool{false, true} {
		t.Run(fmt.Sprintf("page open %v", open), func(t *testing.T) {
			d, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if open {
				p, err := d.LoadPage(0)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := p.Pixmap(); err != nil {
					t.Fatal(err)
				}
				if a, err := p.Annotations(); err != nil || len(a) != 1 {
					t.Fatalf("annotations before scrub: %v, %v", a, err)
				}
			}
			if err := d.Scrub(ScrubOptions{}); err != nil {
				t.Fatal(err)
			}
			out, err := d.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			for _, tok := range []string{annotTok, infoTok} {
				if bytes.Contains(out, []byte(tok)) {
					t.Errorf("scrubbed save still contains %q", tok)
				}
			}
			re, err := OpenStream(out)
			if err != nil {
				t.Fatal(err)
			}
			defer re.Close()
			if h := scrubHidden(t, re); h != (HiddenContent{}) {
				t.Errorf("after scrub: %+v", h)
			}
		})
	}
}

// A page whose /Annots is its /Contents array: the /Annots sweep must not
// take the content stream out of /Contents.
func TestScrubAnnotsArrayIsContents(t *testing.T) {
	scrubCheckKept(t, scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 6 0 R >> >>
   /Contents 5 0 R /Annots 5 0 R >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
		/* 5 */ `[4 0 R]`,
		/* 6 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>`,
	}, ""), ScrubOptions{})
}

// Two pages sharing one indirect /Annots array with a Link and a Square both
// end with only the Link, its URI intact.
func TestScrubSharedAnnotsArray(t *testing.T) {
	const page = `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots 5 0 R >>`
	_, re := scrubRoundTrip(t, scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>`,
		/* 3 */ page,
		/* 4 */ page,
		/* 5 */ `[6 0 R 7 0 R]`,
		/* 6 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A << /S /URI /URI (https://example.com/) >> >>`,
		/* 7 */ `<< /Type /Annot /Subtype /Square /Rect [60 60 100 100] >>`,
	}, ""), ScrubOptions{})
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	for i := range 2 {
		p, err := re.LoadPage(i)
		if err != nil {
			t.Fatal(err)
		}
		if l, err := p.Links(); err != nil || len(l) != 1 || l[0].URI != "https://example.com/" {
			t.Errorf("page %d: links %+v, %v; want the URI link", i, l, err)
		}
		if a, err := p.Annotations(); err != nil || len(a) != 0 {
			t.Errorf("page %d: annotations %+v, %v; want none", i, a, err)
		}
	}
}

// An indirect array that is both a stray /Kids of one page and the /Annots
// of another is expanded in both roles: the Link in it stays a kept
// annotation, so its action survives a removed /Info pointing at it.
func TestScrubKidsArrayIsAlsoAnnots(t *testing.T) {
	re := scrubCheckKept(t, scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R 8 0 R] /Count 2 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 9 0 R >> >>
   /Contents 4 0 R /Annots 7 0 R >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
		/* 5 */ `<< /S /URI /URI (https://example.com/) >>`,
		/* 6 */ `<< /Subtype /Link /Rect [0 0 50 50] /A 5 0 R >>`,
		/* 7 */ `[6 0 R]`,
		/* 8 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Kids 7 0 R >>`,
		/* 9 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Courier >>`,
	}, "/Info 5 0 R"), ScrubOptions{})
	p, err := re.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if l, err := p.Links(); err != nil || len(l) != 1 || l[0].URI != "https://example.com/" {
		t.Errorf("links after scrub = %+v, %v; want the URI link", l, err)
	}
}

// Once Scrub has failed on a document, it fails on every later call too,
// with an error saying so, instead of reporting a scrub it may not have done.
func TestScrubFailureIsSticky(t *testing.T) {
	d, err := OpenStream(scrubBuildPDF([]string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R 99 0 R] /Count 2 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`,
	}, ""))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Scrub(ScrubOptions{}); err == nil {
		t.Fatal("Scrub succeeded on a page tree with a missing page")
	}
	if err := d.Scrub(ScrubOptions{}); err == nil || !strings.Contains(err.Error(), "earlier Scrub failed") {
		t.Errorf("second Scrub: %v, want the earlier failure", err)
	}
}

// Tokens of scrubFormPDF, each in exactly one object.
const (
	formJS     = "GomupdfFormKeystrokeJS5b2e"
	formValue  = "GomupdfFormMergedValue8d1c"
	formParent = "GomupdfFormParentNote3e7a"
	formReply  = "GomupdfFormReplyNote9f4b"
)

// scrubFormPDF is a well-formed one-page form: /AcroForm /Fields lists the
// merged text field and widget 5, which has a keystroke JavaScript /AA; the
// page lists it with the Text annotation 6 and the reply 7 (/IRT 6).
func scrubFormPDF() []byte {
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /DA (/Helv 0 Tf 0 g) >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 8 0 R >> >>
   /Contents 4 0 R /Annots [5 0 R 6 0 R 7 0 R] >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 100 Td (" + scrubPageText + ") Tj ET",
		/* 5 */ `<< /Type /Annot /Subtype /Widget /FT /Tx /T (merged) /V (` + formValue + `) /Rect [20 150 200 170] /P 3 0 R
   /AA << /K << /S /JavaScript /JS (var ` + formJS + ` = 1;) >> >> >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Text /Rect [20 20 40 40] /Contents (` + formParent + `) /P 3 0 R >>`,
		/* 7 */ `<< /Type /Annot /Subtype /Text /Rect [60 20 80 40] /Contents (` + formReply + `) /IRT 6 0 R /P 3 0 R >>`,
		/* 8 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
	}, "")
}

var (
	scrubAnnotsRe = regexp.MustCompile(`/Annots\s*\[([^\]]*)\]`)
	scrubOnlyRefs = regexp.MustCompile(`^\s*(\d+\s+\d+\s+R\s*)*$`)
)

// scrubCheckAnnotsIndirect checks that the saved file has exactly one /Annots
// array and that every entry of it is an indirect reference.
func scrubCheckAnnotsIndirect(t *testing.T, where string, out []byte) {
	t.Helper()
	m := scrubAnnotsRe.FindAllSubmatch(out, -1)
	if len(m) != 1 {
		t.Errorf("%s: %d /Annots arrays, want 1", where, len(m))
		return
	}
	if !scrubOnlyRefs.Match(m[0][1]) {
		t.Errorf("%s: /Annots holds direct entries: %q", where, m[0][1])
	}
}

// With KeepAnnotations, the widget that /AcroForm /Fields also lists and the
// note a reply's /IRT points at stay the objects the page lists: Scrub edits
// them in place (the widget loses its /AA everywhere) instead of giving the
// page private copies, which would leave the script in the original, split
// the field from its widget and orphan the /IRT.
func TestScrubKeepAnnotationsForm(t *testing.T) {
	pdf := scrubFormPDF()
	for _, tok := range []string{formJS, formValue, formParent, formReply} {
		if n := bytes.Count(pdf, []byte(tok)); n != 1 {
			t.Fatalf("fixture holds %q %d times", tok, n)
		}
	}
	for _, garbage := range []int{0, 1, 3} {
		t.Run(fmt.Sprintf("garbage %d", garbage), func(t *testing.T) {
			d, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if !scrubHidden(t, d).JavaScript {
				t.Fatal("JavaScript not detected before scrub")
			}
			if err := d.Scrub(ScrubOptions{KeepAnnotations: true}); err != nil {
				t.Fatal(err)
			}
			out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: garbage})
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(out, []byte(formJS)) {
				t.Error("keystroke JavaScript survived")
			}
			for _, tok := range []string{formValue, formParent, formReply} {
				if n := bytes.Count(out, []byte(tok)); n != 1 {
					t.Errorf("%q appears %d times, want 1", tok, n)
				}
			}
			scrubCheckAnnotsIndirect(t, "scrubbed", out)

			re, err := OpenStream(out)
			if err != nil {
				t.Fatal(err)
			}
			defer re.Close()
			if h, want := scrubHidden(t, re), (HiddenContent{AcroForm: true, NonLinkAnnotations: 2, Widgets: 1}); h != want {
				t.Errorf("after scrub: %+v, want %+v", h, want)
			}
			p, err := re.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			if a, err := p.Annotations(); err != nil || len(a) != 2 {
				t.Errorf("annotations %+v, %v; want 2", a, err)
			}
			w, err := p.Widgets()
			if err != nil || len(w) != 1 || w[0].Value != formValue {
				t.Fatalf("widgets %+v, %v; want the one field", w, err)
			}
			// The page's widget is the /Fields entry: setting it leaves no
			// other copy of the old value.
			const newValue = "GomupdfFormNewValue2c6d"
			if err := p.SetTextField(0, newValue); err != nil {
				t.Fatal(err)
			}
			set, err := re.SaveBytesWithOptions(SaveOptions{Garbage: 1})
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(set, []byte(formValue)) {
				t.Error("old field value survives SetTextField")
			}
			if n := len(regexp.MustCompile(`/V\s*\(`+newValue+`\)`).FindAll(set, -1)); n != 1 {
				t.Errorf("new field value set %d times, want 1", n)
			}

			// Rendering and listing the scrubbed document's annotations, then
			// saving, keeps every appearance.
			sp, err := d.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := sp.Pixmap(); err != nil {
				t.Fatal(err)
			}
			if _, err := sp.Annotations(); err != nil {
				t.Fatal(err)
			}
			rendered, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
			if err != nil {
				t.Fatal(err)
			}
			if regexp.MustCompile(`/N\s*null`).Match(rendered) {
				t.Error("an appearance became /N null")
			}
			scrubCheckAnnotsIndirect(t, "rendered", rendered)
		})
	}
}

// A Link that stays and is also page content (the /ExtGState subdictionary,
// with a graphics state named /AA) is replaced in /Annots by a copy that
// loses /AA, as a new indirect object: the page draws as before, the URI
// survives, and /Annots holds only indirect references.
func TestScrubKeptAnnotationIsContent(t *testing.T) {
	for _, opts := range []ScrubOptions{{}, {KeepAnnotations: true}} {
		t.Run(fmt.Sprintf("KeepAnnotations %v", opts.KeepAnnotations), func(t *testing.T) {
			re := scrubCheckKept(t, scrubRolePDF(scrubRoles{gs: "AA", page: "/Annots [7 0 R]",
				gsSub: "/Subtype /Link /Rect [0 0 50 50] /A 12 0 R",
				extra: []string{`<< /S /URI /URI (https://example.com/) >>`}}), opts)
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
			out, err := re.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			scrubCheckAnnotsIndirect(t, "saved", out)
		})
	}

	// The same for a Square that stays under KeepAnnotations, with its page
	// already open, rendered and its annotations listed before Scrub.
	pdf := scrubRolePDF(scrubRoles{gs: "AA", page: "/Annots [7 0 R]", gsSub: "/Subtype /Square /Rect [0 0 10 10]"})
	ctl, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer ctl.Close()
	text, pix := scrubPage(t, ctl)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Pixmap(); err != nil {
		t.Fatal(err)
	}
	if a, err := p.Annotations(); err != nil || len(a) != 1 {
		t.Fatalf("annotations before scrub: %+v, %v", a, err)
	}
	if err := d.Scrub(ScrubOptions{KeepAnnotations: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Pixmap(); err != nil {
		t.Fatal(err)
	}
	if a, err := p.Annotations(); err != nil || len(a) != 1 {
		t.Errorf("annotations after scrub: %+v, %v; want 1", a, err)
	}
	out, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	scrubCheckAnnotsIndirect(t, "Square", out)
	if regexp.MustCompile(`/N\s*null`).Match(out) {
		t.Error("Square: an appearance became /N null")
	}
	re, err := OpenStream(out)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	if s, pm := scrubPage(t, re); s != text || !bytes.Equal(pm.Samples, pix.Samples) {
		t.Errorf("Square: page text %q or rendering differs from the control", s)
	}
	if h := scrubHidden(t, re); h != (HiddenContent{NonLinkAnnotations: 1}) {
		t.Errorf("Square: after scrub %+v, want one annotation", h)
	}
}

// The same form scrubbed with default options loses the widget, both notes
// and /AcroForm.
func TestScrubRemovesForm(t *testing.T) {
	out, re := scrubRoundTrip(t, scrubFormPDF(), ScrubOptions{})
	for _, tok := range []string{formJS, formValue, formParent, formReply} {
		if bytes.Contains(out, []byte(tok)) {
			t.Errorf("scrubbed save still contains %q", tok)
		}
	}
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	p, _ := re.LoadPage(0)
	if a, err := p.Annotations(); err != nil || len(a) != 0 {
		t.Errorf("annotations %+v, %v; want none", a, err)
	}
	if w, err := p.Widgets(); err != nil || len(w) != 0 {
		t.Errorf("widgets %+v, %v; want none", w, err)
	}
}

// scrubControl saves pdf with scrubSaveOpts without scrubbing it and checks
// that each token is in that save, so a token's absence from a scrubbed save
// is due to Scrub.
func scrubControl(t *testing.T, pdf []byte, tokens ...string) {
	t.Helper()
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	out, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if !bytes.Contains(out, []byte(tok)) {
			t.Fatalf("token %q missing from the unscrubbed save", tok)
		}
	}
}

// scrubNoTokens fails for every token found in out.
func scrubNoTokens(t *testing.T, where string, out []byte, tokens ...string) {
	t.Helper()
	for _, tok := range tokens {
		if bytes.Contains(out, []byte(tok)) {
			t.Errorf("%s: scrubbed save still contains %q", where, tok)
		}
	}
}

var scrubLinkRe = regexp.MustCompile(`/Subtype\s*/Link\b`)

// A Link whose /A is a JavaScript action, or chains through /Next to a
// Launch action, stays after Scrub but loses that /A and becomes inert (MuPDF
// lists only Links with a target); a plain URI Link is untouched.
func TestScrubLinkActions(t *testing.T) {
	const jsTok, launchTok = "GomupdfLinkJS5c9e", "GomupdfLinkLaunch2a7f.exe"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [4 0 R 5 0 R 6 0 R] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A << /S /JavaScript /JS (var ` + jsTok + ` = 1;) >> >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Link /Rect [60 0 110 50]
   /A << /S /URI /URI (https://example.org/next) /Next << /S /Launch /F (` + launchTok + `) >> >> >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Link /Rect [120 0 170 50] /A << /S /URI /URI (https://example.com/) >> >>`,
	}, "")
	scrubControl(t, pdf, jsTok, launchTok)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !scrubHidden(t, d).JavaScript {
		t.Error("Link JavaScript and Launch actions not reported before scrub")
	}
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
	scrubNoTokens(t, "default", out, jsTok, launchTok)
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	if n := len(scrubLinkRe.FindAll(out, -1)); n != 3 {
		t.Errorf("%d Link annotations after scrub, want 3", n)
	}
	scrubCheckAnnotsIndirect(t, "scrubbed", out)
	p, _ := re.LoadPage(0)
	if l, err := p.Links(); err != nil || len(l) != 1 || l[0].URI != "https://example.com/" {
		t.Errorf("links after scrub = %+v, %v; want only the plain URI link", l, err)
	}
}

// With KeepOutlines, an outline item's JavaScript or Launch /A goes; the
// items, their titles and a /Dest stay.
func TestScrubKeepOutlinesActions(t *testing.T) {
	const jsTok, launchTok = "GomupdfOutlineJS7d3b", "GomupdfOutlineLaunch4e1c.exe"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`,
		/* 4 */ `<< /Type /Outlines /First 5 0 R /Last 6 0 R /Count 3 >>`,
		/* 5 */ `<< /Title (Script item) /Parent 4 0 R /Next 6 0 R /First 7 0 R /Last 7 0 R /Count 1
   /A << /S /JavaScript /JS (var ` + jsTok + ` = 1;) >> >>`,
		/* 6 */ `<< /Title (Dest item) /Parent 4 0 R /Prev 5 0 R /Dest [3 0 R /Fit] >>`,
		/* 7 */ `<< /Title (Launch child) /Parent 5 0 R /A << /S /GoTo /D [3 0 R /Fit] /Next << /S /Launch /F (` + launchTok + `) >> >> >>`,
	}, "")
	scrubControl(t, pdf, jsTok, launchTok)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !scrubHidden(t, d).JavaScript {
		t.Error("outline JavaScript not reported before scrub")
	}
	toc, err := d.TOC()
	if err != nil || len(toc) != 3 {
		t.Fatalf("control TOC = %+v, %v", toc, err)
	}
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{KeepOutlines: true})
	scrubNoTokens(t, "KeepOutlines", out, jsTok, launchTok)
	if h := scrubHidden(t, re); h != (HiddenContent{Outlines: true}) {
		t.Errorf("after scrub: %+v, want only Outlines", h)
	}
	got, err := re.TOC()
	if err != nil || len(got) != len(toc) {
		t.Fatalf("TOC after scrub = %+v, %v; control %+v", got, err, toc)
	}
	for i := range got {
		if got[i].Level != toc[i].Level || got[i].Title != toc[i].Title {
			t.Errorf("TOC[%d] = %+v, want %+v", i, got[i], toc[i])
		}
	}
	if got[2].Page != 0 || toc[2].Page != 0 {
		t.Errorf("the /Dest item's page: control %d, scrubbed %d; want 0", toc[2].Page, got[2].Page)
	}
}

// scrubFieldActionsPDF is a form whose non-widget parent field 5 (in /Fields
// and /CO, the array fields) has a calculate JavaScript /AA and whose kid
// widget 6 has a JavaScript /A, both actions indirect and also referenced
// from the structure tree; the page also has a Text annotation.
func scrubFieldActionsPDF(calcTok, actTok, fields string) []byte {
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields ` + fields + ` /CO ` + fields + ` /DA (/Helv 0 Tf 0 g) >>
   /MarkInfo << /Marked true >> /StructTreeRoot 10 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [6 0 R 7 0 R] >>`,
		/* 4 */ `<< /S /JavaScript /JS (var ` + actTok + ` = 1;) >>`,
		/* 5 */ `<< /FT /Tx /T (parent) /V (` + formValue + `) /Kids [6 0 R] /AA << /C 8 0 R >> >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Widget /Parent 5 0 R /Rect [20 150 200 170] /P 3 0 R /A 4 0 R >>`,
		/* 7 */ `<< /Type /Annot /Subtype /Text /Rect [20 20 40 40] /Contents (` + formParent + `) /P 3 0 R >>`,
		/* 8 */ `<< /S /JavaScript /JS 9 0 R >>`,
		/* 9 */ "\nstream\nvar " + calcTok + " = 1;",
		/* 10 */ `<< /Type /StructTreeRoot /K [11 0 R] >>`,
		/* 11 */ `<< /Type /StructElem /S /Form /P 10 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 6 0 R >> /GomupdfRefs [4 0 R 8 0 R 9 0 R] >>`,
	}, "")
}

// Under KeepAnnotations every field-tree node loses /AA and its JavaScript /A,
// and those actions are destroyed although the structure tree references
// them; the field, its widget and the annotation counts stay as they were.
// A field left out of /AcroForm /Fields is reached up its widget's /Parent.
// KeepFormFields does the same, but removes the Text annotation.
func TestScrubKeepAnnotationsFieldActions(t *testing.T) {
	for _, opts := range []ScrubOptions{{KeepAnnotations: true}, {KeepFormFields: true}} {
		for _, fields := range []string{"[5 0 R]", "[]"} {
			t.Run(fmt.Sprintf("%+v Fields %s", opts, fields), func(t *testing.T) { scrubFieldActionsCase(t, opts, fields) })
		}
	}
}

func scrubFieldActionsCase(t *testing.T, opts ScrubOptions, fields string) {
	notes := 0
	if opts.KeepAnnotations {
		notes = 1
	}
	const calcTok, actTok = "GomupdfFieldCalcJS6f2a", "GomupdfWidgetActJS3b8d"
	pdf := scrubFieldActionsPDF(calcTok, actTok, fields)
	scrubControl(t, pdf, calcTok, actTok)
	// The calculate script alone, on a field only the widget's /Parent reaches,
	// is reported too.
	only := scrubFieldActionsPDF(calcTok, actTok, fields)
	only = bytes.Replace(only, []byte("/A 4 0 R"), []byte("        "), 1)
	if h, err := func() (HiddenContent, error) {
		d, err := OpenStream(only)
		if err != nil {
			return HiddenContent{}, err
		}
		defer d.Close()
		return d.HiddenContent()
	}(); err != nil || !h.JavaScript {
		t.Errorf("field /AA alone: %+v, %v; want JavaScript", h, err)
	}
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	before := scrubHidden(t, d)
	if !before.JavaScript || before.Widgets != 1 || before.NonLinkAnnotations != 1 {
		t.Fatalf("before scrub: %+v", before)
	}
	out, re := scrubRoundTrip(t, pdf, opts)
	scrubNoTokens(t, "scrubbed", out, calcTok, actTok)
	scrubCheckAnnotsIndirect(t, "scrubbed", out)
	if h, want := scrubHidden(t, re), (HiddenContent{AcroForm: true, NonLinkAnnotations: notes, Widgets: 1}); h != want {
		t.Errorf("after scrub: %+v, want %+v", h, want)
	}
	p, err := re.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if a, err := p.Annotations(); err != nil || len(a) != notes {
		t.Errorf("annotations %+v, %v; want %d", a, err, notes)
	}
	w, err := p.Widgets()
	if err != nil || len(w) != 1 || w[0].Value != formValue {
		t.Fatalf("widgets %+v, %v; want the one field", w, err)
	}
	const newValue = "GomupdfFieldNewValue8e5a"
	if err := p.SetTextField(0, newValue); err != nil {
		t.Fatal(err)
	}
	set, err := re.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(set, []byte(formValue)) {
		t.Error("old field value survives SetTextField")
	}
	if n := len(regexp.MustCompile(`/V\s*\(`+newValue+`\)`).FindAll(set, -1)); n != 1 {
		t.Errorf("new field value set %d times, want 1", n)
	}
}

// A Link /A or /PA JavaScript or Launch action that the structure tree also
// references is destroyed, with its script stream and Launch filespec; the
// Link's URI /A stays.
func TestScrubLinkActionInStructTree(t *testing.T) {
	const jsTok, launchTok = "GomupdfStructLinkJS1e4b", "GomupdfStructLaunch9c2d.exe"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 9 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R 5 0 R] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A 6 0 R /StructParent 0 >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Link /Rect [60 0 110 50] /A << /S /URI /URI (https://example.com/) >> /PA 8 0 R >>`,
		/* 6 */ `<< /Type /Action /S /JavaScript /JS 7 0 R >>`,
		/* 7 */ "\nstream\nvar " + jsTok + " = 1;",
		/* 8 */ `<< /S /Launch /F 11 0 R >>`,
		/* 9 */ `<< /Type /StructTreeRoot /K [10 0 R] >>`,
		/* 10 */ `<< /Type /StructElem /S /Link /P 9 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 4 0 R >> /GomupdfRefs [6 0 R 7 0 R 8 0 R 11 0 R] >>`,
		/* 11 */ `<< /Type /Filespec /F (` + launchTok + `) >>`,
	}, "")
	scrubControl(t, pdf, jsTok, launchTok)
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
	scrubNoTokens(t, "default", out, jsTok, launchTok)
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	p, _ := re.LoadPage(0)
	if l, err := p.Links(); err != nil || len(l) != 1 || l[0].URI != "https://example.com/" {
		t.Errorf("links after scrub = %+v, %v; want the URI link", l, err)
	}
}

// A Link's JavaScript /A that is also page content (the /ExtGState
// subdictionary, with a graphics state named /JS) is removed from the Link but
// not destroyed: the page draws as before.
func TestScrubLinkActionIsContent(t *testing.T) {
	re := scrubCheckKept(t, scrubRolePDF(scrubRoles{gs: "JS", gsSub: "/S /JavaScript", page: "/Annots [12 0 R]",
		extra: []string{`<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A 7 0 R >>`}}), ScrubOptions{})
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
}

// The keys Scrub removes from pages are removed from, and counted on,
// intermediate page-tree nodes too, and their streams are destroyed although
// the structure tree references them and a kept Link reaches the nodes
// through its destination page.
func TestScrubPageTreeNodes(t *testing.T) {
	const metaTok, thumbTok = "GomupdfNodeMeta3d9a", "GomupdfNodeThumb6b1f"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 10 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 /AA << /O << /S /JavaScript /JS (1) >> >> >>`,
		/* 3 */ `<< /Type /Pages /Parent 2 0 R /Kids [4 0 R] /Count 1 /Metadata 6 0 R /Thumb 7 0 R /AF [8 0 R]
   /PieceInfo << /GoMuPDF << /Private (node piece) >> >> /Resources << /Font << /F1 9 0 R >> >> >>`,
		/* 4 */ `<< /Type /Page /Parent 3 0 R /MediaBox [0 0 200 200] /Contents 5 0 R /Annots [12 0 R] >>`,
		/* 5 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
		/* 6 */ "/Type /Metadata /Subtype /XML\nstream\n<x:xmpmeta xmlns:x=\"adobe:ns:meta/\">" + metaTok + "</x:xmpmeta>",
		/* 7 */ fmt.Sprintf("/Width %d /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8\nstream\n%s", len(thumbTok), thumbTok),
		/* 8 */ `<< /Type /Filespec /F (node-af.txt) >>`,
		/* 9 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 10 */ `<< /Type /StructTreeRoot /K [11 0 R] >>`,
		/* 11 */ `<< /Type /StructElem /S /Figure /P 10 0 R /GomupdfRefs [6 0 R 7 0 R] >>`,
		/* 12 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /Dest [4 0 R /Fit] >>`,
	}, "")
	scrubControl(t, pdf, metaTok, thumbTok)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	want := HiddenContent{JavaScript: true, AssociatedFiles: 1, Thumbnails: 1, PageMetadata: 1, PieceInfo: 1}
	if got := scrubHidden(t, d); got != want {
		t.Errorf("before scrub:\n got %+v\nwant %+v", got, want)
	}
	re := scrubCheckKept(t, pdf, ScrubOptions{})
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	out, err := re.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	scrubNoTokens(t, "default", out, metaTok, thumbTok)
}

// A large well-formed file (70 pages of 1,000 Links, each with its own GoTo
// action, and an outline item with a GoTo action) is not reported as
// JavaScript, and Scrub with KeepOutlines keeps every Link target and the
// outline destination.
func TestScrubManyLinks(t *testing.T) {
	const pages, perPage = 70, 1000
	// 1 catalog, 2 pages, 3 outlines, 4 outline item, then each page followed
	// by its Links.
	objs := []string{`<< /Type /Catalog /Pages 2 0 R /Outlines 3 0 R >>`, "",
		`<< /Type /Outlines /First 4 0 R /Last 4 0 R /Count 1 >>`, ""}
	var kids []int
	last := 0
	for range pages {
		pn := len(objs) + 1
		kids, last = append(kids, pn), pn
		refs := make([]int, perPage)
		for i := range refs {
			refs[i] = pn + 1 + i
		}
		objs = append(objs, `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [`+scrubRefs(refs...)+`] >>`)
		link := fmt.Sprintf(`<< /Subtype /Link /Rect [0 0 1 1] /A << /S /GoTo /D [%d 0 R /Fit] >> >>`, pn)
		for range perPage {
			objs = append(objs, link)
		}
	}
	objs[1] = fmt.Sprintf(`<< /Type /Pages /Kids [%s] /Count %d >>`, scrubRefs(kids...), pages)
	objs[3] = fmt.Sprintf(`<< /Title (Last page) /Parent 3 0 R /A << /S /GoTo /D [%d 0 R /Fit] >> >>`, last)
	pdf := scrubBuildPDF(objs, "")

	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if h := scrubHidden(t, d); h != (HiddenContent{Outlines: true}) {
		t.Errorf("before scrub: %+v, want only Outlines", h)
	}
	_, re := scrubRoundTrip(t, pdf, ScrubOptions{KeepOutlines: true})
	if h := scrubHidden(t, re); h != (HiddenContent{Outlines: true}) {
		t.Errorf("after scrub: %+v, want only Outlines", h)
	}
	for _, p := range []int{0, pages - 1} {
		pg, err := re.LoadPage(p)
		if err != nil {
			t.Fatal(err)
		}
		if l, err := pg.Links(); err != nil || len(l) != perPage {
			t.Errorf("page %d: %d links after scrub, %v; want %d", p, len(l), err, perPage)
		}
	}
	if toc, err := re.TOC(); err != nil || len(toc) != 1 || toc[0].Page != pages-1 {
		t.Errorf("TOC after scrub = %+v, %v; want the last page", toc, err)
	}
}

// scrubSharedLinkPDF is a one-page PDF whose /Annots lists one Link, with a
// direct URI action, n times: 5 xref entries.
func scrubSharedLinkPDF(n int) []byte {
	return scrubBuildPDF([]string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [` + strings.Repeat("4 0 R ", n) + `] >>`,
		`<< /Subtype /Link /Rect [0 0 1 1] /A << /S /URI /URI (https://example.com/) >> >>`,
	}, "")
}

// Pins a documented limit: one HiddenContent call, and each Scrub pass, may
// reach actions 4,096 + 32 x (xref length) times. One Link listed 4,256 times
// on a page of a 5-entry xref is within it; listed once more it reports
// JavaScript, and Scrub removes its action, failing closed.
func TestScrubActionBudgetCeiling(t *testing.T) {
	const budget = 4096 + 32*5
	d, err := OpenStream(scrubSharedLinkPDF(budget))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if scrubHidden(t, d).JavaScript {
		t.Errorf("%d reaches reported as JavaScript", budget)
	}
	_, re := scrubRoundTrip(t, scrubSharedLinkPDF(budget), ScrubOptions{})
	p, _ := re.LoadPage(0)
	if l, err := p.Links(); err != nil || len(l) != budget {
		t.Errorf("within the budget: %d links after scrub, %v; want %d", len(l), err, budget)
	}

	pdf := scrubSharedLinkPDF(budget + 1)
	d, err = OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !scrubHidden(t, d).JavaScript {
		t.Errorf("%d reaches not reported as JavaScript", budget+1)
	}
	_, re = scrubRoundTrip(t, pdf, ScrubOptions{})
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	p, _ = re.LoadPage(0)
	if l, err := p.Links(); err != nil || len(l) != 0 {
		t.Errorf("past the budget: %d links after scrub, %v; want none", len(l), err)
	}
}

// A script that is the direct /Next of an indirect GoTo action G does not
// survive when Scrub removes G from a Link, an outline item or a widget
// although the structure tree also references G: every action dictionary of
// a removed chain is destroyed.
func TestScrubChainActionsDestroyed(t *testing.T) {
	const linkTok, itemTok, fieldTok = "GomupdfChainLink5b1e", "GomupdfChainItem8d3a", "GomupdfChainField2f7c"
	goTo := func(tok string) string {
		return `<< /S /GoTo /D [3 0 R /Fit] /Next << /S /JavaScript /JS (var ` + tok + ` = 1;) >> >>`
	}
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R /AcroForm << /Fields [6 0 R] >>
   /MarkInfo << /Marked true >> /StructTreeRoot 10 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [5 0 R 6 0 R] >>`,
		/* 4 */ `<< /Type /Outlines /First 12 0 R /Last 12 0 R /Count 1 >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A 7 0 R >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Widget /FT /Btn /Ff 65536 /T (b) /Rect [60 0 110 50] /P 3 0 R /A 8 0 R >>`,
		/* 7 */ goTo(linkTok),
		/* 8 */ goTo(fieldTok),
		/* 9 */ goTo(itemTok),
		/* 10 */ `<< /Type /StructTreeRoot /K [11 0 R] >>`,
		/* 11 */ `<< /Type /StructElem /S /Figure /P 10 0 R /Refs [7 0 R 8 0 R 9 0 R] >>`,
		/* 12 */ `<< /Title (Item) /Parent 4 0 R /A 9 0 R >>`,
	}, "")
	scrubControl(t, pdf, linkTok, itemTok, fieldTok)
	opts := ScrubOptions{KeepOutlines: true, KeepFormFields: true}
	out, re := scrubRoundTrip(t, pdf, opts)
	scrubNoTokens(t, "scrubbed", out, linkTok, itemTok, fieldTok)
	if h, want := scrubHidden(t, re), (HiddenContent{AcroForm: true, Outlines: true, Widgets: 1}); h != want {
		t.Errorf("after scrub: %+v, want %+v", h, want)
	}
}

// A javascript: URI is caught however it is encoded: UTF-16BE, UTF-16LE or
// UTF-8 with a byte-order mark, or with a tab or line break inside the
// scheme. A URI that merely contains "javascript:" is not.
func TestScrubScriptURIEncodings(t *testing.T) {
	hexOf := func(b []byte) string { return fmt.Sprintf("<%X>", b) }
	utf16 := func(s string, be bool) []byte {
		b := []byte{0xFE, 0xFF}
		if !be {
			b = []byte{0xFF, 0xFE}
		}
		for _, c := range []byte(s) {
			if be {
				b = append(b, 0, c)
			} else {
				b = append(b, c, 0)
			}
		}
		return b
	}
	const js = "javascript:void0"
	cases := []struct {
		name, uri string
		script    bool
	}{
		{"UTF-16BE BOM", hexOf(utf16(js, true)), true},
		{"UTF-16LE BOM", hexOf(utf16(js, false)), true},
		{"UTF-8 BOM", hexOf(append([]byte{0xEF, 0xBB, 0xBF}, js...)), true},
		{"tab in scheme", `(java\tscript:void0)`, true},
		{"newline in scheme", `(java\nscript:void0)`, true},
		{"carriage return in scheme", `(\r java\r\nscript:void0)`, true},
		{"https", `(https://example.com/javascript:)`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pdf := scrubBuildPDF([]string{
				`<< /Type /Catalog /Pages 2 0 R >>`,
				`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
				`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>`,
				`<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A << /S /URI /URI ` + c.uri + ` >> >>`,
			}, "")
			d, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if got := scrubHidden(t, d).JavaScript; got != c.script {
				t.Errorf("JavaScript before scrub = %v, want %v", got, c.script)
			}
			out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
			if h := scrubHidden(t, re); h != (HiddenContent{}) {
				t.Errorf("after scrub: %+v", h)
			}
			if got := bytes.Contains(out, []byte("/URI")); got == c.script {
				t.Errorf("/URI in the scrubbed save = %v, want %v", got, !c.script)
			}
			p, _ := re.LoadPage(0)
			if l, _ := p.Links(); c.script && len(l) != 0 {
				t.Errorf("links after scrub = %+v, want none", l)
			}
		})
	}
}

// Tokens of scrubFormsFixture, each in exactly one object.
const (
	ffMerged = "GomupdfFFMergedValue4a1e"
	ffPair   = "GomupdfFFPairValue7c3d"
	ffJS     = "GomupdfFFKeystrokeJS2b9f"
	ffNote   = "GomupdfFFNote5e8a"
	ffPopup  = "GomupdfFFPopup1d6c"
	ffReply  = "GomupdfFFReply9f2b"
	ffSquare = "GomupdfFFSquare3a7e"
)

var ffFieldTokens, ffCommentTokens = []string{ffMerged, ffPair}, []string{ffNote, ffPopup, ffReply, ffSquare}

// ffAllTokens returns every token of scrubFormsFixture.
func ffAllTokens() []string {
	return slices.Concat([]string{ffJS}, ffFieldTokens, ffCommentTokens)
}

// scrubFormsFixture is a tagged two-page form: the merged text field and
// widget 5 (with a keystroke JavaScript /AA), the field 6 with kid widgets 7
// (page 1) and 8 (page 2), a Text note 9 with its Popup 10 and reply 11
// (/IRT), a Square 12 and a Link 13, every annotation also referenced from
// the structure tree through /OBJR.
func scrubFormsFixture() []byte {
	var objr strings.Builder
	for _, n := range []int{5, 7, 8, 9, 10, 11, 12, 13} {
		fmt.Fprintf(&objr, "<< /Type /OBJR /Obj %d 0 R >> ", n)
	}
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R 6 0 R] /DA (/Helv 0 Tf 0 g) >>
   /MarkInfo << /Marked true >> /StructTreeRoot 14 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R 4 0 R] /Count 2 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [5 0 R 7 0 R 9 0 R 10 0 R 11 0 R 12 0 R 13 0 R] >>`,
		/* 4 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [8 0 R] >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Widget /FT /Tx /T (merged) /V (` + ffMerged + `) /Rect [20 150 200 170] /P 3 0 R
   /AA << /K << /S /JavaScript /JS (var ` + ffJS + ` = 1;) >> >> >>`,
		/* 6 */ `<< /FT /Tx /T (pair) /V (` + ffPair + `) /Kids [7 0 R 8 0 R] >>`,
		/* 7 */ `<< /Type /Annot /Subtype /Widget /Parent 6 0 R /Rect [20 120 200 140] /P 3 0 R >>`,
		/* 8 */ `<< /Type /Annot /Subtype /Widget /Parent 6 0 R /Rect [20 120 200 140] /P 4 0 R >>`,
		/* 9 */ `<< /Type /Annot /Subtype /Text /Rect [20 20 40 40] /Contents (` + ffNote + `) /Popup 10 0 R /P 3 0 R >>`,
		/* 10 */ `<< /Type /Annot /Subtype /Popup /Rect [40 40 140 90] /Parent 9 0 R /Contents (` + ffPopup + `) >>`,
		/* 11 */ `<< /Type /Annot /Subtype /Text /Rect [60 20 80 40] /Contents (` + ffReply + `) /IRT 9 0 R /P 3 0 R >>`,
		/* 12 */ `<< /Type /Annot /Subtype /Square /Rect [150 20 250 60] /Contents (` + ffSquare + `) /P 3 0 R >>`,
		/* 13 */ `<< /Type /Annot /Subtype /Link /Rect [100 60 140 80] /A << /S /URI /URI (https://example.com/) >> >>`,
		/* 14 */ `<< /Type /StructTreeRoot /K [15 0 R] >>`,
		/* 15 */ `<< /Type /StructElem /S /Document /P 14 0 R /Pg 3 0 R /K [` + objr.String() + `] >>`,
	}, "")
}

// scrubCheckAllAnnotsIndirect checks that the saved file has n /Annots arrays,
// each of indirect references only.
func scrubCheckAllAnnotsIndirect(t *testing.T, where string, out []byte, n int) {
	t.Helper()
	m := scrubAnnotsRe.FindAllSubmatch(out, -1)
	if len(m) != n {
		t.Errorf("%s: %d /Annots arrays, want %d", where, len(m), n)
	}
	for _, a := range m {
		if !scrubOnlyRefs.Match(a[1]) {
			t.Errorf("%s: /Annots holds direct entries: %q", where, a[1])
		}
	}
}

// scrubWidgetValues returns the values of every widget on every page.
func scrubWidgetValues(t *testing.T, d *Document) []string {
	t.Helper()
	var v []string
	for i := range d.PageCount() {
		p, err := d.LoadPage(i)
		if err != nil {
			t.Fatal(err)
		}
		w, err := p.Widgets()
		if err != nil {
			t.Fatal(err)
		}
		for _, x := range w {
			v = append(v, x.Value)
		}
	}
	return v
}

// KeepFormFields keeps the widgets and the field tree (minus their scripts)
// and removes every other non-Link annotation, popups and replies included,
// even though the structure tree references them.
func TestScrubKeepFormFields(t *testing.T) {
	pdf := scrubFormsFixture()
	for _, tok := range ffAllTokens() {
		if n := bytes.Count(pdf, []byte(tok)); n != 1 {
			t.Fatalf("fixture holds %q %d times", tok, n)
		}
	}
	scrubControl(t, pdf, ffAllTokens()...)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.Scrub(ScrubOptions{KeepFormFields: true}); err != nil {
		t.Fatal(err)
	}
	out, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	scrubNoTokens(t, "KeepFormFields", out, append([]string{ffJS}, ffCommentTokens...)...)
	for _, tok := range ffFieldTokens {
		if n := bytes.Count(out, []byte(tok)); n != 1 {
			t.Errorf("%q appears %d times, want 1", tok, n)
		}
	}
	scrubCheckAllAnnotsIndirect(t, "KeepFormFields", out, 2)
	re, err := OpenStream(out)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	if h, want := scrubHidden(t, re), (HiddenContent{AcroForm: true, Widgets: 3}); h != want {
		t.Errorf("after scrub: %+v, want %+v", h, want)
	}
	if got, want := scrubWidgetValues(t, re), []string{ffMerged, ffPair, ffPair}; !slices.Equal(got, want) {
		t.Errorf("widget values %q, want %q", got, want)
	}
	p, err := re.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if a, err := p.Annotations(); err != nil || len(a) != 0 {
		t.Errorf("annotations %+v, %v; want none", a, err)
	}
	if l, err := p.Links(); err != nil || len(l) != 1 || l[0].URI != "https://example.com/" {
		t.Errorf("links %+v, %v; want the URI link", l, err)
	}
	// The page's widget is the /Fields entry: setting it leaves no other copy
	// of the old value.
	const newValue = "GomupdfFFNewValue6d4c"
	if err := p.SetTextField(0, newValue); err != nil {
		t.Fatal(err)
	}
	set, err := re.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(set, []byte(ffMerged)) {
		t.Error("old field value survives SetTextField")
	}
	if n := len(regexp.MustCompile(`/V\s*\(`+newValue+`\)`).FindAll(set, -1)); n != 1 {
		t.Errorf("new field value set %d times, want 1", n)
	}

	// Rendering and listing the scrubbed document's annotations, then saving,
	// keeps every appearance and every /Annots entry indirect.
	for i := range d.PageCount() {
		sp, err := d.LoadPage(i)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Pixmap(); err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Annotations(); err != nil {
			t.Fatal(err)
		}
		if _, err := sp.Widgets(); err != nil {
			t.Fatal(err)
		}
	}
	rendered, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	if regexp.MustCompile(`/N\s*null`).Match(rendered) {
		t.Error("an appearance became /N null")
	}
	scrubCheckAllAnnotsIndirect(t, "rendered", rendered, 2)
}

// The same fixture loses every annotation, widget and field with the default
// options, and keeps them all (minus the script) with KeepAnnotations,
// whatever KeepFormFields says.
func TestScrubFormsFixtureOtherOptions(t *testing.T) {
	pdf := scrubFormsFixture()
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
	scrubNoTokens(t, "default", out, ffAllTokens()...)
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("default: after scrub %+v", h)
	}
	for _, opts := range []ScrubOptions{{KeepAnnotations: true}, {KeepAnnotations: true, KeepFormFields: true}} {
		where := fmt.Sprintf("%+v", opts)
		out, re := scrubRoundTrip(t, pdf, opts)
		scrubNoTokens(t, where, out, ffJS)
		for _, tok := range slices.Concat(ffFieldTokens, ffCommentTokens) {
			if n := bytes.Count(out, []byte(tok)); n != 1 {
				t.Errorf("%s: %q appears %d times, want 1", where, tok, n)
			}
		}
		if h, want := scrubHidden(t, re), (HiddenContent{AcroForm: true, NonLinkAnnotations: 4, Widgets: 3}); h != want {
			t.Errorf("%s: after scrub %+v, want %+v", where, h, want)
		}
		scrubCheckAllAnnotsIndirect(t, where, out, 2)
	}
}

// KeepFormFields with a cyclic or very deep field tree and a Square to remove
// neither crashes (MuPDF's annotation removal must not walk /Fields) nor
// keeps the Square; the field-tree walk fails closed as JavaScript.
func TestScrubKeepFormFieldsTreeLimits(t *testing.T) {
	fieldPDF := func(widget int, fields ...string) []byte {
		return scrubBuildPDF(append([]string{
			`<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> >>`,
			`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
			`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R ` + scrubRefs(widget) + `] >>`,
			`<< /Type /Annot /Subtype /Square /Rect [100 100 150 150] >>`,
		}, fields...), "")
	}
	const n = 1<<16 + 10
	var chain []string
	for i := range n - 1 {
		chain = append(chain, `<< /T (f) /Kids [`+scrubRefs(i+6)+`] >>`)
	}
	w := 5 + n - 1
	chain = append(chain, fmt.Sprintf(`<< /Type /Annot /Subtype /Widget /FT /Tx /Parent %d 0 R /Rect [0 0 50 20] /V (x) >>`, w-1))
	cases := map[string][]byte{
		"cycle": fieldPDF(6, `<< /T (f) /Kids [5 0 R 6 0 R] >>`,
			`<< /Type /Annot /Subtype /Widget /FT /Tx /Parent 5 0 R /Rect [0 0 50 20] /V (x) >>`),
		"deep": fieldPDF(w, chain...),
	}
	for name, pdf := range cases {
		t.Run(name, func(t *testing.T) {
			scrubConcurrently(func() {
				d, err := OpenStream(pdf)
				if err != nil {
					t.Error(err)
					return
				}
				defer d.Close()
				if err := d.Scrub(ScrubOptions{KeepFormFields: true}); err != nil {
					t.Error(err)
				}
				want := HiddenContent{AcroForm: true, Widgets: 1, JavaScript: true}
				if h, err := d.HiddenContent(); err != nil || h != want {
					t.Errorf("after scrub: %+v, %v; want %+v", h, err, want)
				}
			})
		})
	}
}

// Tokens of scrubXFAPDF: the script is in the XFA template stream 6, the value
// in the datasets stream 7.
const (
	xfaScript = "GomupdfXFAScript3e9b"
	xfaValue  = "GomupdfXFAValue8c2d"
)

// scrubXFAPDF is a tagged one-page form whose merged text field and widget 5
// holds formValue, with /AcroForm /XFA set to xfa (6 0 R is the template
// stream, 7 0 R the datasets stream) and a catalog /NeedsRendering; the
// structure tree references refs.
func scrubXFAPDF(xfa, refs string) []byte {
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] /XFA ` + xfa + ` /DA (/Helv 0 Tf 0 g) >>
   /NeedsRendering true /MarkInfo << /Marked true >> /StructTreeRoot 4 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [5 0 R] >>`,
		/* 4 */ `<< /Type /StructTreeRoot /K [8 0 R] >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Widget /FT /Tx /T (merged) /V (` + formValue + `) /Rect [20 150 200 170] /P 3 0 R >>`,
		/* 6 */ "\nstream\n<template><field><event><script>var " + xfaScript + " = 1;</script></event></field></template>",
		/* 7 */ "\nstream\n<xfa:datasets><xfa:data><merged>" + xfaValue + "</merged></xfa:data></xfa:datasets>",
		/* 8 */ `<< /Type /StructElem /S /Form /P 4 0 R /Pg 3 0 R /K << /Type /OBJR /Obj 5 0 R >> /GomupdfRefs [` + refs + `] >>`,
	}, "")
}

// Scrub removes /AcroForm /XFA and destroys its streams whatever the options,
// although the structure tree references them; with KeepFormFields the
// fields stay listed and fillable.
func TestScrubXFA(t *testing.T) {
	cases := []struct {
		name, xfa, refs string
		tokens          []string
	}{
		{"pairs", "[(template) 6 0 R (datasets) 7 0 R]", "6 0 R 7 0 R", []string{xfaScript, xfaValue}},
		{"stream", "6 0 R", "6 0 R", []string{xfaScript}},
	}
	for _, c := range cases {
		pdf := scrubXFAPDF(c.xfa, c.refs)
		scrubControl(t, pdf, append([]string{"NeedsRendering"}, c.tokens...)...)
		d, err := OpenStream(pdf)
		if err != nil {
			t.Fatal(err)
		}
		if h := scrubHidden(t, d); !h.XFA {
			t.Errorf("%s: XFA not reported before scrub: %+v", c.name, h)
		}
		d.Close()
		for _, opts := range []ScrubOptions{{}, {KeepFormFields: true}, {KeepAnnotations: true}} {
			where := fmt.Sprintf("%s %+v", c.name, opts)
			out, re := scrubRoundTrip(t, pdf, opts)
			scrubNoTokens(t, where, out, append([]string{"NeedsRendering"}, c.tokens...)...)
			h := scrubHidden(t, re)
			if h.XFA {
				t.Errorf("%s: XFA after scrub", where)
			}
			if opts == (ScrubOptions{}) {
				continue
			}
			if want := (HiddenContent{AcroForm: true, Widgets: 1}); h != want {
				t.Errorf("%s: after scrub %+v, want %+v", where, h, want)
			}
			p, err := re.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			if w, err := p.Widgets(); err != nil || len(w) != 1 || w[0].Value != formValue {
				t.Fatalf("%s: widgets %+v, %v; want the one field", where, w, err)
			}
			const newValue = "GomupdfXFANewValue5a7e"
			if err := p.SetTextField(0, newValue); err != nil {
				t.Fatal(err)
			}
			set, err := re.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(set, []byte(formValue)) || !bytes.Contains(set, []byte(newValue)) {
				t.Errorf("%s: SetTextField did not replace the value", where)
			}
		}
	}
}

// A javascript: URI (any case, after leading whitespace and control
// characters) and an action carrying /JS whatever its /S are unsafe: their
// Links are made inert. A URI that merely contains "javascript:" stays.
func TestScrubScriptLikeActions(t *testing.T) {
	const uriTok, rendTok = "GomupdfJSURI6d1a", "GomupdfRenditionJS2f8c"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [4 0 R 5 0 R 6 0 R] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A << /S /URI /URI ( \t\001JaVaScRiPt:alert\('` + uriTok + `'\)) >> >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Link /Rect [60 0 110 50] /A << /S /Rendition /OP 0 /JS (var ` + rendTok + ` = 1;) >> >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Link /Rect [120 0 170 50] /A << /S /URI /URI (https://example.com/javascript:) >> >>`,
	}, "")
	scrubControl(t, pdf, uriTok, rendTok)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !scrubHidden(t, d).JavaScript {
		t.Error("javascript: URI and /JS action not reported before scrub")
	}
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
	scrubNoTokens(t, "default", out, uriTok, rendTok)
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	if n := len(scrubLinkRe.FindAll(out, -1)); n != 3 {
		t.Errorf("%d Link annotations after scrub, want 3", n)
	}
	p, _ := re.LoadPage(0)
	if l, err := p.Links(); err != nil || len(l) != 1 || l[0].URI != "https://example.com/javascript:" {
		t.Errorf("links after scrub = %+v, %v; want only the https link", l, err)
	}
}

// A removed Launch action takes its indirect /Win and /Mac dictionaries and
// their /F file specifications with it, and a removed /Names/JavaScript
// action the unsafe actions of its /Next chain, although the structure tree
// references them.
func TestScrubLaunchPlatformsAndNamesNext(t *testing.T) {
	const winTok, macTok, nextTok = "GomupdfLaunchWin7b3e", "GomupdfLaunchMac4c9a", "GomupdfNamesNextJS1d5f"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 11 0 R
   /Names << /JavaScript << /Names [(a) << /S /JavaScript /JS (1) /Next 9 0 R >>] >> >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A 5 0 R >>`,
		/* 5 */ `<< /S /Launch /Win 6 0 R /Mac 7 0 R >>`,
		/* 6 */ `<< /F (` + winTok + `.exe) /P (/c) >>`,
		/* 7 */ `<< /F 8 0 R >>`,
		/* 8 */ `<< /Type /Filespec /F (` + macTok + `.app) >>`,
		/* 9 */ `<< /S /JavaScript /JS 10 0 R >>`,
		/* 10 */ "\nstream\nvar " + nextTok + " = 1;",
		/* 11 */ `<< /Type /StructTreeRoot /K [12 0 R] >>`,
		/* 12 */ `<< /Type /StructElem /S /Figure /P 11 0 R /GomupdfRefs [5 0 R 6 0 R 7 0 R 8 0 R 9 0 R 10 0 R] >>`,
	}, "")
	scrubControl(t, pdf, winTok, macTok, nextTok)
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
	scrubNoTokens(t, "default", out, winTok, macTok, nextTok)
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
}

// scrubAppendUpdate appends to pdf, a file scrubBuildPDF wrote with size
// xref entries, an incremental update that rewrites object num as body, with
// trailerExtra in its trailer.
func scrubAppendUpdate(pdf []byte, size, num int, body, trailerExtra string) []byte {
	i := bytes.LastIndex(pdf, []byte("startxref\n")) + len("startxref\n")
	var prev int
	fmt.Sscanf(string(pdf[i:]), "%d", &prev)
	b := bytes.NewBuffer(append([]byte(nil), pdf...))
	off := b.Len()
	fmt.Fprintf(b, "%d 0 obj\n%s\nendobj\n", num, body)
	x := b.Len()
	fmt.Fprintf(b, "xref\n0 1\n0000000000 65535 f \n%d 1\n%010d 00000 n \ntrailer\n<< /Size %d /Root 1 0 R /Prev %d %s >>\nstartxref\n%d\n%%%%EOF\n",
		num, off, size, prev, trailerExtra, x)
	return b.Bytes()
}

// scrubXRefStreamPDF is scrubBuildPDF with a cross-reference stream (object
// len(objs)+1, its dictionary holding trailerExtra) instead of an xref table.
func scrubXRefStreamPDF(objs []string, trailerExtra string) []byte {
	pdf := scrubBuildPDF(objs, "")
	pdf = pdf[:bytes.LastIndex(pdf, []byte("\nxref\n"))+1]
	var offs []int
	for i := range objs {
		offs = append(offs, bytes.Index(pdf, []byte(fmt.Sprintf("\n%d 0 obj\n", i+1)))+1)
	}
	n := len(objs) + 1
	offs = append(offs, len(pdf))
	var rows bytes.Buffer
	rows.Write([]byte{0, 0, 0, 0, 0, 0xff, 0xff})
	for _, o := range offs {
		rows.Write([]byte{1, byte(o >> 24), byte(o >> 16), byte(o >> 8), byte(o), 0, 0})
	}
	b := bytes.NewBuffer(pdf)
	fmt.Fprintf(b, "%d 0 obj\n<< /Type /XRef /Size %d /W [1 4 2] /Root 1 0 R %s /Length %d >>\nstream\n", n, n+1, trailerExtra, rows.Len())
	b.Write(rows.Bytes())
	fmt.Fprintf(b, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", offs[n-1])
	return b.Bytes()
}

// /Info goes from a file whose trailer is a cross-reference stream (where
// MuPDF's first edit to it starts a new xref section with a copy of the
// trailer) and from one with an incremental update, and an /Info typed
// /Type /Info is destroyed although the structure tree references it.
// (Reduced from pdf.js issue11131_reduced.pdf.)
func TestScrubInfoTrailerCopies(t *testing.T) {
	const tok = "GomupdfIncrInfo4d2b"
	page := `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`
	objs := []string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 5 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ page,
		/* 4 */ `<< /Type /Info /Title (` + tok + `) >>`,
		/* 5 */ `<< /Type /StructTreeRoot /K [6 0 R] >>`,
		/* 6 */ `<< /Type /StructElem /S /Document /P 5 0 R /GomupdfRefs [4 0 R] >>`,
	}
	cases := map[string][]byte{
		"xref stream":        scrubXRefStreamPDF(objs, "/Info 4 0 R"),
		"incremental update": scrubAppendUpdate(scrubBuildPDF(objs, "/Info 4 0 R"), 7, 3, page, "/Info 4 0 R"),
	}
	for name, pdf := range cases {
		t.Run(name, func(t *testing.T) {
			scrubControl(t, pdf, tok)
			d, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if h := scrubHidden(t, d); !h.Info {
				t.Fatalf("Info not reported before scrub: %+v", h)
			}
			if err := d.Scrub(ScrubOptions{}); err != nil {
				t.Fatal(err)
			}
			if h := scrubHidden(t, d); h != (HiddenContent{}) {
				t.Errorf("in memory after scrub: %+v", h)
			}
			out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
			scrubNoTokens(t, "default", out, tok)
			if h := scrubHidden(t, re); h != (HiddenContent{}) {
				t.Errorf("after scrub: %+v", h)
			}
		})
	}
}

// A field tree in which every node lists its one kid twice (2^24 paths, 25
// distinct nodes) is walked once per node: a script on a node deep in it is
// found and removed, and without one the form reads clean, with no
// fail-closed JavaScript from an exhausted walk. (Reduced from qpdf
// shared-unnamed-field.pdf.)
func TestScrubSharedFieldKids(t *testing.T) {
	const depth = 24
	const tok = "GomupdfSharedFieldJS8a3c"
	tree := func(script bool) []byte {
		objs := []string{
			`<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>`,
			`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
			fmt.Sprintf(`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [%d 0 R] >>`, 4+depth),
		}
		for i := range depth {
			aa := ""
			if script && i == depth-4 {
				aa = ` /AA << /C << /S /JavaScript /JS (var ` + tok + ` = 1;) >> >>`
			}
			parent := ""
			if i > 0 {
				parent = fmt.Sprintf(" /Parent %d 0 R", 3+i)
			}
			objs = append(objs, fmt.Sprintf(`<< /Kids [%d 0 R %d 0 R]%s%s >>`, 5+i, 5+i, parent, aa))
		}
		objs = append(objs, fmt.Sprintf(`<< /Type /Annot /Subtype /Widget /FT /Tx /T (leaf) /Rect [0 0 50 20] /Parent %d 0 R >>`, 3+depth))
		return scrubBuildPDF(objs, "")
	}
	clean := HiddenContent{AcroForm: true, Widgets: 1}
	for _, script := range []bool{false, true} {
		t.Run(fmt.Sprintf("script %v", script), func(t *testing.T) {
			pdf := tree(script)
			d, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			want := clean
			want.JavaScript = script
			start := time.Now()
			if h := scrubHidden(t, d); h != want {
				t.Errorf("before scrub: %+v, want %+v", h, want)
			}
			out, re := scrubRoundTrip(t, pdf, ScrubOptions{KeepFormFields: true})
			if el := time.Since(start); el > 2*time.Second {
				t.Errorf("took %v", el)
			}
			scrubNoTokens(t, "KeepFormFields", out, tok)
			if h := scrubHidden(t, re); h != clean {
				t.Errorf("after scrub: %+v, want %+v", h, clean)
			}
		})
	}
}

// Scrub removes /AF wherever it occurs (here on an image the page draws, a
// form XObject, a structure element and a kept Link) and destroys the
// filespecs and embedded files, although the page still draws the image;
// a colour space resource that happens to be named /AF is not touched.
// (Reduced from veraPDF 6-8-t02-pass-b.pdf, where the attachment is listed
// in /Names/EmbeddedFiles and in the /AF of three drawn images.)
func TestScrubAFEverywhere(t *testing.T) {
	const imgTok, formTok, elemTok, linkTok, bodyTok = "GomupdfAFImage.csv", "GomupdfAFForm.csv",
		"GomupdfAFElem.csv", "GomupdfAFLink.csv", "GomupdfAFBody6e1d"
	fs := func(tok string, ef string) string {
		return `<< /Type /Filespec /F (` + tok + `) /UF (` + tok + `)` + ef + ` /AFRelationship /Source >>`
	}
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 13 0 R
   /Names << /EmbeddedFiles << /Names [(a) 8 0 R] >> >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Annots [12 0 R]
   /Resources << /Font << /F1 5 0 R >> /XObject << /Im1 6 0 R /Fm1 7 0 R >> /ColorSpace 16 0 R >> >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET q 40 0 0 40 120 20 cm /Im1 Do Q /Fm1 Do q /AF cs 1 sc 10 10 30 30 re f Q",
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 6 */ "/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /AF [8 0 R]\nstream\n\x00\xff\xff\x00",
		/* 7 */ "/Type /XObject /Subtype /Form /BBox [0 0 200 200] /AF [10 0 R]\nstream\n0 0 1 rg 60 60 20 20 re f",
		/* 8 */ fs(imgTok, ` /EF << /F 9 0 R >>`),
		/* 9 */ "/Type /EmbeddedFile\nstream\n" + bodyTok,
		/* 10 */ fs(formTok, ""),
		/* 11 */ fs(elemTok, ""),
		/* 12 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 20 20] /A << /S /URI /URI (https://example.com/) >> /AF [15 0 R] >>`,
		/* 13 */ `<< /Type /StructTreeRoot /K [14 0 R] >>`,
		/* 14 */ `<< /Type /StructElem /S /Figure /P 13 0 R /Pg 3 0 R /AF [11 0 R] >>`,
		/* 15 */ fs(linkTok, ""),
		/* 16 */ `<< /AF [/Indexed /DeviceRGB 1 <FF000000FF00>] >>`,
	}, "")
	scrubControl(t, pdf, imgTok, formTok, elemTok, linkTok, bodyTok)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if h := scrubHidden(t, d); h.AssociatedFiles != 4 {
		t.Errorf("AssociatedFiles before scrub = %d, want 4", h.AssociatedFiles)
	}
	re := scrubCheckKept(t, pdf, ScrubOptions{})
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	out, err := re.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	scrubNoTokens(t, "default", out, imgTok, formTok, elemTok, linkTok, bodyTok)
	p, _ := re.LoadPage(0)
	if l, err := p.Links(); err != nil || len(l) != 1 {
		t.Errorf("links after scrub = %+v, %v; want the URI link", l, err)
	}
}

// An /AF array with entries other than file specifications (null, a name)
// still loses its file specifications: the attachment of a drawn image goes,
// and the page draws as before.
func TestScrubAFMixedEntries(t *testing.T) {
	const tok, bodyTok = "GomupdfAFMixed.csv", "GomupdfAFMixedBody2c8e"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles << /Names [(a) 7 0 R] >> >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R
   /Resources << /Font << /F1 5 0 R >> /XObject << /Im1 6 0 R /Im2 9 0 R >> >> >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET q 40 0 0 40 120 20 cm /Im1 Do Q q 40 0 0 40 20 20 cm /Im2 Do Q",
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 6 */ "/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /AF [7 0 R null]\nstream\n\x00\xff\xff\x00",
		/* 7 */ `<< /Type /Filespec /F (` + tok + `) /UF (` + tok + `) /EF << /F 8 0 R >> >>`,
		/* 8 */ "/Type /EmbeddedFile\nstream\n" + bodyTok,
		/* 9 */ "/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /AF [/x 7 0 R]\nstream\n\xff\x00\x00\xff",
	}, "")
	scrubControl(t, pdf, tok, bodyTok)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if h := scrubHidden(t, d); h.AssociatedFiles != 2 {
		t.Errorf("AssociatedFiles before scrub = %d, want 2", h.AssociatedFiles)
	}
	re := scrubCheckKept(t, pdf, ScrubOptions{})
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	out, err := re.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	scrubNoTokens(t, "default", out, tok, bodyTok)
}

// A file whose cross-reference table is broken (it lists too few objects, so
// MuPDF would count 0 pages, and one entry points at the wrong object, so
// loading every object makes MuPDF repair it) is repaired before anything is
// read: the page that appears is checked, and PageCount is what a save would
// give. (Reduced from pdf.js GHOSTSCRIPT-698804-1-fuzzed.pdf.)
func TestScrubHiddenContentRepairsFirst(t *testing.T) {
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 3 0 R /Outline 2 0 R >>`, // sic, as in the original
		/* 2 */ `<< /Type /Outlines /Count 0 >>`,
		/* 3 */ `<< /Type /Pages /Kids [4 0 R] /Count 1 >>`,
		/* 4 */ `<< /Type /Page /Parent 3 0 R /MediaBox [0 0 200 200] /Annots [5 0 R] >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A << /S /JavaScript /JS (app.alert\(1\)) >> >>`,
	}, "")
	// List objects 0 to 2 only, with object 2 pointing at object 4.
	x := bytes.LastIndex(pdf, []byte("\nxref\n")) + 1
	rows := pdf[x+len("xref\n"):]
	rows = rows[bytes.IndexByte(rows, '\n')+1:]
	short := fmt.Appendf(append([]byte(nil), pdf[:x]...), "xref\n0 3\n%s%010d 00000 n \n",
		rows[:40], bytes.Index(pdf, []byte("\n4 0 obj"))+1)
	short = fmt.Appendf(short, "trailer\n<< /Size 3 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", x)
	// Open repairs a damaged file before anything reads it, so the page is
	// already there; before that, this file opened with 0 pages.
	d, err := OpenStream(short)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	h := scrubHidden(t, d)
	if !h.JavaScript {
		t.Errorf("HiddenContent missed the Link script on the repaired page: %+v", h)
	}
	if n := d.PageCount(); n != 1 {
		t.Errorf("PageCount = %d, want 1", n)
	}
}

// Cyclic and very long /Next chains on a Link /A or /PA neither crash nor
// pass as safe: HiddenContent reports JavaScript and Scrub removes the action.
func TestScrubLinkActionLimits(t *testing.T) {
	var chain []string
	for i := range 4999 {
		chain = append(chain, `<< /S /GoTo /D [3 0 R /Fit] /Next `+scrubRefs(i+6)+`>>`)
	}
	chain = append(chain, `<< /S /GoTo /D [3 0 R /Fit] >>`)
	cycle := []string{`<< /S /GoTo /D [3 0 R /Fit] /Next 5 0 R >>`}
	// linkPDF: a page whose only Link has /key 5 0 R, the chain from 5.
	linkPDF := func(key string, acts []string) []byte {
		return scrubBuildPDF(append([]string{
			`<< /Type /Catalog /Pages 2 0 R >>`,
			`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
			`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R] >>`,
			`<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /` + key + ` 5 0 R >>`,
		}, acts...), "")
	}
	cases := map[string][]byte{
		"A cycle":  linkPDF("A", cycle),
		"A long":   linkPDF("A", chain),
		"PA cycle": linkPDF("PA", cycle),
		"PA long":  linkPDF("PA", chain),
	}
	for name, pdf := range cases {
		t.Run(name, func(t *testing.T) {
			scrubConcurrently(func() {
				d, err := OpenStream(pdf)
				if err != nil {
					t.Error(err)
					return
				}
				defer d.Close()
				if h, err := d.HiddenContent(); err != nil || !h.JavaScript {
					t.Errorf("before scrub: %+v, %v; want JavaScript", h, err)
				}
				if err := d.Scrub(ScrubOptions{}); err != nil {
					t.Error(err)
				}
				if h, err := d.HiddenContent(); err != nil || h != (HiddenContent{}) {
					t.Errorf("after scrub: %+v, %v", h, err)
				}
			})
		})
	}
}

// scrubRedactWord redacts every occurrence of word on page 0 of d by its
// search quads and applies the redaction.
func scrubRedactWord(t *testing.T, d *Document, word string) {
	t.Helper()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	q, err := p.Search(word)
	if err != nil || len(q) == 0 {
		t.Fatalf("search %q: %v, %v", word, q, err)
	}
	if err := p.AddRedactionQuads(q, RedactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyRedactions(ApplyOptions{}); err != nil {
		t.Fatal(err)
	}
}

// A non-standard trailer key (qpdf's /QTest, a leftover /Gone) that holds the
// only reference to a page's old content stream: after a redaction replaces
// that stream, a garbage-collecting save would still write it out, as an
// object nothing references, because garbage collection marks from the whole
// trailer while the writer emits only the standard keys. Scrub deletes the
// key, from a table trailer, from a cross-reference stream's dictionary and
// from the trailer of an incremental update, whether or not an edit has
// already started a new xref section with a copy of the trailer. (Reduced
// from qpdf bad22.pdf and incremental-1.pdf.)
func TestScrubTrailerKeys(t *testing.T) {
	const word = "GomupdfTrailerSecret5c2a"
	page := `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>`
	objs := []string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ page,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (Visible " + word + ") Tj ET",
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
	}
	cases := []struct {
		name string
		pdf  []byte
		keys int
	}{
		{"table", scrubBuildPDF(objs, "/QTest 4 0 R"), 1},
		{"xref stream", scrubXRefStreamPDF(objs, "/QTest 4 0 R"), 1},
		{"incremental update", scrubAppendUpdate(scrubBuildPDF(objs, ""), 6, 3, page, "/Gone 4 0 R /QTest [4 0 R]"), 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, err := OpenStream(c.pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if h := scrubHidden(t, d); h != (HiddenContent{TrailerKeys: c.keys}) {
				t.Errorf("before scrub: %+v, want TrailerKeys %d", h, c.keys)
			}
			scrubRedactWord(t, d, word)
			control, err := d.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(control, []byte(word)) {
				t.Fatal("control: the redacted stream is not in the unscrubbed save")
			}
			if err := d.Scrub(ScrubOptions{}); err != nil {
				t.Fatal(err)
			}
			if h := scrubHidden(t, d); h != (HiddenContent{}) {
				t.Errorf("in memory after scrub: %+v", h)
			}
			out, err := d.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			scrubNoTokens(t, c.name, out, word)
			re, err := OpenStream(out)
			if err != nil {
				t.Fatal(err)
			}
			defer re.Close()
			if h := scrubHidden(t, re); h != (HiddenContent{}) {
				t.Errorf("after scrub: %+v", h)
			}
			if s := scrubText(t, re); !strings.Contains(s, "Visible") || strings.Contains(s, word) {
				t.Errorf("page text after redaction and scrub = %q", s)
			}

			// Scrub on its own, its trailer edit the first edit of the
			// document, leaves no key either.
			_, re = scrubRoundTrip(t, c.pdf, ScrubOptions{})
			if h := scrubHidden(t, re); h != (HiddenContent{}) {
				t.Errorf("scrub only: %+v", h)
			}
		})
	}

	// A cross-reference stream's own keys and the standard ones are not
	// counted: MuPDF's trailer for such a file is the stream's dictionary.
	for name, pdf := range map[string][]byte{
		"xref stream": scrubXRefStreamPDF(objs, "/Info << /Title (x) >> /ID [<00> <00>]"),
		"incremental": scrubAppendUpdate(scrubBuildPDF(objs, "/ID [<00> <00>]"), 6, 3, page, "/ID [<00> <01>]"),
	} {
		d, err := OpenStream(pdf)
		if err != nil {
			t.Fatal(err)
		}
		if h := scrubHidden(t, d); h.TrailerKeys != 0 {
			t.Errorf("%s: TrailerKeys = %d, want 0", name, h.TrailerKeys)
		}
		d.Close()
	}
}

// Tokens of scrubOrphanPagesPDF, each in exactly one object.
const (
	orphanA     = "GomupdfOrphanStructPage4b1e"
	orphanForm  = "GomupdfOrphanForm8c3d"
	orphanAnnot = "GomupdfOrphanAnnot2e9f"
	orphanB     = "GomupdfOrphanDestsPage7a5c"
	orphanC     = "GomupdfOrphanLinkPage1d6b"
	orphanT     = "GomupdfOrphanTemplate9f2a"
)

// scrubOrphanPagesPDF is a one-page tagged PDF with four page dictionaries
// that are not in the page tree, each holding text MuPDF never extracts: A
// (6), which only a structure element's /Pg reaches (pymupdf test_3725), and
// which draws the form 13 and lists the Square 14; B (7), only in a
// /Names/Dests destination (test_4412), whose /Contents array also holds the
// real page's content stream 4; C (8), untyped, only the destination of the
// real page's Link (test_4363's extra pages, which keep /Parent to the real
// tree); and the template T (17) of /Names/Templates. All of them use the real
// page's font 5, and the structure element references every orphan stream and
// the Square, so unlinking alone keeps them.
func scrubOrphanPagesPDF() []byte {
	text := func(y int, s string) string { return fmt.Sprintf("\nstream\nBT /F1 12 Tf 20 %d Td (%s) Tj ET", y, s) }
	const res = `/Resources << /Font << /F1 5 0 R >> >>`
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 10 0 R
   /Names << /Dests << /Names [(d) [7 0 R /Fit]] >> /Templates << /Names [(t) 17 0 R] >> >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] ` + res + ` /Contents 4 0 R /Annots [12 0 R] >>`,
		/* 4 */ text(150, scrubPageText),
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 6 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> /XObject << /Fm1 13 0 R >> >>
   /Contents 9 0 R /Annots [14 0 R] /StructParents 0 >>`,
		/* 7 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] ` + res + ` /Contents [15 0 R 4 0 R] >>`,
		/* 8 */ `<< /Parent 2 0 R /MediaBox [0 0 300 200] ` + res + ` /Contents 16 0 R >>`,
		/* 9 */ "\nstream\nBT /F1 12 Tf 20 100 Td (" + orphanA + ") Tj ET /Fm1 Do",
		/* 10 */ `<< /Type /StructTreeRoot /K [11 0 R] >>`,
		/* 11 */ `<< /Type /StructElem /S /P /P 10 0 R /Pg 6 0 R /K 0 /GomupdfRefs [9 0 R 13 0 R 14 0 R 15 0 R 16 0 R 18 0 R] >>`,
		/* 12 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /Dest [8 0 R /Fit] >>`,
		/* 13 */ "/Type /XObject /Subtype /Form /BBox [0 0 300 200] " + res + text(50, orphanForm),
		/* 14 */ `<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /Contents (` + orphanAnnot + `) >>`,
		/* 15 */ text(120, orphanB),
		/* 16 */ text(80, orphanC),
		/* 17 */ `<< /Type /Template /MediaBox [0 0 300 200] ` + res + ` /Contents 18 0 R >>`,
		/* 18 */ text(60, orphanT),
	}, "")
}

// Page dictionaries that are not in the page tree lose their content: their
// content streams and own images and forms are destroyed although the
// structure tree references them, while the content stream and font the real
// page shares with them stay, and the real page renders and extracts as
// before. Each emptied dictionary stays, page-shaped, for the references to
// it. KeepAnnotations keeps (only unlinks) the orphan page's annotation.
func TestScrubOrphanPages(t *testing.T) {
	pdf := scrubOrphanPagesPDF()
	content := []string{orphanA, orphanForm, orphanB, orphanC, orphanT}
	scrubControl(t, pdf, append(content, orphanAnnot)...)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if d.PageCount() != 1 {
		t.Fatalf("fixture: %d pages, want 1", d.PageCount())
	}
	// The Square on the orphan page A is an annotation on no page.
	if h := scrubHidden(t, d); h != (HiddenContent{OrphanPages: 4, NonLinkAnnotations: 1}) {
		t.Errorf("before scrub: %+v, want OrphanPages 4 and one annotation", h)
	}
	for _, opts := range []ScrubOptions{{}, {KeepAnnotations: true}} {
		t.Run(fmt.Sprintf("%+v", opts), func(t *testing.T) {
			re := scrubCheckKept(t, pdf, opts)
			out, err := re.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			scrubNoTokens(t, "orphan pages", out, content...)
			if got := bytes.Contains(out, []byte(orphanAnnot)); got != opts.KeepAnnotations {
				t.Errorf("orphan page annotation in save = %v, want %v", got, opts.KeepAnnotations)
			}
			want := HiddenContent{}
			if opts.KeepAnnotations {
				want.NonLinkAnnotations = 1
			}
			if h := scrubHidden(t, re); h != want {
				t.Errorf("after scrub: %+v, want %+v", h, want)
			}
			if n := len(regexp.MustCompile(`/Type\s*/Page\b`).FindAll(out, -1)); n != 3 {
				t.Errorf("%d /Type /Page dictionaries after scrub, want 3", n)
			}
			if !regexp.MustCompile(`/Type\s*/Template\b`).Match(out) {
				t.Error("the emptied template is gone")
			}
			p, err := re.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			if l, err := p.Links(); err != nil || len(l) != 1 {
				t.Errorf("links after scrub = %+v, %v; want the Link", l, err)
			}
		})
	}
}

// scrubOrphanCase checks pdf (whose real page 0 shows scrubPageText and
// shares resources or content with the content it hides) before Scrub
// against before, then for each opts that a Scrub keeps page 0's text and
// rendering, leaves none of tokens in a Garbage 1 save and reads after.
func scrubOrphanCase(t *testing.T, pdf []byte, before, after HiddenContent, tokens ...string) {
	t.Helper()
	scrubControl(t, pdf, tokens...)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if h := scrubHidden(t, d); h != before {
		t.Errorf("before scrub:\n got %+v\nwant %+v", h, before)
	}
	re := scrubCheckKept(t, pdf, ScrubOptions{})
	out, err := re.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	scrubNoTokens(t, "scrubbed", out, tokens...)
	if h := scrubHidden(t, re); h != after {
		t.Errorf("after scrub:\n got %+v\nwant %+v", h, after)
	}
}

// An orphan page's content goes beyond its own /Contents and /Resources: the
// resources it inherits through a /Parent outside the page tree, and forms
// nested in its forms, are destroyed too, although the structure tree
// references them; the real page, which shares the font and draws one of the
// same forms, renders and extracts as before.
func TestScrubOrphanPageClosure(t *testing.T) {
	const inheritTok, nestedTok = "GomupdfOrphanInherited5d2c", "GomupdfOrphanNested8a1f"
	text := func(s string) string { return "\nstream\nBT /F1 12 Tf 20 60 Td (" + s + ") Tj ET" }
	form := func(res, body string) string {
		return "/Type /XObject /Subtype /Form /BBox [0 0 300 200] /Resources " + res + body
	}
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 8 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> /XObject << /Sh 15 0 R >> >> /Contents 4 0 R >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET /Sh Do",
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		// Orphan A inherits its resources from the /Pages node 7, outside the tree.
		/* 6 */ `<< /Type /Page /Parent 7 0 R /MediaBox [0 0 300 200] /Contents 10 0 R >>`,
		/* 7 */ `<< /Type /Pages /Kids [6 0 R] /Count 1 /Resources << /Font << /F1 5 0 R >> /XObject << /Fm1 9 0 R >> >> >>`,
		/* 8 */ `<< /Type /StructTreeRoot /K [11 0 R] >>`,
		/* 9 */ form(`<< /Font << /F1 5 0 R >> >>`, text(inheritTok)),
		/* 10 */ "\nstream\n/Fm1 Do",
		/* 11 */ `<< /Type /StructElem /S /P /P 8 0 R /Pg 6 0 R /K [<< /Type /OBJR /Obj 12 0 R >> << /Type /MCR /Pg 12 0 R /Stm 14 0 R /MCID 0 >>]
   /GomupdfRefs [9 0 R 13 0 R] >>`,
		// Orphan B draws the form 13, which draws the form 14 and the
		// real page's form 15.
		/* 12 */ `<< /Type /Page /MediaBox [0 0 300 200] /Resources << /XObject << /Fm1 13 0 R >> >> /Contents 10 0 R >>`,
		/* 13 */ form(`<< /XObject << /Fm2 14 0 R /Sh 15 0 R >> >>`, "\nstream\n/Fm2 Do /Sh Do"),
		/* 14 */ form(`<< /Font << /F1 5 0 R >> >>`, text(nestedTok)),
		/* 15 */ form(`<< >>`, "\nstream\n0 0 1 rg 200 20 30 30 re f"),
	}, "")
	scrubOrphanCase(t, pdf, HiddenContent{OrphanPages: 2}, HiddenContent{}, inheritTok, nestedTok)
}

// Page-tree nodes MuPDF does not serve are not part of the document: a page
// past a /Count that is too small is an orphan page, and the annotations on
// an intermediate /Pages node are annotations on no page. Their text, the
// note's /Contents and the attachment's embedded file are destroyed, while
// a page listed twice, which MuPDF serves, is not an orphan.
func TestScrubUncountedPageTreeNodes(t *testing.T) {
	const pageTok, noteTok, fileTok = "GomupdfUncountedPage3c7e", "GomupdfUncountedNote6b2a", "GomupdfNodeAttachBody1f8d"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [10 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 10 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 6 */ `<< /Type /Page /Parent 10 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 7 0 R /Annots [8 0 R] >>`,
		/* 7 */ "\nstream\nBT /F1 12 Tf 20 100 Td (" + pageTok + ") Tj ET",
		/* 8 */ `<< /Type /Annot /Subtype /Text /Rect [10 10 30 30] /Contents (` + noteTok + `) >>`,
		/* 9 */ `<< /Type /Annot /Subtype /FileAttachment /Rect [0 0 10 10] /FS << /Type /Filespec /F (a.txt) /EF << /F 11 0 R >> >> >>`,
		/* 10 */ `<< /Type /Pages /Parent 2 0 R /Kids [3 0 R 6 0 R] /Count 1 /Annots [9 0 R] >>`,
		/* 11 */ "/Type /EmbeddedFile\nstream\n" + fileTok,
	}, "")
	scrubOrphanCase(t, pdf, HiddenContent{OrphanPages: 1, NonLinkAnnotations: 2}, HiddenContent{}, pageTok, noteTok, fileTok)

	// The real page listed twice under a /Count of 2: both entries are the
	// page MuPDF serves, so nothing is an orphan and the page keeps its text.
	twice := scrubBuildPDF([]string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R 3 0 R] /Count 2 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>`,
		"\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
	}, "")
	scrubOrphanCase(t, twice, HiddenContent{}, HiddenContent{})
}

// Thousands of orphan pages sharing one large /Contents array cost one walk
// of it, not one per page.
func TestScrubOrphanPagesSharedContents(t *testing.T) {
	const orphans, entries = 5000, 50000
	objs := []string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 6 */ `[` + strings.Repeat("4 0 R ", entries) + `]`,
	}
	for range orphans {
		objs = append(objs, `<< /Type /Page /MediaBox [0 0 300 200] /Contents 6 0 R >>`)
	}
	pdf := scrubBuildPDF(objs, "")
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	start := time.Now()
	if h := scrubHidden(t, d); h.OrphanPages != orphans {
		t.Errorf("OrphanPages %d, want %d", h.OrphanPages, orphans)
	}
	if err := d.Scrub(ScrubOptions{}); err != nil {
		t.Fatal(err)
	}
	if el := time.Since(start); el > 2*time.Second {
		t.Errorf("HiddenContent and Scrub took %v", el)
	}
	if h := scrubHidden(t, d); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	if s := scrubText(t, d); !strings.Contains(s, scrubPageText) {
		t.Errorf("page text after scrub = %q", s)
	}
}

// Tokens of scrubPrivateDataPDF, each in exactly one object.
const (
	pvFontXMP   = "GomupdfPvFontXMP3a1b"
	pvFormXMP   = "GomupdfPvFormXMP7c2d"
	pvStructXMP = "GomupdfPvStructXMP5e4f"
	pvDescXMP   = "GomupdfPvDescXMP1a8b"
	pvICCXMP    = "GomupdfPvICCXMP9d3c"
	pvImagePriv = "GomupdfPvImagePrivate2b6e"
	pvAIPriv    = "GomupdfPvAIPrivate8f1a"
	pvLM        = "D:1999GomupdfPvLastMod"
	pvSig       = "GomupdfPvSigName4c7e"
	pvCert      = "GomupdfPvCert6a2d@example.com"
	pvSpider    = "GomupdfPvSpiderURL5b9c"
	pvSpiderSet = "GomupdfPvSpiderSet3e8a"
	pvTrap      = "GomupdfPvTrapProperty7d4b"
	pvTrapXMP   = "GomupdfPvTrapXMPProperty2c5a"
	pvTrapLM    = "GomupdfPvTrapLMProperty8e3f"
)

var (
	pvPrivate   = []string{pvFontXMP, pvFormXMP, pvStructXMP, pvDescXMP, pvICCXMP, pvImagePriv, pvAIPriv, pvLM, pvSpider, pvSpiderSet}
	pvSignature = []string{pvSig, pvCert}
)

// scrubPrivateDataPDF is a tagged one-page PDF with private data on objects
// other than the catalog and pages: XMP /Metadata on a font (6), a form
// XObject (7), a structure element (17), a font descriptor (20) and an ICC
// profile (22, an output intent's; the intent is referenced only from the
// structure element, so MuPDF does not try to load the fake profile);
// /PieceInfo on the form (Illustrator's
// shape: a data dictionary whose /Private dictionary holds a private stream)
// and on an image (8); /LastModified on the catalog, the page, the form and
// the image; catalog /Perms (a DocMDP signature, 24) and /DSS (a certificate
// stream, 26); and the web-capture /SpiderInfo with /Names/IDS and /URLS (the
// content set 27). The structure element references every removed object.
// The page also draws through resources named /Metadata (a form), /PieceInfo
// (a graphics state, and a marked-content property list that looks like a
// page-piece dictionary) and /LastModified (a font), and marked-content
// property lists named /Metadata (an XMP-typed stream) and /LastModified (a
// string), in indirect resource dictionaries, which Scrub must not touch.
func scrubPrivateDataPDF() []byte {
	xmp := func(tok string) string {
		return "/Type /Metadata /Subtype /XML\nstream\n<x:xmpmeta>" + tok + "</x:xmpmeta>"
	}
	lm := `/LastModified (` + pvLM + `)`
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R ` + lm + ` /MarkInfo << /Marked true >> /StructTreeRoot 19 0 R
   /Perms << /DocMDP 24 0 R >> /DSS 25 0 R /SpiderInfo << /V 1.0 /C [<< /URL (` + pvSpider + `) >>] >>
   /Names << /IDS << /Names [(id1) 27 0 R] >> /URLS << /Names [(http://example.com/) 27 0 R] >> >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources 5 0 R /Contents 4 0 R ` + lm + ` >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET q /PieceInfo gs 1 0 0 rg 10 10 30 30 re f Q /Fm1 Do" +
			" q 40 0 0 40 120 20 cm /Im1 Do Q /Metadata Do /Span /PieceInfo BDC BT /LastModified 10 Tf 20 120 Td (Trap) Tj ET EMC" +
			" /Span /Metadata BDC EMC /Span /LastModified BDC EMC",
		/* 5 */ `<< /Font 31 0 R /XObject 32 0 R /ExtGState 33 0 R /Properties 30 0 R >>`,
		/* 6 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Metadata 11 0 R >>`,
		/* 7 */ "/Type /XObject /Subtype /Form /BBox [0 0 300 200] /PieceInfo << /Illustrator 12 0 R >> /Metadata 13 0 R " + lm +
			"\nstream\n0 0 1 rg 60 60 20 20 re f",
		/* 8 */ "/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 " + lm +
			" /PieceInfo << /App << " + lm + " /Private 14 0 R >> >>\nstream\n\x00\xff\xff\x00",
		/* 9 */ "/Type /XObject /Subtype /Form /BBox [0 0 300 200]\nstream\n0 1 0 rg 100 100 20 20 re f",
		/* 10 */ `<< /ca 0.5 >>`,
		/* 11 */ xmp(pvFontXMP),
		/* 12 */ `<< ` + lm + ` /Private 15 0 R >>`,
		/* 13 */ xmp(pvFormXMP),
		/* 14 */ "\nstream\n" + pvImagePriv,
		/* 15 */ `<< /AIPrivateData1 16 0 R /NumBlock 1 >>`,
		/* 16 */ "\nstream\n" + pvAIPriv,
		/* 17 */ `<< /Type /StructElem /S /Figure /P 19 0 R /Metadata 18 0 R
   /GomupdfRefs [11 0 R 12 0 R 13 0 R 14 0 R 15 0 R 16 0 R 20 0 R 21 0 R 23 0 R 24 0 R 26 0 R 27 0 R 29 0 R] >>`,
		/* 18 */ xmp(pvStructXMP),
		/* 19 */ `<< /Type /StructTreeRoot /K [17 0 R] >>`,
		/* 20 */ `<< /Type /FontDescriptor /FontName /GomupdfDesc /Flags 32 /Metadata 29 0 R >>`,
		/* 21 */ `<< /Type /OutputIntent /S /GTS_PDFA1 /OutputConditionIdentifier (sRGB) /DestOutputProfile 22 0 R >>`,
		/* 22 */ "/N 1 /Metadata 23 0 R\nstream\nnot a real profile",
		/* 23 */ xmp(pvICCXMP),
		/* 24 */ `<< /Type /Sig /Filter /Adobe.PPKLite /SubFilter /adbe.pkcs7.detached /Name (` + pvSig + `) /Contents <00> >>`,
		/* 25 */ `<< /Certs [26 0 R] >>`,
		/* 26 */ "\nstream\n" + pvCert,
		/* 27 */ `<< /Type /SpiderContentSet /S /SPS /ID <00> /O [] /SI << /AU (` + pvSpiderSet + `) >> >>`,
		/* 28 */ `<< /A << /Private (` + pvTrap + `) >> >>`,
		/* 29 */ xmp(pvDescXMP),
		/* 30 */ `<< /PieceInfo 28 0 R /Metadata 34 0 R /LastModified (` + pvTrapLM + `) >>`,
		/* 31 */ `<< /F1 6 0 R /LastModified 6 0 R >>`,
		/* 32 */ `<< /Fm1 7 0 R /Im1 8 0 R /Metadata 9 0 R >>`,
		/* 33 */ `<< /PieceInfo 10 0 R >>`,
		/* 34 */ xmp(pvTrapXMP),
	}, "")
}

// Scrub removes /Metadata, /PieceInfo and /LastModified from every object on
// which they have that meaning and destroys their XMP streams and private
// data, although the structure tree references them and the page draws the
// form and image that held them; it removes /SpiderInfo with /Names/IDS and
// /URLS always, and /Perms and /DSS unless the form stays. Resources with
// those names are not touched: the page draws and extracts as before.
func TestScrubPrivateData(t *testing.T) {
	pdf := scrubPrivateDataPDF()
	traps := []string{pvTrap, pvTrapXMP, pvTrapLM}
	scrubControl(t, pdf, slices.Concat(pvPrivate, pvSignature, traps)...)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	want := HiddenContent{PieceInfo: 2, ObjectMetadata: 5, LastModified: 4, Perms: true, DSS: true, WebCapture: true}
	if h := scrubHidden(t, d); h != want {
		t.Errorf("before scrub:\n got %+v\nwant %+v", h, want)
	}
	for _, opts := range []ScrubOptions{{}, {KeepFormFields: true}, {KeepAnnotations: true}} {
		t.Run(fmt.Sprintf("%+v", opts), func(t *testing.T) {
			re := scrubCheckKept(t, pdf, opts)
			out, err := re.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			scrubNoTokens(t, "private data", out, pvPrivate...)
			for _, tok := range traps {
				if !bytes.Contains(out, []byte(tok)) {
					t.Errorf("the property list holding %q was removed", tok)
				}
			}
			keep := opts != (ScrubOptions{})
			for _, tok := range pvSignature {
				if got := bytes.Contains(out, []byte(tok)); got != keep {
					t.Errorf("%q in save = %v, want %v", tok, got, keep)
				}
			}
			if h := scrubHidden(t, re); h != (HiddenContent{Perms: keep, DSS: keep}) {
				t.Errorf("after scrub: %+v", h)
			}
		})
	}
}

// Tokens of scrubOrphanAnnotsPDF, each in exactly one object.
const (
	oaWidget   = "GomupdfOrphanWidgetValue3c8e"
	oaWidgetJS = "GomupdfOrphanWidgetJS6a1d"
	oaAP       = "GomupdfOrphanAppearance9b2f"
	oaField    = "GomupdfOrphanFieldValue4d7a"
	oaSquare   = "GomupdfOrphanSquare1e5c"
	oaLinkJS   = "GomupdfOrphanLinkJS8f3b"
	oaFormKid  = "GomupdfOrphanFormKid2a6d"
)

// scrubOrphanAnnotsPDF is a tagged one-page form whose page lists a Link (4)
// and the hidden widget 5 of /Fields (hidden, so the page renders the same
// with or without it). Only the structure tree reaches the rest
// (pdf.js bug1722036.pdf, where /T (lea) widgets are on no page and in no
// field tree): the widget 6 (with a keystroke script /AA and an appearance
// stream 7), the field 8 (no /Subtype), the Square 9 and the Link 11 with a
// JavaScript /A. The field 12 of /Fields has a kid widget 13 that is on no
// page, which is part of the form.
func scrubOrphanAnnotsPDF() []byte {
	var objr strings.Builder
	for _, n := range []int{6, 8, 9, 11, 13} {
		fmt.Fprintf(&objr, "<< /Type /OBJR /Obj %d 0 R >> ", n)
	}
	return scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R 12 0 R] >> /MarkInfo << /Marked true >> /StructTreeRoot 10 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 15 0 R >> >> /Contents 16 0 R /Annots [4 0 R 5 0 R] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A << /S /URI /URI (https://example.com/) >> >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Widget /FT /Tx /T (real) /V (real value) /Rect [20 150 200 170] /F 2 /P 3 0 R >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Widget /FT /Tx /T (lea) /TU (Appropriate LEA) /V (` + oaWidget + `) /Rect [20 100 200 120]
   /AP << /N 7 0 R >> /AA << /K << /S /JavaScript /JS (var ` + oaWidgetJS + ` = 1;) >> >> >>`,
		/* 7 */ "/Type /XObject /Subtype /Form /BBox [0 0 180 20] /Resources << /Font << /F1 15 0 R >> >>\nstream\nBT /F1 10 Tf 2 5 Td (" + oaAP + ") Tj ET",
		/* 8 */ `<< /FT /Tx /T (lone) /V (` + oaField + `) >>`,
		/* 9 */ `<< /Type /Annot /Subtype /Square /Rect [150 20 250 60] /Contents (` + oaSquare + `) >>`,
		/* 10 */ `<< /Type /StructTreeRoot /K [14 0 R] >>`,
		/* 11 */ `<< /Subtype /Link /Rect [60 0 110 50] /A << /S /JavaScript /JS (var ` + oaLinkJS + ` = 1;) >> >>`,
		/* 12 */ `<< /FT /Tx /T (pair) /V (` + oaFormKid + `) /Kids [13 0 R] >>`,
		/* 13 */ `<< /Type /Annot /Subtype /Widget /Parent 12 0 R /Rect [20 60 200 80] >>`,
		/* 14 */ `<< /Type /StructElem /S /Form /P 10 0 R /Pg 3 0 R /K [` + objr.String() + `] >>`,
		/* 15 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 16 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
	}, "")
}

// Annotations and fields on no page and in no field tree are reported, and
// Scrub removes what the options would remove from a page: by default all of
// them but the Link, with KeepFormFields the Square, with KeepAnnotations
// nothing. Whatever stays loses its scripts, as on a page; the orphan Link
// stays inert. A widget of the form that is on no page is part of the form.
func TestScrubOrphanAnnotations(t *testing.T) {
	pdf := scrubOrphanAnnotsPDF()
	all := []string{oaWidget, oaWidgetJS, oaAP, oaField, oaSquare, oaLinkJS, oaFormKid}
	scrubControl(t, pdf, all...)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	want := HiddenContent{AcroForm: true, JavaScript: true, Widgets: 3, NonLinkAnnotations: 1}
	if h := scrubHidden(t, d); h != want {
		t.Errorf("before scrub:\n got %+v\nwant %+v", h, want)
	}
	cases := []struct {
		opts  ScrubOptions
		kept  []string
		after HiddenContent
	}{
		{ScrubOptions{}, nil, HiddenContent{}},
		{ScrubOptions{KeepFormFields: true}, []string{oaWidget, oaAP, oaField, oaFormKid}, HiddenContent{AcroForm: true, Widgets: 3}},
		{ScrubOptions{KeepAnnotations: true}, []string{oaWidget, oaAP, oaField, oaFormKid, oaSquare},
			HiddenContent{AcroForm: true, Widgets: 3, NonLinkAnnotations: 1}},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%+v", c.opts), func(t *testing.T) {
			re := scrubCheckKept(t, pdf, c.opts)
			out, err := re.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			for _, tok := range all {
				if got := bytes.Contains(out, []byte(tok)); got != slices.Contains(c.kept, tok) {
					t.Errorf("%q in save = %v, want %v", tok, got, !got)
				}
			}
			if h := scrubHidden(t, re); h != c.after {
				t.Errorf("after scrub:\n got %+v\nwant %+v", h, c.after)
			}
			if n := len(scrubLinkRe.FindAll(out, -1)); n != 2 {
				t.Errorf("%d Link annotations after scrub, want 2", n)
			}
			p, err := re.LoadPage(0)
			if err != nil {
				t.Fatal(err)
			}
			if l, err := p.Links(); err != nil || len(l) != 1 || l[0].URI != "https://example.com/" {
				t.Errorf("links after scrub = %+v, %v; want the URI link", l, err)
			}
		})
	}
}

// A widget on no page that is part of the form only as the sibling of a page
// widget under a field /Fields leaves out has its script reported and, when
// the form stays, removed.
func TestScrubFormSiblingScript(t *testing.T) {
	const tok = "GomupdfSiblingJS4f1c"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [] >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [5 0 R] >>`,
		/* 4 */ `<< /FT /Tx /T (parent) /Kids [5 0 R 6 0 R] >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Widget /Parent 4 0 R /Rect [20 150 200 170] /P 3 0 R >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Widget /Parent 4 0 R /Rect [20 100 200 120] /AA << /K << /S /JavaScript /JS (var ` + tok + ` = 1;) >> >> >>`,
	}, "")
	scrubControl(t, pdf, tok)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if h := scrubHidden(t, d); !h.JavaScript {
		t.Errorf("before scrub: %+v, want JavaScript", h)
	}
	for _, opts := range []ScrubOptions{{}, {KeepFormFields: true}, {KeepAnnotations: true}} {
		out, re := scrubRoundTrip(t, pdf, opts)
		scrubNoTokens(t, fmt.Sprintf("%+v", opts), out, tok)
		if h := scrubHidden(t, re); h.JavaScript {
			t.Errorf("%+v: after scrub %+v", opts, h)
		}
	}
}

// A Hide action's /T names the annotation it hides. A kept Link's Hide action
// and a kept /OpenAction Hide action that target an annotation Scrub removes
// do not keep it: the annotation is destroyed, and /T resolves to null.
// (Reduced from veraPDF 6-6-1-t01-fail-a.pdf.)
func TestScrubHideActionTarget(t *testing.T) {
	const tokSquare, tokCircle = "GomupdfHideSquare7e2a", "GomupdfHideCircle3b9d"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /OpenAction << /S /Hide /T 6 0 R /H true >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [4 0 R 5 0 R 6 0 R] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A << /S /Hide /T 5 0 R >> >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Square /Rect [60 60 120 120] /Contents (` + tokSquare + `) >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Circle /Rect [130 60 190 120] /Contents (` + tokCircle + `) >>`,
	}, "")
	scrubControl(t, pdf, tokSquare, tokCircle)
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
	scrubNoTokens(t, "default", out, tokSquare, tokCircle)
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	if n := len(scrubLinkRe.FindAll(out, -1)); n != 1 {
		t.Errorf("%d Link annotations after scrub, want 1", n)
	}
	// KeepAnnotations keeps both.
	out, _ = scrubRoundTrip(t, pdf, ScrubOptions{KeepAnnotations: true})
	for _, tok := range []string{tokSquare, tokCircle} {
		if !bytes.Contains(out, []byte(tok)) {
			t.Errorf("KeepAnnotations: %q removed", tok)
		}
	}
}

// scrubSparseXRefPDF is scrubXRefStreamPDF with /Size size and a free entry
// size-1: the entries between the xref stream and that one are listed in no
// section.
func scrubSparseXRefPDF(objs []string, size int) []byte {
	pdf := scrubBuildPDF(objs, "")
	pdf = pdf[:bytes.LastIndex(pdf, []byte("\nxref\n"))+1]
	n := len(objs) + 1
	var rows bytes.Buffer
	rows.Write([]byte{0, 0, 0, 0, 0, 0xff, 0xff})
	for i := range objs {
		o := bytes.Index(pdf, fmt.Appendf(nil, "\n%d 0 obj\n", i+1)) + 1
		rows.Write([]byte{1, byte(o >> 24), byte(o >> 16), byte(o >> 8), byte(o), 0, 0})
	}
	x := len(pdf)
	rows.Write([]byte{1, byte(x >> 24), byte(x >> 16), byte(x >> 8), byte(x), 0, 0})
	rows.Write([]byte{0, 0, 0, 0, 0, 0xff, 0xff})
	b := bytes.NewBuffer(pdf)
	fmt.Fprintf(b, "%d 0 obj\n<< /Type /XRef /Size %d /Index [0 %d %d 1] /W [1 4 2] /Root 1 0 R /Length %d >>\nstream\n",
		n, size, n+1, size-1, rows.Len())
	b.Write(rows.Bytes())
	fmt.Fprintf(b, "\nendstream\nendobj\nstartxref\n%d\n%%%%EOF\n", x)
	return b.Bytes()
}

// Entries that hold no object after Open loaded everything (100,000 members
// of an object stream that does not parse, 155,000 entries no section
// lists) are not parsed again by every step of the all-objects sweep:
// HiddenContent, Scrub and ShowAllLayers each take well under a second.
// (Reduced from pdf.js issue16091.pdf, whose /Size is 156,342 with a few
// dozen objects.)
func TestScrubUnloadableEntriesCost(t *testing.T) {
	cases := map[string][]byte{
		"broken object stream": brokenObjStmPDF(100000),
		"sparse xref": scrubSparseXRefPDF([]string{
			`<< /Type /Catalog /Pages 2 0 R >>`,
			`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
			`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>`,
			"\nstream\nBT /F1 24 Tf 20 100 Td (Hello) Tj ET",
			`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		}, 155000),
	}
	for name, pdf := range cases {
		t.Run(name, func(t *testing.T) {
			for _, step := range []struct {
				name string
				run  func(*Document) error
			}{
				{"HiddenContent", func(d *Document) error { _, err := d.HiddenContent(); return err }},
				{"Scrub", func(d *Document) error { return d.Scrub(ScrubOptions{}) }},
				{"ShowAllLayers", func(d *Document) error { return d.ShowAllLayers() }},
			} {
				d, err := OpenStream(pdf)
				if err != nil {
					t.Fatal(err)
				}
				start := time.Now()
				err = step.run(d)
				took := time.Since(start)
				if err != nil {
					t.Errorf("%s: %v", step.name, err)
				}
				t.Logf("%s: %v", step.name, took)
				if took > 500*time.Millisecond {
					t.Errorf("%s took %v", step.name, took)
				}
				if s := scrubText(t, d); !strings.Contains(s, "Hello") {
					t.Errorf("%s: page text %q", step.name, s)
				}
				d.Close()
			}
		})
	}
}

// A non-standard trailer key whose value is null (qpdf bad14.pdf's
// /Something null) is removed like any other, instead of failing Scrub.
func TestScrubTrailerNullKey(t *testing.T) {
	pdf := scrubMiniPDF("", "", "")
	pdf = bytes.Replace(pdf, []byte("/Root 1 0 R"), []byte("/Root 1 0 R /Something null"), 1)
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if h := scrubHidden(t, d); h != (HiddenContent{TrailerKeys: 1}) {
		t.Errorf("before scrub: %+v, want TrailerKeys 1", h)
	}
	if err := d.Scrub(ScrubOptions{}); err != nil {
		t.Fatal(err)
	}
	if h := scrubHidden(t, d); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
}

// Under KeepFormFields a kept widget's appearance keeps everything it uses,
// even a graphics state shaped like an annotation: the widget renders as
// before. HiddenContent still counts that object as an annotation on no page.
func TestScrubKeptAppearanceResource(t *testing.T) {
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 8 0 R >> >> /Contents 4 0 R /Annots [5 0 R] >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
		/* 5 */ `<< /Type /Annot /Subtype /Widget /FT /Btn /Ff 65536 /T (b) /Rect [20 20 120 80] /P 3 0 R /AP << /N 6 0 R >> >>`,
		/* 6 */ "/Type /XObject /Subtype /Form /BBox [0 0 100 60] /Resources << /ExtGState << /G 7 0 R >> >>\nstream\n/G gs 1 0 0 rg 0 0 100 60 re f",
		/* 7 */ `<< /Subtype /Square /Rect [0 0 1 1] /ca 0.3 >>`,
		/* 8 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
	}, "")
	re := scrubCheckKept(t, pdf, ScrubOptions{KeepFormFields: true})
	if h := scrubHidden(t, re); h != (HiddenContent{AcroForm: true, Widgets: 1, NonLinkAnnotations: 1}) {
		t.Errorf("after scrub: %+v", h)
	}
}

// The annotations of a page MuPDF does not serve are annotations on no page,
// and go or stay with the options as such: KeepAnnotations keeps them in the
// saved file (the orphan page keeps its /Annots, so a garbage-collecting
// save does not drop them), the default options destroy all but the Link.
// (Reduced from veraPDF isartor-6-5-2-t01-fail-a.pdf and pdf.js
// poppler-395-0-fuzzed.pdf, whose annotations are on such pages.)
func TestScrubOrphanPageAnnotations(t *testing.T) {
	const textTok, popupTok = "GomupdfOrphanNote5e3b", "GomupdfOrphanPopup9a4c"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 7 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>`,
		/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
		/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		/* 6 */ `<< /Type /Page /MediaBox [0 0 300 200] /Annots 9 0 R >>`,
		/* 7 */ `<< /Type /StructTreeRoot /K [8 0 R] >>`,
		/* 8 */ `<< /Type /StructElem /S /P /P 7 0 R /Pg 6 0 R >>`,
		/* 9 */ `[10 0 R 11 0 R 12 0 R]`,
		/* 10 */ `<< /Type /Annot /Subtype /Text /P 6 0 R /Rect [10 10 30 30] /Contents (` + textTok + `) /Popup 11 0 R >>`,
		/* 11 */ `<< /Type /Annot /Subtype /Popup /Parent 10 0 R /Rect [30 30 130 90] /Contents (` + popupTok + `) >>`,
		/* 12 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /URI /URI (https://example.com/) >> >>`,
	}, "")
	scrubControl(t, pdf, textTok, popupTok)
	ctl := HiddenContent{NonLinkAnnotations: 2}
	d, err := OpenStream(pdf)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if h := scrubHidden(t, d); h != ctl {
		t.Errorf("before scrub: %+v, want %+v", h, ctl)
	}
	for _, opts := range []ScrubOptions{{}, {KeepAnnotations: true}} {
		t.Run(fmt.Sprintf("%+v", opts), func(t *testing.T) {
			re := scrubCheckKept(t, pdf, opts)
			out, err := re.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			for _, tok := range []string{textTok, popupTok} {
				if got := bytes.Contains(out, []byte(tok)); got != opts.KeepAnnotations {
					t.Errorf("%q in save = %v, want %v", tok, got, opts.KeepAnnotations)
				}
			}
			if n := len(scrubLinkRe.FindAll(out, -1)); n != 1 {
				t.Errorf("%d Links in save, want 1", n)
			}
			want := HiddenContent{}
			if opts.KeepAnnotations {
				want = ctl
			}
			if h := scrubHidden(t, re); h != want {
				t.Errorf("after scrub: %+v, want %+v", h, want)
			}
		})
	}
}

// KeepAnnotations keeps media annotations with their embedded media (a
// Screen's rendition and media clip, a Sound's sound stream) and strips their
// scripts: the Screen's /AA and a Rendition action carrying /JS.
func TestScrubKeepAnnotationsMedia(t *testing.T) {
	const mediaTok, soundTok, jsTok, aaTok = "GomupdfScreenMedia2d8f", "GomupdfSoundData7c1a", "GomupdfRenditionJS4b6e", "GomupdfScreenAA9e3d"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Annots [4 0 R 5 0 R 6 0 R] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /Screen /Rect [0 0 100 100] /AA << /PO << /S /JavaScript /JS (var ` + aaTok + `;) >> >>
   /A << /S /Rendition /OP 0 /AN 4 0 R /R << /S /MR /C << /S /MCD /D << /Type /Filespec /F (m.mp4) /EF << /F 7 0 R >> >> /CT (video/mp4) >> >> >> >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Screen /Rect [100 0 200 100] /A << /S /Rendition /OP 0 /AN 5 0 R /JS (var ` + jsTok + `;) >> >>`,
		/* 6 */ `<< /Type /Annot /Subtype /Sound /Rect [200 0 220 20] /Sound 8 0 R >>`,
		/* 7 */ "/Type /EmbeddedFile\nstream\n" + mediaTok,
		/* 8 */ "/Type /Sound /R 8000\nstream\n" + soundTok,
	}, "")
	scrubControl(t, pdf, mediaTok, soundTok, jsTok, aaTok)
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{KeepAnnotations: true})
	scrubNoTokens(t, "KeepAnnotations", out, jsTok, aaTok)
	for _, tok := range []string{mediaTok, soundTok} {
		if !bytes.Contains(out, []byte(tok)) {
			t.Errorf("%q removed", tok)
		}
	}
	if h := scrubHidden(t, re); h != (HiddenContent{NonLinkAnnotations: 3}) {
		t.Errorf("after scrub: %+v, want the three annotations", h)
	}
}

// A page tree in which MuPDF cannot look up some page does not make
// HiddenContent or Scrub fail: the pages MuPDF serves are checked and
// scrubbed, and a page dictionary it never serves is an orphan page. A null
// /Kids entry (which MuPDF's page lookup reports as a cycle, although the
// pages after it are served) hides nothing; a /Kids entry that is the tree's
// own root (a real cycle) stops MuPDF serving the pages after it, which are
// then reported and emptied. (Reduced from the Garbage 1 save of qpdf
// xref-compressed-in-compressed.pdf, whose damaged /Kids entry the save
// writes as null.)
func TestScrubUnservedPageInTree(t *testing.T) {
	const tok = "GomupdfServedPageTwo3f7a"
	page := func(s string) string {
		return `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 5 0 R >> >> /Contents ` + s + ` >>`
	}
	for _, c := range []struct {
		name, kids string
		served     bool // page 2 is served
	}{{"null kid", "[3 0 R null 6 0 R] /Count 3", true}, {"root as kid", "[3 0 R 2 0 R 6 0 R] /Count 3", false}} {
		name, kids := c.name, c.kids
		t.Run(name, func(t *testing.T) {
			pdf := scrubBuildPDF([]string{
				/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
				/* 2 */ `<< /Type /Pages /Kids ` + kids + ` >>`,
				/* 3 */ page("4 0 R"),
				/* 4 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
				/* 5 */ `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
				/* 6 */ page("7 0 R"),
				/* 7 */ "\nstream\nBT /F1 12 Tf 20 150 Td (" + tok + ") Tj ET",
			}, "/Info << /Title (x) >>")
			d, err := OpenStream(pdf)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			want := HiddenContent{Info: true}
			if !c.served {
				want.OrphanPages = 1
			}
			if h, err := d.HiddenContent(); err != nil || h != want {
				t.Errorf("before scrub: %+v, %v; want %+v", h, err, want)
			}
			re := scrubCheckKept(t, pdf, ScrubOptions{})
			if h, err := re.HiddenContent(); err != nil || h != (HiddenContent{}) {
				t.Errorf("after scrub: %+v, %v", h, err)
			}
			out, err := re.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Fatal(err)
			}
			if got := bytes.Contains(out, []byte(tok)); got != c.served {
				t.Errorf("page 2 text in save = %v, want %v", got, c.served)
			}
			if !c.served {
				return
			}
			p, err := re.LoadPage(2)
			if err != nil {
				t.Fatal(err)
			}
			if s, err := p.GetText(); err != nil || !strings.Contains(s, tok) {
				t.Errorf("page 2 after scrub: %q, %v", s, err)
			}
		})
	}
}

// A page count larger than the file's number of objects is impossible: Open,
// OpenStream and OpenAnyStream refuse it (it would make MuPDF allocate a page
// map for every declared page, 11 GB for pdf.js poppler-67295-0.pdf's /Count
// 1410065407), and HiddenContent and Scrub, on a document opened without
// that check, refuse it at once rather than look up every declared page. A
// count equal to the number of objects (one page listed in every /Kids
// entry) opens.
func TestScrubImpossiblePageCount(t *testing.T) {
	pdf := scrubBuildPDF([]string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1048576 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`,
	}, "")
	const msg = "page tree declares 1048576 pages but the file has only 4 objects"
	path := filepath.Join(t.TempDir(), "count.pdf")
	if err := os.WriteFile(path, pdf, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, open := range map[string]func() (*Document, error){
		"Open":          func() (*Document, error) { return Open(path) },
		"OpenStream":    func() (*Document, error) { return OpenStream(pdf) },
		"OpenAnyStream": func() (*Document, error) { return OpenAnyStream(pdf, "pdf") },
	} {
		if d, err := open(); err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: %v, want %q", name, err, msg)
			if d != nil {
				d.Close()
			}
		}
	}
	if d, err := OpenStream(scrubBuildPDF([]string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R 3 0 R 3 0 R 3 0 R] /Count 4 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`,
	}, "")); err != nil {
		t.Errorf("a count equal to the number of objects: %v", err)
	} else {
		if n := d.PageCount(); n != 4 {
			t.Errorf("PageCount %d, want 4", n)
		}
		d.Close()
	}

	d := openLazy(t, pdf)
	defer d.Close()
	start := time.Now()
	if _, err := d.HiddenContent(); err == nil || !strings.Contains(err.Error(), "declares 1048576 pages") {
		t.Errorf("HiddenContent: %v", err)
	}
	if err := d.Scrub(ScrubOptions{}); err == nil || !strings.Contains(err.Error(), "declares 1048576 pages") {
		t.Errorf("Scrub: %v", err)
	}
	if el := time.Since(start); el > time.Second {
		t.Errorf("took %v", el)
	}
}

// A kept Link's /IRT does not keep the annotation it points at: the Square
// the default options remove is destroyed and its /Contents leaves the file.
func TestScrubKeptLinkIRT(t *testing.T) {
	const tok = "GomupdfIRTSquare6c1e"
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [4 0 R 5 0 R] >>`,
		/* 4 */ `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /IRT 5 0 R /A << /S /URI /URI (https://example.com/) >> >>`,
		/* 5 */ `<< /Type /Annot /Subtype /Square /Rect [60 60 120 120] /Contents (` + tok + `) >>`,
	}, "")
	scrubControl(t, pdf, tok)
	out, re := scrubRoundTrip(t, pdf, ScrubOptions{})
	scrubNoTokens(t, "default", out, tok)
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("after scrub: %+v", h)
	}
	p, _ := re.LoadPage(0)
	if l, err := p.Links(); err != nil || len(l) != 1 {
		t.Errorf("links after scrub = %+v, %v; want the URI link", l, err)
	}
}
