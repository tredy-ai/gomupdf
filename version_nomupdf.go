//go:build nomupdf

package gomupdf

// MuPDFVersion returns "" because the nomupdf build links no MuPDF.
func MuPDFVersion() string { return "" }
