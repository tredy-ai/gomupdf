//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include "gomupdf_bigstack.h"
#include <mupdf/pdf.h>
#include <stdlib.h>
#include <stdio.h>

static int gomupdf_insert_link_run(fz_context *ctx, fz_document *doc, int pageno,
                               float x0, float y0, float x1, float y1,
                               const char *uri, char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    fz_var(page);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        fz_rect bbox = fz_make_rect(x0, y0, x1, y1);
        pdf_create_link(ctx, page, bbox, uri);
    }
    fz_always(ctx) {
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

static int gomupdf_insert_goto_link_run(fz_context *ctx, fz_document *doc, int pageno,
                                    float x0, float y0, float x1, float y1,
                                    int destPage, char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    char *uri = NULL;
    fz_var(page);
    fz_var(uri);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        fz_rect bbox = fz_make_rect(x0, y0, x1, y1);
        fz_link_dest dest;
        dest.loc = fz_make_location(0, destPage);
        dest.type = FZ_LINK_DEST_FIT;
        dest.x = dest.y = dest.w = dest.h = dest.zoom = 0;
        uri = fz_format_link_uri(ctx, doc, dest);
        pdf_create_link(ctx, page, bbox, uri);
    }
    fz_always(ctx) {
        fz_free(ctx, uri);
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

static int gomupdf_delete_link_run(fz_context *ctx, fz_document *doc, int pageno,
                               int index, char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    fz_link *links = NULL;
    fz_var(page);
    fz_var(links);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        links = fz_load_links(ctx, (fz_page *)page);
        fz_link *cur = links;
        int i = 0;
        while (cur && i < index) { cur = cur->next; i++; }
        if (!cur) {
            snprintf(err, errlen, "link index %d out of range", index);
            fz_throw(ctx, FZ_ERROR_GENERIC, "link index out of range");
        }
        pdf_delete_link(ctx, page, cur);
    }
    fz_always(ctx) {
        fz_drop_link(ctx, links);
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

// gomupdf_delete_link runs gomupdf_delete_link_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    int index;
    char *err;
    int errlen;
    int ret;
} gomupdf_delete_link_args;

static void gomupdf_delete_link_thunk(void *p) {
    gomupdf_delete_link_args *a = p;
    a->ret = gomupdf_delete_link_run(a->ctx, a->doc, a->pageno, a->index, a->err, a->errlen);
}

static int gomupdf_delete_link(fz_context *ctx, fz_document *doc, int pageno, int index, char *err, int errlen) {
    gomupdf_delete_link_args a = {ctx, doc, pageno, index, err, errlen};
    gomupdf_call_bigstack(gomupdf_delete_link_thunk, &a);
    return a.ret;
}

// gomupdf_insert_goto_link runs gomupdf_insert_goto_link_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    float x0;
    float y0;
    float x1;
    float y1;
    int destPage;
    char *err;
    int errlen;
    int ret;
} gomupdf_insert_goto_link_args;

static void gomupdf_insert_goto_link_thunk(void *p) {
    gomupdf_insert_goto_link_args *a = p;
    a->ret = gomupdf_insert_goto_link_run(a->ctx, a->doc, a->pageno, a->x0, a->y0, a->x1, a->y1, a->destPage, a->err, a->errlen);
}

static int gomupdf_insert_goto_link(fz_context *ctx, fz_document *doc, int pageno, float x0, float y0, float x1, float y1, int destPage, char *err, int errlen) {
    gomupdf_insert_goto_link_args a = {ctx, doc, pageno, x0, y0, x1, y1, destPage, err, errlen};
    gomupdf_call_bigstack(gomupdf_insert_goto_link_thunk, &a);
    return a.ret;
}

// gomupdf_insert_link runs gomupdf_insert_link_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    float x0;
    float y0;
    float x1;
    float y1;
    const char *uri;
    char *err;
    int errlen;
    int ret;
} gomupdf_insert_link_args;

static void gomupdf_insert_link_thunk(void *p) {
    gomupdf_insert_link_args *a = p;
    a->ret = gomupdf_insert_link_run(a->ctx, a->doc, a->pageno, a->x0, a->y0, a->x1, a->y1, a->uri, a->err, a->errlen);
}

static int gomupdf_insert_link(fz_context *ctx, fz_document *doc, int pageno, float x0, float y0, float x1, float y1, const char *uri, char *err, int errlen) {
    gomupdf_insert_link_args a = {ctx, doc, pageno, x0, y0, x1, y1, uri, err, errlen};
    gomupdf_call_bigstack(gomupdf_insert_link_thunk, &a);
    return a.ret;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func (d *mupdfDoc) insertLink(pageNo int, rect [4]float64, uri string) error {
	if err := d.writable(); err != nil {
		return err
	}
	curi := C.CString(uri)
	defer C.free(unsafe.Pointer(curi))
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_insert_link(d.ctx, d.doc, C.int(pageNo),
		C.float(rect[0]), C.float(rect[1]), C.float(rect[2]), C.float(rect[3]),
		curi, errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: insert link: " + errText(errBuf))
	}
	return nil
}

func (d *mupdfDoc) insertGotoLink(pageNo int, rect [4]float64, destPage int) error {
	if err := d.writable(); err != nil {
		return err
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_insert_goto_link(d.ctx, d.doc, C.int(pageNo),
		C.float(rect[0]), C.float(rect[1]), C.float(rect[2]), C.float(rect[3]),
		C.int(destPage), errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: insert goto link: " + errText(errBuf))
	}
	return nil
}

func (d *mupdfDoc) deleteLink(pageNo, index int) error {
	if err := d.writable(); err != nil {
		return err
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_delete_link(d.ctx, d.doc, C.int(pageNo), C.int(index), errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: delete link: " + errText(errBuf))
	}
	return nil
}
