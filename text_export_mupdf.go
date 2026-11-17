//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include "gomupdf_stext.h"
#include <stdlib.h>
#include <string.h>

static char *gomupdf_page_stext_html(fz_context *ctx, fz_document *doc, int pageno,
                                     char *err, int errlen) {
    fz_page *page = NULL;
    fz_stext_page *stext = NULL;
    fz_buffer *buf = NULL;
    fz_output *out = NULL;
    char *result = NULL;
    fz_var(page);
    fz_var(stext);
    fz_var(buf);
    fz_var(out);
    fz_try(ctx) {
        page = fz_load_page(ctx, doc, pageno);
        fz_stext_options opts;
        memset(&opts, 0, sizeof(opts)); opts.flags = FZ_STEXT_IGNORE_ACTUALTEXT;
        stext = gomupdf_new_stext_page(ctx, page, &opts);
        buf = fz_new_buffer(ctx, 8192);
        out = fz_new_output_with_buffer(ctx, buf);
        fz_print_stext_page_as_html(ctx, out, stext, pageno);
        fz_close_output(ctx, out);
        unsigned char *data = NULL;
        size_t n = fz_buffer_storage(ctx, buf, &data);
        result = (char *)malloc(n + 1);
        if (!result) fz_throw(ctx, FZ_ERROR_SYSTEM, "out of memory (%zu bytes)", (size_t)(n + 1));
        if (result) { memcpy(result, data, n); result[n] = '\0'; }
    }
    fz_always(ctx) {
        fz_drop_output(ctx, out);
        fz_drop_buffer(ctx, buf);
        fz_drop_stext_page(ctx, stext);
        fz_drop_page(ctx, page);
    }
    fz_catch(ctx) {
        if (result) { free(result); result = NULL; }
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return NULL;
    }
    return result;
}

static char *gomupdf_page_stext_xhtml(fz_context *ctx, fz_document *doc, int pageno,
                                      char *err, int errlen) {
    fz_page *page = NULL;
    fz_stext_page *stext = NULL;
    fz_buffer *buf = NULL;
    fz_output *out = NULL;
    char *result = NULL;
    fz_var(page);
    fz_var(stext);
    fz_var(buf);
    fz_var(out);
    fz_try(ctx) {
        page = fz_load_page(ctx, doc, pageno);
        fz_stext_options opts;
        memset(&opts, 0, sizeof(opts)); opts.flags = FZ_STEXT_IGNORE_ACTUALTEXT;
        stext = gomupdf_new_stext_page(ctx, page, &opts);
        buf = fz_new_buffer(ctx, 8192);
        out = fz_new_output_with_buffer(ctx, buf);
        fz_print_stext_page_as_xhtml(ctx, out, stext, pageno);
        fz_close_output(ctx, out);
        unsigned char *data = NULL;
        size_t n = fz_buffer_storage(ctx, buf, &data);
        result = (char *)malloc(n + 1);
        if (!result) fz_throw(ctx, FZ_ERROR_SYSTEM, "out of memory (%zu bytes)", (size_t)(n + 1));
        if (result) { memcpy(result, data, n); result[n] = '\0'; }
    }
    fz_always(ctx) {
        fz_drop_output(ctx, out);
        fz_drop_buffer(ctx, buf);
        fz_drop_stext_page(ctx, stext);
        fz_drop_page(ctx, page);
    }
    fz_catch(ctx) {
        if (result) { free(result); result = NULL; }
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return NULL;
    }
    return result;
}

static char *gomupdf_page_stext_xml(fz_context *ctx, fz_document *doc, int pageno,
                                    char *err, int errlen) {
    fz_page *page = NULL;
    fz_stext_page *stext = NULL;
    fz_buffer *buf = NULL;
    fz_output *out = NULL;
    char *result = NULL;
    fz_var(page);
    fz_var(stext);
    fz_var(buf);
    fz_var(out);
    fz_try(ctx) {
        page = fz_load_page(ctx, doc, pageno);
        fz_stext_options opts;
        memset(&opts, 0, sizeof(opts)); opts.flags = FZ_STEXT_IGNORE_ACTUALTEXT;
        stext = gomupdf_new_stext_page(ctx, page, &opts);
        buf = fz_new_buffer(ctx, 8192);
        out = fz_new_output_with_buffer(ctx, buf);
        fz_print_stext_page_as_xml(ctx, out, stext, pageno);
        fz_close_output(ctx, out);
        unsigned char *data = NULL;
        size_t n = fz_buffer_storage(ctx, buf, &data);
        result = (char *)malloc(n + 1);
        if (!result) fz_throw(ctx, FZ_ERROR_SYSTEM, "out of memory (%zu bytes)", (size_t)(n + 1));
        if (result) { memcpy(result, data, n); result[n] = '\0'; }
    }
    fz_always(ctx) {
        fz_drop_output(ctx, out);
        fz_drop_buffer(ctx, buf);
        fz_drop_stext_page(ctx, stext);
        fz_drop_page(ctx, page);
    }
    fz_catch(ctx) {
        if (result) { free(result); result = NULL; }
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return NULL;
    }
    return result;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func (d *mupdfDoc) htmlRaw(pageNo int) (string, error) {
	if d.doc == nil {
		return "", errors.New("gomupdf: document closed")
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	cstr := C.gomupdf_page_stext_html(d.ctx, d.doc, C.int(pageNo), errBuf, errBufLen)
	if cstr == nil {
		return "", errors.New("gomupdf: HTML: " + errText(errBuf))
	}
	defer C.free(unsafe.Pointer(cstr))
	return C.GoString(cstr), nil
}

func (d *mupdfDoc) xhtmlRaw(pageNo int) (string, error) {
	if d.doc == nil {
		return "", errors.New("gomupdf: document closed")
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	cstr := C.gomupdf_page_stext_xhtml(d.ctx, d.doc, C.int(pageNo), errBuf, errBufLen)
	if cstr == nil {
		return "", errors.New("gomupdf: XHTML: " + errText(errBuf))
	}
	defer C.free(unsafe.Pointer(cstr))
	return C.GoString(cstr), nil
}

func (d *mupdfDoc) xmlRaw(pageNo int) (string, error) {
	if d.doc == nil {
		return "", errors.New("gomupdf: document closed")
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	cstr := C.gomupdf_page_stext_xml(d.ctx, d.doc, C.int(pageNo), errBuf, errBufLen)
	if cstr == nil {
		return "", errors.New("gomupdf: XML: " + errText(errBuf))
	}
	defer C.free(unsafe.Pointer(cstr))
	return C.GoString(cstr), nil
}
