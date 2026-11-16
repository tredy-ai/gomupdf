//go:build !nomupdf

package gomupdf

/*
#include <mupdf/fitz.h>
*/
import "C"

// stextFlags maps opts to fz_stext_options flags. ExtractText and
// RawDictWithOptions both use it, so an option means the same in both.
// FZ_STEXT_CLIP is never set; Clip is applied per character instead.
func stextFlags(opts TextOptions) C.int {
	var flags C.int
	if opts.PreserveLigatures {
		flags |= C.FZ_STEXT_PRESERVE_LIGATURES
	}
	if opts.PreserveWhitespace {
		flags |= C.FZ_STEXT_PRESERVE_WHITESPACE
	}
	if opts.PreserveImages {
		flags |= C.FZ_STEXT_PRESERVE_IMAGES
	}
	if opts.InhibitSpaces {
		flags |= C.FZ_STEXT_INHIBIT_SPACES
	}
	if opts.Dehyphenate {
		flags |= C.FZ_STEXT_DEHYPHENATE
	}
	if opts.AccurateBBoxes {
		flags |= C.FZ_STEXT_ACCURATE_BBOXES
	}
	return flags
}

// stextClip returns opts.Clip as the C helpers' clip arguments.
func stextClip(opts TextOptions) (enabled C.int, x0, y0, x1, y1 C.float) {
	if opts.Clip == nil {
		return 0, 0, 0, 0, 0
	}
	c := opts.Clip
	return 1, C.float(c.X0), C.float(c.Y0), C.float(c.X1), C.float(c.Y1)
}
