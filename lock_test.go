//go:build !nomupdf

package gomupdf

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// lockedPDF is a one-page PDF with an /Info /Title, encrypted with user
// password "u" and owner password "o".
func lockedPDF(t *testing.T) []byte {
	t.Helper()
	d, err := NewPDF()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.NewPage(300, 200); err != nil {
		t.Fatal(err)
	}
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.InsertText(20, 100, 24, "SECRET"); err != nil {
		t.Fatal(err)
	}
	if err := d.SetMetadata(map[string]string{"title": "Locked fixture title"}); err != nil {
		t.Fatal(err)
	}
	out, err := d.SaveBytesWithOptions(SaveOptions{Encrypt: true, UserPassword: "u", OwnerPassword: "o"})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// A locked document is neither loaded nor changed before Authenticate: the
// change fails, and the document reads correctly once unlocked. Loading
// every object first would cache strings decrypted with the wrong key.
func TestLockedDocumentRefusesChanges(t *testing.T) {
	d, err := OpenStream(lockedPDF(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if !d.NeedsPass() {
		t.Fatal("fixture not locked")
	}
	p := &Page{doc: d, Number: 0} // LoadPage refuses a locked document
	locked := func(what string, err error) {
		t.Helper()
		if err == nil || !strings.Contains(err.Error(), "locked") {
			t.Errorf("%s on a locked document: %v, want the locked error", what, err)
		}
	}
	locked("AddRedaction", p.AddRedaction(geometry.Rect{X0: 10, Y0: 10, X1: 50, Y1: 50}, RedactOptions{}))
	_, err = p.ApplyRedactions(ApplyOptions{})
	locked("ApplyRedactions", err)
	locked("InsertLink", p.InsertLink(geometry.Rect{X0: 10, Y0: 10, X1: 50, Y1: 50}, "https://example.com/"))
	_, err = d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
	locked("SaveBytesWithOptions", err)

	if !d.Authenticate("u") {
		t.Fatal("Authenticate(u) = false")
	}
	if m, err := d.Metadata(); err != nil || m["title"] != "Locked fixture title" {
		t.Errorf("title after Authenticate = %q, %v", m["title"], err)
	}
	if err := p.AddRedaction(geometry.Rect{X0: 10, Y0: 10, X1: 50, Y1: 50}, RedactOptions{}); err != nil {
		t.Errorf("AddRedaction after Authenticate: %v", err)
	}

	d2, err := OpenStream(lockedPDF(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d2.Close()
	locked("SetMetadata", d2.SetMetadata(map[string]string{"title": "x"}))
	if !d2.Authenticate("u") {
		t.Fatal("Authenticate(u) = false")
	}
	if m, err := d2.Metadata(); err != nil || m["title"] != "Locked fixture title" {
		t.Errorf("title after a refused SetMetadata = %q, %v", m["title"], err)
	}
}

// lockedReadPDF is a one-page PDF encrypted with user password "u" and
// owner password "o" holding something for every read: text, an /Info
// /Title, XMP, an outline entry, a page label prefix, a link, a square
// annotation and a text field.
func lockedReadPDF(t *testing.T) []byte {
	t.Helper()
	d, err := NewPDF()
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.NewPage(300, 200); err != nil {
		t.Fatal(err)
	}
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	r := geometry.Rect{X0: 10, Y0: 10, X1: 50, Y1: 30}
	for _, err := range []error{
		p.InsertText(20, 100, 24, "SECRET"),
		d.SetMetadata(map[string]string{"title": "Locked fixture title"}),
		d.SetXMP("<x:xmpmeta xmlns:x='adobe:ns:meta/'>locked xmp</x:xmpmeta>"),
		d.SetTOC([]TOCEntry{{Level: 1, Title: "Chapter one", Page: 1}}),
		d.SetPageLabels([]PageLabel{{Style: LabelDecimal, Prefix: "Prefix-"}}),
		p.InsertLink(r, "https://example.com/locked"),
		p.AddRectAnnot(geometry.Rect{X0: 200, Y0: 10, X1: 250, Y1: 30}),
		p.AddTextField("field", geometry.Rect{X0: 200, Y0: 150, X1: 280, Y1: 170}, "field value"),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	out, err := d.SaveBytesWithOptions(SaveOptions{Encrypt: true, UserPassword: "u", OwnerPassword: "o"})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// Reading a locked document caches its objects decrypted with the wrong
// key: Metadata, or TOC, before Authenticate left the title garbled after.
// Every read now refuses until Authenticate and then reads correctly.
func TestLockedDocumentRefusesReads(t *testing.T) {
	data := lockedReadPDF(t)
	type read struct {
		name string
		call func(d *Document, p *Page) (string, error) // what it read, for the check after Authenticate
		want string                                     // a substring of it after Authenticate
	}
	str := func(v any, err error) (string, error) { return fmt.Sprintf("%+v", v), err }
	reads := []read{
		{"Metadata", func(d *Document, _ *Page) (string, error) { return str(d.Metadata()) }, "Locked fixture title"},
		{"XMP", func(d *Document, _ *Page) (string, error) { return d.XMP() }, "locked xmp"},
		{"TOC", func(d *Document, _ *Page) (string, error) { return str(d.TOC()) }, "Chapter one"},
		{"PageLabels", func(d *Document, _ *Page) (string, error) { return str(d.PageLabels()) }, "Prefix-"},
		{"Label", func(_ *Document, p *Page) (string, error) { return p.Label() }, "Prefix-1"},
		{"Text", func(d *Document, _ *Page) (string, error) { return d.Text() }, "SECRET"},
		{"TextByPage", func(d *Document, _ *Page) (string, error) { return str(d.TextByPage()) }, "SECRET"},
		{"AllLines", func(d *Document, _ *Page) (string, error) { return str(d.AllLines()) }, "SECRET"},
		{"GetText", func(_ *Document, p *Page) (string, error) { return p.GetText() }, "SECRET"},
		{"ExtractText", func(_ *Document, p *Page) (string, error) { return p.ExtractText(TextOptions{}) }, "SECRET"},
		{"Words", func(_ *Document, p *Page) (string, error) { return str(p.Words()) }, "SECRET"},
		{"RawDict", func(_ *Document, p *Page) (string, error) { return str(p.RawDict()) }, "Rune:83"},
		{"StructuredJSON", func(_ *Document, p *Page) (string, error) { return p.StructuredJSON() }, "SECRET"},
		{"HTML", func(_ *Document, p *Page) (string, error) { return p.HTML() }, "SECRET"},
		{"XHTML", func(_ *Document, p *Page) (string, error) { return p.XHTML() }, "SECRET"},
		// MuPDF 1.25 writes one element per character.
		{"XML", func(_ *Document, p *Page) (string, error) { return p.XML() }, `font name="Helvetica"`},
		{"Search", func(_ *Document, p *Page) (string, error) { return str(p.Search("SECRET")) }, "UL"},
		{"Bound", func(_ *Document, p *Page) (string, error) { return str(p.Bound()) }, "X1:300"},
		{"MediaBox", func(_ *Document, p *Page) (string, error) { return str(p.MediaBox()) }, "Y1:200"},
		{"GetFonts", func(_ *Document, p *Page) (string, error) { return str(p.GetFonts()) }, "Helvetica"},
		{"GetImages", func(_ *Document, p *Page) (string, error) { return str(p.GetImages()) }, "[]"},
		{"GetDrawings", func(_ *Document, p *Page) (string, error) { return str(p.GetDrawings()) }, "["},
		{"Links", func(_ *Document, p *Page) (string, error) { return str(p.Links()) }, "https://example.com/locked"},
		{"Annotations", func(_ *Document, p *Page) (string, error) { return str(p.Annotations()) }, "Square"},
		{"Widgets", func(_ *Document, p *Page) (string, error) { return str(p.Widgets()) }, "field value"},
		{"Pixmap", func(_ *Document, p *Page) (string, error) {
			px, err := p.Pixmap()
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("%dx%d", px.Width, px.Height), nil
		}, "300x200"},
		{"HiddenContent", func(d *Document, _ *Page) (string, error) { return str(d.HiddenContent()) }, "Outlines:true"},
		{"ConvertToPDF", func(d *Document, _ *Page) (string, error) {
			c, err := d.ConvertToPDF()
			if err != nil {
				return "", err
			}
			defer c.Close()
			return fmt.Sprint(c.PageCount()), nil
		}, "1"},
	}
	for _, r := range reads {
		t.Run(r.name, func(t *testing.T) {
			d, err := OpenStream(data)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			p := &Page{doc: d, Number: 0} // LoadPage refuses a locked document
			if _, err := r.call(d, p); !errors.Is(err, errLocked) {
				t.Errorf("before Authenticate: %v, want the locked error", err)
			}
			if !d.Authenticate("u") {
				t.Fatal("Authenticate(u) = false")
			}
			got, err := r.call(d, p)
			if err != nil || !strings.Contains(got, r.want) {
				t.Errorf("after Authenticate: %q, %v; want %q in it", got, err, r.want)
			}
			// The title is the canary for strings cached before.
			if m, err := d.Metadata(); err != nil || m["title"] != "Locked fixture title" {
				t.Errorf("title after Authenticate = %q, %v", m["title"], err)
			}
		})
	}
}

// Counting pages loads the catalog, and a string inline in it, so a locked
// document has no pages until Authenticate; then the page label prefix in
// the catalog reads correctly.
func TestLockedDocumentPageCount(t *testing.T) {
	src, err := OpenStream(buildPDF(
		"<< /Type /Catalog /Pages 2 0 R /PageLabels << /Nums [0 << /S /D /P (Prefix-) >>] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] >>",
	))
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	data, err := src.SaveBytesWithOptions(SaveOptions{Encrypt: true, UserPassword: "u", OwnerPassword: "o"})
	if err != nil {
		t.Fatal(err)
	}
	d, err := OpenStream(data)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if n := d.PageCount(); n != 0 {
		t.Errorf("PageCount while locked = %d, want 0", n)
	}
	if _, err := d.LoadPage(0); !errors.Is(err, errLocked) {
		t.Errorf("LoadPage while locked: %v, want the locked error", err)
	}
	for range d.Pages() {
		t.Error("Pages yielded a page while locked")
	}
	if !d.IsEncrypted() || !d.NeedsPass() {
		t.Error("IsEncrypted or NeedsPass false while locked")
	}
	if !d.Authenticate("u") {
		t.Fatal("Authenticate(u) = false")
	}
	if n := d.PageCount(); n != 1 {
		t.Errorf("PageCount after Authenticate = %d, want 1", n)
	}
	if l, err := d.PageLabels(); err != nil || len(l) != 1 || l[0].Prefix != "Prefix-" {
		t.Errorf("page labels after Authenticate = %+v, %v", l, err)
	}
}

// A repair made by the save's own load of every object (the document was
// not loaded first) drops the edits made before: the save fails rather than
// write the file without them.
func TestSaveReportsRepairOfItsOwnLoad(t *testing.T) {
	d := openLazy(t, repairPDF(repairInfo))
	defer d.Close()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.InsertLink(geometry.Rect{X0: 10, Y0: 10, X1: 50, Y1: 30}, "https://example.com/"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 1}); err == nil || !strings.Contains(err.Error(), "repaired") {
		t.Errorf("save = %v, want the repair error", err)
	}
}
