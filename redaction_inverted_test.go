package gomupdf

import (
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// A rect inverted in x, in y or in both redacts what the normal rect does:
// MuPDF normalises it.
func TestAddRedactionInvertedRect(t *testing.T) {
	r := secretRect
	redacted := func(rect geometry.Rect) string {
		t.Helper()
		d, p := newRedactDoc(t)
		defer d.Close()
		if err := p.AddRedaction(rect, RedactOptions{}); err != nil {
			t.Fatalf("AddRedaction(%v): %v", rect, err)
		}
		if n, err := p.ApplyRedactions(ApplyOptions{}); err != nil || n != 1 {
			t.Fatalf("ApplyRedactions = %d, %v", n, err)
		}
		txt, err := p.GetText()
		if err != nil {
			t.Fatal(err)
		}
		return txt
	}
	want := redacted(r)
	if strings.Contains(want, "SECRET") {
		t.Fatalf("normal rect leaves %q", want)
	}
	for _, c := range []struct {
		name string
		rect geometry.Rect
	}{
		{"x", geometry.Rect{X0: r.X1, Y0: r.Y0, X1: r.X0, Y1: r.Y1}},
		{"y", geometry.Rect{X0: r.X0, Y0: r.Y1, X1: r.X1, Y1: r.Y0}},
		{"both", geometry.Rect{X0: r.X1, Y0: r.Y1, X1: r.X0, Y1: r.Y0}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := redacted(c.rect); got != want {
				t.Errorf("text after redaction = %q, want %q (as the normal rect)", got, want)
			}
		})
	}
}
