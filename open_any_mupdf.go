//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include <mupdf/pdf.h>
#include "gomupdf_bigstack.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

// Render every page of any source document into a new PDF and return the
// serialized PDF bytes (malloc'd + *out_len). Done entirely in the source
// context to avoid cross-context object access.
static unsigned char *gomupdf_convert_to_pdf_run(fz_context *ctx, fz_document *src,
                                             int *out_len, char *err, int errlen) {
    pdf_document *pdf = NULL;
    fz_buffer *outbuf = NULL;
    fz_output *out = NULL;
    unsigned char *result = NULL;
    fz_var(pdf);
    fz_var(outbuf);
    fz_var(out);
    fz_try(ctx) {
        pdf = pdf_create_document(ctx);
        int n = fz_count_pages(ctx, src);
        for (int i = 0; i < n; i++) {
            fz_page *page = NULL;
            fz_device *dev = NULL;
            fz_buffer *contents = NULL;
            pdf_obj *resources = NULL;
            pdf_obj *pageobj = NULL;
            fz_var(page);
            fz_var(dev);
            fz_var(contents);
            fz_var(resources);
            fz_var(pageobj);
            fz_try(ctx) {
                page = fz_load_page(ctx, src, i);
                fz_rect bounds = fz_bound_page(ctx, page);
                dev = pdf_page_write(ctx, pdf, bounds, &resources, &contents);
                gomupdf_run_page_big(ctx, page, dev, 0);
                fz_close_device(ctx, dev);
                pageobj = pdf_add_page(ctx, pdf, bounds, 0, resources, contents);
                pdf_insert_page(ctx, pdf, -1, pageobj);
            }
            fz_always(ctx) {
                pdf_drop_obj(ctx, pageobj);
                pdf_drop_obj(ctx, resources);
                fz_drop_buffer(ctx, contents);
                fz_drop_device(ctx, dev);
                fz_drop_page(ctx, page);
            }
            fz_catch(ctx) { fz_rethrow(ctx); }
        }
        outbuf = fz_new_buffer(ctx, 8192);
        out = fz_new_output_with_buffer(ctx, outbuf);
        pdf_write_options opts;
        memset(&opts, 0, sizeof(opts));
        opts.do_garbage = 3;
        opts.do_compress = 1;
        gomupdf_write_big(ctx, pdf, out, NULL, &opts);
        fz_close_output(ctx, out);
        unsigned char *data = NULL;
        size_t sz = fz_buffer_storage(ctx, outbuf, &data);
        result = (unsigned char *)malloc(sz);
        if (!result) fz_throw(ctx, FZ_ERROR_SYSTEM, "out of memory (%zu bytes)", (size_t)(sz));
        if (result) { memcpy(result, data, sz); *out_len = (int)sz; }
    }
    fz_always(ctx) {
        fz_drop_output(ctx, out);
        fz_drop_buffer(ctx, outbuf);
        fz_drop_document(ctx, (fz_document *)pdf);
    }
    fz_catch(ctx) {
        if (result) { free(result); result = NULL; }
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return NULL;
    }
    return result;
}

// gomupdf_convert_to_pdf runs gomupdf_convert_to_pdf_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *src;
    int *out_len;
    char *err;
    int errlen;
    unsigned char *ret;
} gomupdf_convert_to_pdf_args;

static void gomupdf_convert_to_pdf_thunk(void *p) {
    gomupdf_convert_to_pdf_args *a = p;
    a->ret = gomupdf_convert_to_pdf_run(a->ctx, a->src, a->out_len, a->err, a->errlen);
}

static unsigned char *gomupdf_convert_to_pdf(fz_context *ctx, fz_document *src, int *out_len, char *err, int errlen) {
    gomupdf_convert_to_pdf_args a = {ctx, src, out_len, err, errlen};
    int e = gomupdf_call_bigstack(gomupdf_convert_to_pdf_thunk, &a);
    if (e) {
        gomupdf_bigstack_err(e, err, errlen);
        *out_len = -1;
        return NULL;
    }
    return a.ret;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func (d *mupdfDoc) convertToPDF() ([]byte, error) {
	if err := d.readable(); err != nil {
		return nil, err
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	var outLen C.int
	ptr := C.gomupdf_convert_to_pdf(d.ctx, d.doc, &outLen, errBuf, errBufLen)
	if ptr == nil {
		return nil, errors.New("gomupdf: convert to pdf: " + errText(errBuf))
	}
	defer C.free(unsafe.Pointer(ptr))
	return C.GoBytes(unsafe.Pointer(ptr), outLen), nil
}
