//go:build linux && !nomupdf && mupdf_bundled

package gomupdf

// Link flags for a MuPDF built from source with its bundled third-party code
// (libmupdf + libmupdf-third), installed under /usr or /usr/local.

/*
#cgo CFLAGS: -I/usr/include -I/usr/local/include
#cgo LDFLAGS: -lmupdf -lmupdf-third -lm
*/
import "C"
