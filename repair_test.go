//go:build !nomupdf

package gomupdf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// Objects of the repair fixtures: "SECRET public" on a tagged page, with
// /Info (6) and a structure element (8) that only the page's /StructParents
// reaches. The content stream has no marked content, so extraction reads
// neither.
const (
	repairInfo = 6
	repairElem = 8
)

func repairObjects() []string {
	return []string{
		/* 1 */ `<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 7 0 R >>`,
		/* 2 */ `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		/* 3 */ `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R /StructParents 0 >>`,
		/* 4 */ helvetica,
		/* 5 */ "\nstream\nBT /F1 24 Tf 20 100 Td (SECRET public) Tj ET",
		/* 6 */ `<< /Title (Repair fixture) >>`,
		/* 7 */ `<< /Type /StructTreeRoot /K [8 0 R] /ParentTree << /Nums [0 [8 0 R]] >> >>`,
		/* 8 */ `<< /Type /StructElem /S /P /P 7 0 R /Pg 3 0 R /K 0 >>`,
	}
}

// pointAtFirst makes the xref entry of object bad point at object 1, so
// MuPDF repairs the xref the first time it loads bad.
func pointAtFirst(pdf []byte, bad int) []byte {
	x := bytes.LastIndex(pdf, []byte("\nxref\n")) + len("\nxref\n")
	x += bytes.IndexByte(pdf[x:], '\n') + 1
	copy(pdf[x+20*bad:x+20*(bad+1)], pdf[x+20:x+40])
	return pdf
}

// repairPDF is the fixture with object bad damaged: /Info is first loaded by
// a save, the structure element by ApplyRedactions' check of the structure
// tree (as in the corpus files test_4790.pdf and issue18503.pdf).
func repairPDF(bad int) []byte {
	return pointAtFirst(scrubBuildPDF(repairObjects(), "/Info 6 0 R"), bad)
}

// noXrefPDF is the fixture's objects with neither xref table nor trailer, so
// MuPDF repairs it on open.
func noXrefPDF() []byte {
	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	for i, body := range repairObjects() {
		if dict, data, ok := strings.Cut(body, "\nstream\n"); ok {
			body = fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
		}
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	return b.Bytes()
}

// noPagesPDF has an xref that covers only objects 0 to 2, with object 2
// pointing at object 1: until something loads object 2 and MuPDF repairs
// the xref, the page tree (object 3) is out of range and the document has no
// pages (as in the corpus files GHOSTSCRIPT-698804-1-fuzzed.pdf and
// issue18986.pdf).
func noPagesPDF() []byte {
	pdf := scrubBuildPDF([]string{
		/* 1 */ `<< /Type /Catalog /Pages 3 0 R /Outlines 2 0 R >>`,
		/* 2 */ `<< /Type /Outlines /Count 0 >>`,
		/* 3 */ `<< /Type /Pages /Kids [4 0 R] /Count 1 >>`,
		/* 4 */ `<< /Type /Page /Parent 3 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 6 0 R >> >> /Contents 5 0 R >>`,
		/* 5 */ "\nstream\nBT /F1 24 Tf 20 100 Td (SECRET public) Tj ET",
		/* 6 */ helvetica,
	}, "")
	x := bytes.LastIndex(pdf, []byte("\nxref\n")) + len("\nxref\n")
	tail := fmt.Sprintf("0 3\n%s%s%strailer\n<< /Size 3 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		pdf[x+len("0 7\n"):x+len("0 7\n")+20], pdf[x+len("0 7\n")+20:x+len("0 7\n")+40],
		pdf[x+len("0 7\n")+20:x+len("0 7\n")+40], x-len("xref\n"))
	return append(pdf[:x:x], tail...)
}

// openLazy opens pdf without loading every object first, as Open did before
// it stabilised documents: a repair can then still happen later.
func openLazy(t *testing.T, pdf []byte) *Document {
	t.Helper()
	b, _, err := mupdfDriver{lazy: true}.open(pdf, ".pdf")
	if err != nil {
		t.Fatal(err)
	}
	return &Document{b: b}
}

func docRepaired(d *Document) bool { return d.b.(*mupdfDoc).repaired() }

// redactSecret redacts "SECRET" on page 0 of d by its quads or by the union
// of its boxes. It returns the centre of "E" in page space.
func redactSecret(t *testing.T, d *Document, quads bool) (geometry.Point, error) {
	t.Helper()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	cs := pageGlyphs(t, p)
	if len(cs) < 6 || runesOf(cs[:6]) != "SECRET" {
		t.Fatalf("page glyphs = %q, want SECRET first", runesOf(cs))
	}
	cs = cs[:6]
	e := cs[1].BBox
	mid := geometry.Point{X: (e.X0 + e.X1) / 2, Y: (e.Y0 + e.Y1) / 2}
	if quads {
		var qs []geometry.Quad
		for _, c := range cs {
			qs = append(qs, c.Quad)
		}
		err = p.AddRedactionQuads(qs, RedactOptions{})
	} else {
		r := cs[0].BBox
		for _, c := range cs {
			r = r.IncludeRect(c.BBox)
		}
		err = p.AddRedaction(r, RedactOptions{})
	}
	if err != nil {
		return mid, err
	}
	n, err := p.ApplyRedactions(ApplyOptions{})
	if err == nil && n != 1 {
		t.Fatalf("ApplyRedactions = %d, want 1", n)
	}
	return mid, err
}

// checkRedacted saves d, reopens it and checks that "SECRET" is gone,
// "public" stays and the box covers mid.
func checkRedacted(t *testing.T, d *Document, mid geometry.Point) {
	t.Helper()
	out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	re, err := OpenStream(out)
	if err != nil {
		t.Fatal(err)
	}
	defer re.Close()
	if txt := pageText(t, re); strings.Contains(txt, "SECRET") || !strings.Contains(txt, "public") {
		t.Errorf("saved text = %q, want public without SECRET", txt)
	}
	p, err := re.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if got := px(t, p, int(mid.X), int(mid.Y)); got != black {
		t.Errorf("saved page at %v = %v, want the black box", mid, got)
	}
}

// A file MuPDF would repair late, when a save or the redaction's own
// structure check first loads the damaged object, is repaired on open and
// redacted for real.
func TestRedactRepairedFile(t *testing.T) {
	for _, bad := range []int{repairInfo, repairElem} {
		pdf := repairPDF(bad)
		// The fixture needs a repair that neither opening nor extraction
		// makes, and the repair does not change what extraction reads.
		lazy := openLazy(t, pdf)
		text := pageText(t, lazy)
		if docRepaired(lazy) {
			t.Fatalf("object %d: fixture repaired on open or extraction", bad)
		}
		if err := lazy.b.(*mupdfDoc).stabilise(); err != nil || !docRepaired(lazy) {
			t.Fatalf("object %d: stabilise = %v, repaired %v; want a repair", bad, err, docRepaired(lazy))
		}
		if got := pageText(t, lazy); got != text {
			t.Errorf("object %d: text after repair = %q, before %q", bad, got, text)
		}
		lazy.Close()

		for _, quads := range []bool{true, false} {
			t.Run(fmt.Sprintf("object %d quads %v", bad, quads), func(t *testing.T) {
				d, err := OpenStream(pdf)
				if err != nil {
					t.Fatal(err)
				}
				defer d.Close()
				if !docRepaired(d) {
					t.Fatal("OpenStream did not repair the file")
				}
				mid, err := redactSecret(t, d, quads)
				if err != nil {
					t.Fatal(err)
				}
				checkRedacted(t, d, mid)
			})
		}
	}
}

// A file with no xref table or trailer is repaired on open, before any
// change, and redacted as usual.
func TestRedactNoXrefFile(t *testing.T) {
	for _, quads := range []bool{true, false} {
		d, err := OpenStream(noXrefPDF())
		if err != nil {
			t.Fatal(err)
		}
		if !docRepaired(d) {
			t.Fatal("no-xref fixture not repaired on open")
		}
		mid, err := redactSecret(t, d, quads)
		if err != nil {
			t.Fatal(err)
		}
		checkRedacted(t, d, mid)
		d.Close()
	}
}

// A file whose page tree MuPDF finds only by repairing it has its page on
// open, by path and from bytes, so the page is read and redacted.
func TestOpenRepairsPageTree(t *testing.T) {
	pdf := noPagesPDF()
	lazy := openLazy(t, pdf)
	if n := lazy.PageCount(); n != 0 || docRepaired(lazy) {
		t.Fatalf("fixture: %d pages, repaired %v; want 0 pages before the repair", n, docRepaired(lazy))
	}
	lazy.Close()

	path := filepath.Join(t.TempDir(), "nopages.pdf")
	if err := os.WriteFile(path, pdf, 0o600); err != nil {
		t.Fatal(err)
	}
	dp, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer dp.Close()
	if n := dp.PageCount(); n != 1 {
		t.Errorf("Open: %d pages, want 1", n)
	}
	for _, quads := range []bool{true, false} {
		d, err := OpenStream(pdf)
		if err != nil {
			t.Fatal(err)
		}
		if n := d.PageCount(); n != 1 {
			t.Fatalf("OpenStream: %d pages, want 1", n)
		}
		if txt := pageText(t, d); !strings.Contains(txt, "SECRET public") {
			t.Errorf("text = %q", txt)
		}
		mid, err := redactSecret(t, d, quads)
		if err != nil {
			t.Fatal(err)
		}
		checkRedacted(t, d, mid)
		d.Close()
	}
}

// If MuPDF still repairs the xref during a redaction or a save (here forced
// by opening without loading every object and marking the document as
// loaded), the call fails rather than report success for changes the repair
// discarded.
func TestRedactLateRepairFails(t *testing.T) {
	for _, quads := range []bool{true, false} {
		d := openLazy(t, repairPDF(repairElem))
		d.b.(*mupdfDoc).stable = true
		if _, err := redactSecret(t, d, quads); err == nil || !strings.Contains(err.Error(), "must not be treated as redacted") {
			t.Errorf("quads %v: ApplyRedactions error = %v, want the repair error", quads, err)
		}
		d.Close()

		d = openLazy(t, repairPDF(repairInfo))
		d.b.(*mupdfDoc).stable = true
		if _, err := redactSecret(t, d, quads); err != nil {
			t.Fatal(err)
		}
		if _, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 1}); err == nil || !strings.Contains(err.Error(), "nothing was saved") {
			t.Errorf("quads %v: save error = %v, want the repair error", quads, err)
		}
		d.Close()
	}
}

// Every change, not only redaction, is made after the repair: a link or a
// page added to a file that a save would repair is in the saved file.
func TestMutateRepairedFile(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Document) error
		check  func(*Document) error
	}{
		{"InsertLink", func(d *Document) error {
			p, err := d.LoadPage(0)
			if err != nil {
				return err
			}
			return p.InsertLink(geometry.Rect{X0: 10, Y0: 10, X1: 50, Y1: 30}, "https://example.com/")
		}, func(d *Document) error {
			p, err := d.LoadPage(0)
			if err != nil {
				return err
			}
			if l, err := p.Links(); err != nil || len(l) != 1 {
				return fmt.Errorf("links = %+v, %v; want one", l, err)
			}
			return nil
		}},
		{"NewPage", func(d *Document) error { return d.NewPage(100, 100) }, func(d *Document) error {
			if n := d.PageCount(); n != 2 {
				return fmt.Errorf("%d pages, want 2", n)
			}
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, err := OpenStream(repairPDF(repairInfo))
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if err := tc.mutate(d); err != nil {
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
			defer re.Close()
			if err := tc.check(re); err != nil {
				t.Error(err)
			}
		})
	}
}
