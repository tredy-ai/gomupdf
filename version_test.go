//go:build !nomupdf

package gomupdf

import (
	"strings"
	"testing"
)

func TestMuPDFVersion(t *testing.T) {
	v := MuPDFVersion()
	if v == "" || !strings.HasPrefix(v, "1.") {
		t.Fatalf("MuPDFVersion() = %q, want non-empty with prefix \"1.\"", v)
	}
}
