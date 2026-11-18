package gomupdf

import (
	"testing"
)

// newThreePageDoc creates a fresh in-memory PDF with 3 blank pages (300x300).
func newThreePageDoc(t *testing.T) *Document {
	t.Helper()
	d, err := NewPDF()
	if err != nil {
		t.Fatalf("NewPDF: %v", err)
	}
	for i := 0; i < 3; i++ {
		if err := d.NewPage(300, 300); err != nil {
			d.Close()
			t.Fatalf("NewPage %d: %v", i, err)
		}
	}
	return d
}

// saveAndReopen saves d to bytes and reopens a new Document from them.
func saveAndReopen(t *testing.T, d *Document) *Document {
	t.Helper()
	data, err := d.SaveBytes(true)
	if err != nil {
		t.Fatalf("SaveBytes: %v", err)
	}
	re, err := OpenStream(data)
	if err != nil {
		t.Fatalf("OpenStream: %v", err)
	}
	return re
}

func TestSetTOCRoundTrip(t *testing.T) {
	d := newThreePageDoc(t)
	defer d.Close()

	entries := []TOCEntry{
		{Level: 1, Title: "Chapter 1", Page: 0},
		{Level: 2, Title: "Section 1.1", Page: 1},
		{Level: 1, Title: "Chapter 2", Page: 2},
	}
	if err := d.SetTOC(entries); err != nil {
		t.Fatalf("SetTOC: %v", err)
	}

	re := saveAndReopen(t, d)
	defer re.Close()

	got, err := re.TOC()
	if err != nil {
		t.Fatalf("TOC: %v", err)
	}
	if len(got) != len(entries) {
		t.Fatalf("TOC length = %d, want %d; got %+v", len(got), len(entries), got)
	}
	for i, want := range entries {
		if got[i].Level != want.Level {
			t.Errorf("entry[%d].Level = %d, want %d", i, got[i].Level, want.Level)
		}
		if got[i].Title != want.Title {
			t.Errorf("entry[%d].Title = %q, want %q", i, got[i].Title, want.Title)
		}
		if got[i].Page != want.Page {
			t.Errorf("entry[%d].Page = %d, want %d", i, got[i].Page, want.Page)
		}
	}
}

func TestSetTOCThreeLevel(t *testing.T) {
	d := newThreePageDoc(t)
	defer d.Close()

	entries := []TOCEntry{
		{Level: 1, Title: "Part I", Page: 0},
		{Level: 2, Title: "Chapter 1", Page: 0},
		{Level: 3, Title: "Section 1.1", Page: 1},
		{Level: 1, Title: "Part II", Page: 2},
	}
	if err := d.SetTOC(entries); err != nil {
		t.Fatalf("SetTOC: %v", err)
	}

	re := saveAndReopen(t, d)
	defer re.Close()

	got, err := re.TOC()
	if err != nil {
		t.Fatalf("TOC: %v", err)
	}
	if len(got) != len(entries) {
		t.Fatalf("TOC length = %d, want %d; got %+v", len(got), len(entries), got)
	}
	for i, want := range entries {
		if got[i].Level != want.Level || got[i].Title != want.Title || got[i].Page != want.Page {
			t.Errorf("entry[%d] = {%d, %q, %d}, want {%d, %q, %d}",
				i, got[i].Level, got[i].Title, got[i].Page,
				want.Level, want.Title, want.Page)
		}
	}
}

func TestSetTOCNilRemoves(t *testing.T) {
	d := newThreePageDoc(t)
	defer d.Close()

	// First install a TOC, then remove it.
	if err := d.SetTOC([]TOCEntry{{1, "Chapter 1", 0}}); err != nil {
		t.Fatalf("SetTOC set: %v", err)
	}
	if err := d.SetTOC(nil); err != nil {
		t.Fatalf("SetTOC nil: %v", err)
	}

	re := saveAndReopen(t, d)
	defer re.Close()

	got, err := re.TOC()
	if err != nil {
		t.Fatalf("TOC: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty TOC after nil, got %+v", got)
	}
}

func TestSetTOCOutOfRangePageClamps(t *testing.T) {
	d := newThreePageDoc(t) // 3 pages: 0,1,2
	defer d.Close()

	entries := []TOCEntry{
		{Level: 1, Title: "Too High", Page: 99}, // should clamp to 2
		{Level: 1, Title: "Too Low", Page: -5},  // should clamp to 0
	}
	if err := d.SetTOC(entries); err != nil {
		t.Fatalf("SetTOC: %v", err)
	}

	re := saveAndReopen(t, d)
	defer re.Close()

	got, err := re.TOC()
	if err != nil {
		t.Fatalf("TOC: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("TOC length = %d, want 2; got %+v", len(got), got)
	}
	if got[0].Page != 2 {
		t.Errorf("clamped-high page = %d, want 2", got[0].Page)
	}
	if got[1].Page != 0 {
		t.Errorf("clamped-low page = %d, want 0", got[1].Page)
	}
}

func TestSetTOCClosedDoc(t *testing.T) {
	d := newThreePageDoc(t)
	d.Close()

	err := d.SetTOC([]TOCEntry{{1, "test", 0}})
	if err == nil {
		t.Fatal("expected error for closed document, got nil")
	}
}

// SetTOC, SetPageLabels and SetMetadata parse their input with strtok_r:
// strtok keeps one hidden position for the whole process, so documents
// edited from several goroutines at once lost or mixed entries (a
// 100,000-entry SetTOC kept about 60).
func TestSetSpecsConcurrently(t *testing.T) {
	toc := make([]TOCEntry, 20000)
	for i := range toc {
		toc[i] = TOCEntry{Level: 1, Title: "t", Page: i % 3}
	}
	labels := []PageLabel{{StartPage: 0, Style: LabelDecimal, Prefix: "a", Start: 1},
		{StartPage: 1, Style: LabelDecimal, Prefix: "b", Start: 1}, {StartPage: 2, Style: LabelDecimal, Prefix: "c", Start: 1}}
	meta := map[string]string{"title": "T", "author": "A", "subject": "S", "keywords": "K", "creator": "C", "producer": "P"}
	scrubConcurrently(func() {
		for range 20 {
			d := newThreePageDoc(t)
			if err := d.SetTOC(toc); err != nil {
				t.Error(err)
			}
			if err := d.SetPageLabels(labels); err != nil {
				t.Error(err)
			}
			if err := d.SetMetadata(meta); err != nil {
				t.Error(err)
			}
			if got, err := d.TOC(); err != nil || len(got) != len(toc) {
				t.Errorf("TOC has %d entries, want %d (%v)", len(got), len(toc), err)
			}
			if got, err := d.PageLabels(); err != nil || len(got) != len(labels) {
				t.Errorf("page labels %+v, want %d rules (%v)", got, len(labels), err)
			}
			if got, err := d.Metadata(); err != nil || got["title"] != "T" || got["producer"] != "P" {
				t.Errorf("metadata %v (%v)", got, err)
			}
			d.Close()
		}
	})
}
