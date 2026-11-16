//go:build !nomupdf

package gomupdf

/*
#include <mupdf/fitz.h>
*/
import "C"

// MuPDFVersion returns the MuPDF version (FZ_VERSION, for example "1.25.6")
// of the headers this binary was compiled against. Extraction output depends
// on the MuPDF version, so record it alongside any output you persist.
//
// New in the tredy-ai fork; upstream has no equivalent. In a build with
// -tags nomupdf it returns "".
func MuPDFVersion() string { return C.FZ_VERSION }
