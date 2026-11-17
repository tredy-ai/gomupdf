//go:build !nomupdf

package gomupdf

import (
	"strings"
	"testing"
)

// A C helper that fails without writing a message (as the save helpers did
// when their output buffer could not be allocated) yields a message, not
// "gomupdf: save: ". The buffer is not freed: tests cannot call C.free.
func TestErrTextNeverEmpty(t *testing.T) {
	if got := errText(newErrBuf()); !strings.HasPrefix(got, "unknown error") {
		t.Errorf("errText of an untouched buffer = %q, want the unknown-error message", got)
	}
}

// ExtractFont tells a MuPDF error (here an object out of range) from a font
// with no embedded file by the helper's status, not by whether the message
// is empty.
func TestExtractFontErrorIsError(t *testing.T) {
	d, err := OpenStream(pdfPage("", "", "BT /F1 12 Tf 20 100 Td (a) Tj ET"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, _, data, err := d.ExtractFont(1 << 20); err == nil || data != nil {
		t.Errorf("ExtractFont(out of range) = %d bytes, %v; want an error", len(data), err)
	}
	if _, _, data, err := d.ExtractFont(4); err != nil || data != nil {
		t.Errorf("ExtractFont(non-embedded Helvetica) = %d bytes, %v; want nothing, no error", len(data), err)
	}
}
