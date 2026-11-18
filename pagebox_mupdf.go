//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include "gomupdf_bigstack.h"
#include <mupdf/pdf.h>
#include <stdlib.h>
#include <stdio.h>

// Emit a page's geometry as "rot mx0 my0 mx1 my1 cx0 cy0 cx1 cy1 hascrop".
// Boxes are the raw, inheritable dictionary values in unrotated PDF points; the
// CropBox falls back to the MediaBox when absent (hascrop reports which).
static char *gomupdf_page_geometry_run(fz_context *ctx, fz_document *doc, int pageno,
                                   char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return NULL; }
    pdf_page *page = NULL;
    char *result = NULL;
    fz_var(page);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        pdf_obj *obj = page->obj;
        int rot = pdf_to_int(ctx, pdf_dict_get_inheritable(ctx, obj, PDF_NAME(Rotate)));
        pdf_obj *mbo = pdf_dict_get_inheritable(ctx, obj, PDF_NAME(MediaBox));
        fz_rect mb = pdf_to_rect(ctx, mbo);
        pdf_obj *cbo = pdf_dict_get_inheritable(ctx, obj, PDF_NAME(CropBox));
        int hascrop = (cbo != NULL);
        fz_rect cb = hascrop ? fz_intersect_rect(pdf_to_rect(ctx, cbo), mb) : mb;
        result = (char *)malloc(256);
        if (!result) fz_throw(ctx, FZ_ERROR_SYSTEM, "out of memory (%zu bytes)", (size_t)(256));
        if (result)
            snprintf(result, 256, "%d %g %g %g %g %g %g %g %g %d",
                     rot, mb.x0, mb.y0, mb.x1, mb.y1,
                     cb.x0, cb.y0, cb.x1, cb.y1, hascrop);
    }
    fz_always(ctx) { fz_drop_page(ctx, (fz_page *)page); }
    fz_catch(ctx) {
        if (result) { free(result); result = NULL; }
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return NULL;
    }
    return result;
}

static int gomupdf_set_rotation_run(fz_context *ctx, fz_document *doc, int pageno,
                                int deg, char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    fz_var(page);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        pdf_dict_put_int(ctx, page->obj, PDF_NAME(Rotate), deg);
    }
    fz_always(ctx) { fz_drop_page(ctx, (fz_page *)page); }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

// which: 0 = MediaBox, 1 = CropBox.
static int gomupdf_set_box_run(fz_context *ctx, fz_document *doc, int pageno, int which,
                           float x0, float y0, float x1, float y1,
                           char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    fz_var(page);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        fz_rect r = fz_make_rect(x0, y0, x1, y1);
        pdf_obj *key = which == 1 ? PDF_NAME(CropBox) : PDF_NAME(MediaBox);
        pdf_dict_put_rect(ctx, page->obj, key, r);
    }
    fz_always(ctx) { fz_drop_page(ctx, (fz_page *)page); }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

// gomupdf_page_geometry runs gomupdf_page_geometry_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    char *err;
    int errlen;
    char *ret;
} gomupdf_page_geometry_args;

static void gomupdf_page_geometry_thunk(void *p) {
    gomupdf_page_geometry_args *a = p;
    a->ret = gomupdf_page_geometry_run(a->ctx, a->doc, a->pageno, a->err, a->errlen);
}

static char *gomupdf_page_geometry(fz_context *ctx, fz_document *doc, int pageno, char *err, int errlen) {
    gomupdf_page_geometry_args a = {ctx, doc, pageno, err, errlen};
    gomupdf_call_bigstack(gomupdf_page_geometry_thunk, &a);
    return a.ret;
}

// gomupdf_set_box runs gomupdf_set_box_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    int which;
    float x0;
    float y0;
    float x1;
    float y1;
    char *err;
    int errlen;
    int ret;
} gomupdf_set_box_args;

static void gomupdf_set_box_thunk(void *p) {
    gomupdf_set_box_args *a = p;
    a->ret = gomupdf_set_box_run(a->ctx, a->doc, a->pageno, a->which, a->x0, a->y0, a->x1, a->y1, a->err, a->errlen);
}

static int gomupdf_set_box(fz_context *ctx, fz_document *doc, int pageno, int which, float x0, float y0, float x1, float y1, char *err, int errlen) {
    gomupdf_set_box_args a = {ctx, doc, pageno, which, x0, y0, x1, y1, err, errlen};
    gomupdf_call_bigstack(gomupdf_set_box_thunk, &a);
    return a.ret;
}

// gomupdf_set_rotation runs gomupdf_set_rotation_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    int deg;
    char *err;
    int errlen;
    int ret;
} gomupdf_set_rotation_args;

static void gomupdf_set_rotation_thunk(void *p) {
    gomupdf_set_rotation_args *a = p;
    a->ret = gomupdf_set_rotation_run(a->ctx, a->doc, a->pageno, a->deg, a->err, a->errlen);
}

static int gomupdf_set_rotation(fz_context *ctx, fz_document *doc, int pageno, int deg, char *err, int errlen) {
    gomupdf_set_rotation_args a = {ctx, doc, pageno, deg, err, errlen};
    gomupdf_call_bigstack(gomupdf_set_rotation_thunk, &a);
    return a.ret;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func (d *mupdfDoc) geometryRaw(pageNo int) (string, error) {
	if err := d.readable(); err != nil {
		return "", err
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	cstr := C.gomupdf_page_geometry(d.ctx, d.doc, C.int(pageNo), errBuf, errBufLen)
	if cstr == nil {
		return "", errors.New("gomupdf: page geometry: " + errText(errBuf))
	}
	defer C.free(unsafe.Pointer(cstr))
	return C.GoString(cstr), nil
}

func (d *mupdfDoc) setRotation(pageNo, deg int) error {
	if err := d.writable(); err != nil {
		return err
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_set_rotation(d.ctx, d.doc, C.int(pageNo), C.int(deg), errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: set rotation: " + errText(errBuf))
	}
	return nil
}

func (d *mupdfDoc) setBox(pageNo, which int, x0, y0, x1, y1 float64) error {
	if err := d.writable(); err != nil {
		return err
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_set_box(d.ctx, d.doc, C.int(pageNo), C.int(which),
		C.float(x0), C.float(y0), C.float(x1), C.float(y1), errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: set box: " + errText(errBuf))
	}
	return nil
}
