//go:build linux && !nomupdf && !mupdf_bundled

package gomupdf

// Default linux link flags: ask pkg-config for the system MuPDF (Alpine's
// mupdf-dev installs mupdf.pc), so the flags always match the installed
// package. Distro packages unbundle MuPDF's third-party libraries, so there
// is no -lmupdf-third here; build with -tags mupdf_bundled for a MuPDF built
// from source with its bundled third-party code.

/*
#cgo pkg-config: mupdf
*/
import "C"
