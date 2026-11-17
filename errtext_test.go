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
