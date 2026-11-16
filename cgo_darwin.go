//go:build darwin && !nomupdf

package gomupdf

// Homebrew MuPDF (Apple Silicon prefix). Flags carried over unchanged from
// the per-file directives.

/*
#cgo CFLAGS: -I/opt/homebrew/include
#cgo LDFLAGS: -L/opt/homebrew/lib -lmupdf
*/
import "C"
