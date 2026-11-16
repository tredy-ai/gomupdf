//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include <mupdf/pdf.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

static int gomupdf_add_redaction(fz_context *ctx, fz_document *doc, int pageno,
                                 float x0, float y0, float x1, float y1,
                                 char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    pdf_annot *annot = NULL;
    fz_var(page);
    fz_var(annot);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        annot = pdf_create_annot(ctx, page, PDF_ANNOT_REDACT);
        fz_rect r = fz_make_rect(x0, y0, x1, y1);
        pdf_set_annot_rect(ctx, annot, r);
        pdf_update_annot(ctx, annot);
    }
    fz_always(ctx) {
        if (annot) pdf_drop_annot(ctx, annot);
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

// fz_quad is read straight from the caller's flat float array.
_Static_assert(sizeof(fz_quad) == 8 * sizeof(float), "fz_quad is not 8 packed floats");

// gomupdf_add_redaction_quads creates one redaction annotation with /QuadPoints
// set to the n quads (page space, ul ur ll lr each). pdf_set_annot_quad_points
// does not touch /Rect; pdf_update_annot synthesises the appearance and sets
// /Rect to the union of the quads' bounds, each expanded by its 1pt outline.
// It overwrites any /Rect set before it, so none is set here.
static int gomupdf_add_redaction_quads(fz_context *ctx, fz_document *doc, int pageno,
                                       const float *quads, int n,
                                       char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    const fz_quad *qv = (const fz_quad *)quads;
    pdf_page *page = NULL;
    pdf_annot *annot = NULL;
    fz_var(page);
    fz_var(annot);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        annot = pdf_create_annot(ctx, page, PDF_ANNOT_REDACT);
        pdf_set_annot_quad_points(ctx, annot, n, qv);
        pdf_update_annot(ctx, annot);
    }
    fz_always(ctx) {
        if (annot) pdf_drop_annot(ctx, annot);
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

// gomupdf_apply_redactions applies every redaction annotation on the page.
// image_method, line_art and text carry the Go-side enum values (see
// ApplyOptions); each is mapped to its PDF_REDACT_* constant by name, and an
// unknown value is an error. black_boxes is MuPDF's flag (non-zero = draw).
static int gomupdf_apply_redactions(fz_context *ctx, fz_document *doc, int pageno,
                                    int black_boxes, int image_method,
                                    int line_art, int text,
                                    char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_redact_options opts;
    memset(&opts, 0, sizeof(opts));
    opts.black_boxes = black_boxes ? 1 : 0;
    switch (image_method) {
    case 0: opts.image_method = PDF_REDACT_IMAGE_PIXELS; break;
    case 1: opts.image_method = PDF_REDACT_IMAGE_NONE; break;
    case 2: opts.image_method = PDF_REDACT_IMAGE_REMOVE; break;
    default: snprintf(err, errlen, "invalid image method %d", image_method); return -1;
    }
    switch (line_art) {
    case 0: opts.line_art = PDF_REDACT_LINE_ART_NONE; break;
    case 1: opts.line_art = PDF_REDACT_LINE_ART_REMOVE_IF_COVERED; break;
    case 2: opts.line_art = PDF_REDACT_LINE_ART_REMOVE_IF_TOUCHED; break;
    default: snprintf(err, errlen, "invalid line art method %d", line_art); return -1;
    }
    switch (text) {
    case 0: opts.text = PDF_REDACT_TEXT_REMOVE; break;
    case 1: opts.text = PDF_REDACT_TEXT_NONE; break;
    default: snprintf(err, errlen, "invalid text method %d", text); return -1;
    }
    pdf_page *page = NULL;
    int count = 0;
    fz_var(page);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        pdf_annot *a;
        for (a = pdf_first_annot(ctx, page); a; a = pdf_next_annot(ctx, a)) {
            if (pdf_annot_type(ctx, a) == PDF_ANNOT_REDACT) {
                count++;
            }
        }
        if (count > 0) {
            pdf_redact_page(ctx, pdf, page, &opts);
        }
    }
    fz_always(ctx) {
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return count;
}
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

func (d *mupdfDoc) addRedaction(pageNo int, rect [4]float64) error {
	if d.doc == nil {
		return errors.New("gomupdf: document closed")
	}
	errBuf := (*C.char)(C.malloc(errBufLen))
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_add_redaction(d.ctx, d.doc, C.int(pageNo),
		C.float(rect[0]), C.float(rect[1]), C.float(rect[2]), C.float(rect[3]),
		errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: add redaction: " + C.GoString(errBuf))
	}
	return nil
}

func (d *mupdfDoc) addRedactionQuads(pageNo int, quads []float32) error {
	if d.doc == nil {
		return errors.New("gomupdf: document closed")
	}
	if len(quads) == 0 || len(quads)%8 != 0 {
		return errors.New("gomupdf: add redaction quads: want 8 floats per quad")
	}
	errBuf := (*C.char)(C.malloc(errBufLen))
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_add_redaction_quads(d.ctx, d.doc, C.int(pageNo),
		(*C.float)(unsafe.Pointer(&quads[0])), C.int(len(quads)/8),
		errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: add redaction quads: " + C.GoString(errBuf))
	}
	return nil
}

func (d *mupdfDoc) applyRedactions(pageNo int, opts ApplyOptions) (int, error) {
	if d.doc == nil {
		return 0, errors.New("gomupdf: document closed")
	}
	// Range-check in Go: C.int would silently truncate 1<<32+1 to a valid value.
	if opts.Images < ImagePixels || opts.Images > ImageRemove ||
		opts.LineArt < LineArtNone || opts.LineArt > LineArtRemoveIfTouched ||
		opts.Text < TextRemove || opts.Text > TextNone {
		return 0, fmt.Errorf("gomupdf: apply redactions: invalid method (images %d, line art %d, text %d)",
			opts.Images, opts.LineArt, opts.Text)
	}
	// NoBox is inverted relative to MuPDF's black_boxes.
	boxes := C.int(1)
	if opts.NoBox {
		boxes = 0
	}
	errBuf := (*C.char)(C.malloc(errBufLen))
	defer C.free(unsafe.Pointer(errBuf))
	n := C.gomupdf_apply_redactions(d.ctx, d.doc, C.int(pageNo), boxes,
		C.int(opts.Images), C.int(opts.LineArt), C.int(opts.Text), errBuf, errBufLen)
	if n < 0 {
		return 0, errors.New("gomupdf: apply redactions: " + C.GoString(errBuf))
	}
	return int(n), nil
}
