package gomupdf

import (
	"os"
	"strings"
	"testing"
)

// actualTextPage draws "VISIBLE" inside a span whose /ActualText declares
// "DECLARED", the shape a hostile or sloppy producer uses to feed a text
// extractor something other than what is on the page.
func actualTextPage(t *testing.T) *Page {
	t.Helper()
	d := onePagePDF(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"/Span << /ActualText (DECLARED) >> BDC\nBT /F1 24 Tf 20 100 Td (VISIBLE) Tj ET\nEMC")
	t.Cleanup(func() { d.Close() })
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func checkActual(t *testing.T, api, got string, err error, declared bool) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v", api, err)
	}
	if declared {
		if !strings.Contains(got, "DECLARED") {
			t.Errorf("%s with UseActualText: %q lacks DECLARED", api, got)
		}
		return
	}
	if strings.Contains(got, "DECLARED") || !strings.Contains(got, "VISIBLE") {
		t.Errorf("%s by default: %q, want VISIBLE and no DECLARED", api, got)
	}
}

// TestActualTextCrashInput documents a MuPDF bug reproduced on 1.26.11: with
// /ActualText honoured, the stext device (flush_actualtext, dev->last.font ==
// NULL) dereferences NULL and the process dies with SIGSEGV when the glyphs
// match a suffix of the declared text and the span is the first text on the
// page. It is skipped because it kills the test binary. Set
// GOMUPDF_TEST_ACTUALTEXT_CRASH=1 after a MuPDF upgrade to see whether it is
// fixed; if it passes, update the UseActualText warning.
func TestActualTextCrashInput(t *testing.T) {
	if os.Getenv("GOMUPDF_TEST_ACTUALTEXT_CRASH") != "1" {
		t.Skip("MuPDF 1.26.11 segfaults on this input with UseActualText; set GOMUPDF_TEST_ACTUALTEXT_CRASH=1 to retry on a newer MuPDF")
	}
	d := onePagePDF(t, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"/Span <</ActualText (AB)>> BDC BT /F1 12 Tf 50 100 Td (B) Tj ET EMC")
	defer d.Close()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ExtractText(TextOptions{UseActualText: true}); err != nil {
		t.Fatal(err)
	}
}

func TestActualTextIgnoredByDefault(t *testing.T) {
	p := actualTextPage(t)

	for _, declared := range []bool{false, true} {
		opts := TextOptions{UseActualText: declared}
		s, err := p.ExtractText(opts)
		checkActual(t, "ExtractText", s, err, declared)
		blocks, err := p.RawDictWithOptions(opts)
		checkActual(t, "RawDictWithOptions", pageChars(blocks), err, declared)
	}

	blocks, err := p.RawDict()
	checkActual(t, "RawDict", pageChars(blocks), err, false)
	s, err := p.GetText()
	checkActual(t, "GetText", s, err, false)
	lines, err := p.Lines()
	checkActual(t, "Lines", strings.Join(lines, "\n"), err, false)
	words, err := p.Words()
	var ws []string
	for _, w := range words {
		ws = append(ws, w.Text)
	}
	checkActual(t, "Words", strings.Join(ws, " "), err, false)
	s, err = p.StructuredJSON()
	checkActual(t, "StructuredJSON", s, err, false)
	sb, err := p.StructuredText()
	var st []string
	for _, b := range sb {
		for _, sp := range b.Spans {
			st = append(st, sp.Text)
		}
	}
	checkActual(t, "StructuredText", strings.Join(st, " "), err, false)
	s, err = p.HTML()
	checkActual(t, "HTML", s, err, false)
	s, err = p.XHTML()
	checkActual(t, "XHTML", s, err, false)
	// MuPDF 1.25 writes XML one <char c="V"/> element per character, so the
	// literal word never appears; compare the characters instead.
	s, err = p.XML()
	if err != nil {
		t.Fatalf("XML: %v", err)
	}
	var xmlChars strings.Builder
	for _, part := range strings.Split(s, ` c="`)[1:] {
		xmlChars.WriteString(part[:strings.IndexByte(part, '"')])
	}
	if got := xmlChars.String(); got != "VISIBLE" || strings.Contains(s, "DECLARED") {
		t.Errorf("XML by default: chars %q, want VISIBLE and no DECLARED in %q", got, s)
	}

	for _, needle := range []string{"VISIBLE", "DECLARED"} {
		q, err := p.Search(needle)
		if err != nil {
			t.Fatal(err)
		}
		if (len(q) > 0) != (needle == "VISIBLE") {
			t.Errorf("Search(%q): %d hits", needle, len(q))
		}
	}
	r, err := p.SearchRects("DECLARED")
	if err != nil || len(r) != 0 {
		t.Errorf("SearchRects(DECLARED) = %v, %v", r, err)
	}
	// FindTables works from Words, covered above; it must not see DECLARED
	// either, but a one-line page has no table to assert on.
}
