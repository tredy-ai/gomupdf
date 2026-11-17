//go:build !nomupdf

package gomupdf

import (
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
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
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
