//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include "gomupdf_bigstack.h"
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

// cs_kind: 0=rgb, 1=gray, 2=cmyk
// have_clip: 0=no clip, 1=use clipx0/y0/x1/y1
// no_annots: 0=render everything, 1=skip annotations
// Returns malloc'd blob: 5x int32 header [w,h,n,stride,alpha] + raw samples.
static unsigned char *gomupdf_render_run(
        fz_context *ctx, fz_document *doc, int pageno,
        float zoom, int cs_kind, int alpha,
        float clipx0, float clipy0, float clipx1, float clipy1,
        int have_clip, int no_annots,
        int *out_len, char *err, int errlen)
{
    fz_page    *page   = NULL;
    fz_pixmap  *pix    = NULL;
    fz_device  *dev    = NULL;
    unsigned char *result = NULL;

    fz_var(page);
    fz_var(pix);
    fz_var(dev);

    fz_try(ctx) {
        fz_matrix m = fz_scale(zoom, zoom);

        fz_colorspace *cs;
        switch (cs_kind) {
        case 1:  cs = fz_device_gray(ctx); break;
        case 2:  cs = fz_device_cmyk(ctx); break;
        default: cs = fz_device_rgb(ctx);  break;
        }

        page = fz_load_page(ctx, doc, pageno);

        fz_rect r = fz_bound_page(ctx, page);
        if (have_clip) {
            fz_rect clip = fz_make_rect(clipx0, clipy0, clipx1, clipy1);
            r = fz_intersect_rect(r, clip);
        }
        fz_irect ir = fz_round_rect(fz_transform_rect(r, m));

        pix = fz_new_pixmap_with_bbox(ctx, cs, ir, NULL, alpha);
        if (alpha)
            fz_clear_pixmap(ctx, pix);
        else
            fz_clear_pixmap_with_value(ctx, pix, 0xff);

        dev = fz_new_draw_device(ctx, m, pix);
        gomupdf_run_page_big(ctx, page, dev, no_annots);
        fz_close_device(ctx, dev);
        fz_drop_device(ctx, dev);
        dev = NULL;

        int32_t hdr[5];
        hdr[0] = pix->w;
        hdr[1] = pix->h;
        hdr[2] = pix->n;
        hdr[3] = pix->stride;
        hdr[4] = pix->alpha;
        size_t samples = (size_t)pix->stride * pix->h;
        size_t total   = sizeof(hdr) + samples;
        result = (unsigned char *)malloc(total);
        if (!result) fz_throw(ctx, FZ_ERROR_SYSTEM, "out of memory (%zu bytes)", (size_t)(total));
        if (result) {
            memcpy(result, hdr, sizeof(hdr));
            memcpy(result + sizeof(hdr), pix->samples, samples);
            *out_len = (int)total;
        }
    }
    fz_always(ctx) {
        fz_drop_device(ctx, dev);
        fz_drop_pixmap(ctx, pix);
        fz_drop_page(ctx, page);
    }
    fz_catch(ctx) {
        if (result) { free(result); result = NULL; }
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return NULL;
    }
    return result;
}

// gomupdf_render runs gomupdf_render_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    float zoom;
    int cs_kind;
    int alpha;
    float clipx0;
    float clipy0;
    float clipx1;
    float clipy1;
    int have_clip;
    int no_annots;
    int *out_len;
    char *err;
    int errlen;
    unsigned char *ret;
} gomupdf_render_args;

static void gomupdf_render_thunk(void *p) {
    gomupdf_render_args *a = p;
    a->ret = gomupdf_render_run(a->ctx, a->doc, a->pageno, a->zoom, a->cs_kind, a->alpha, a->clipx0, a->clipy0, a->clipx1, a->clipy1, a->have_clip, a->no_annots, a->out_len, a->err, a->errlen);
}

static unsigned char *gomupdf_render(fz_context *ctx, fz_document *doc, int pageno, float zoom, int cs_kind, int alpha, float clipx0, float clipy0, float clipx1, float clipy1, int have_clip, int no_annots, int *out_len, char *err, int errlen) {
    gomupdf_render_args a = {ctx, doc, pageno, zoom, cs_kind, alpha, clipx0, clipy0, clipx1, clipy1, have_clip, no_annots, out_len, err, errlen};
    gomupdf_call_bigstack(gomupdf_render_thunk, &a);
    return a.ret;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func (d *mupdfDoc) pixmap(pageNo int, o PixmapOptions) ([]byte, error) {
	if err := d.readable(); err != nil {
		return nil, err
	}

	// Resolve zoom.
	zoom := o.Zoom
	if o.DPI > 0 {
		zoom = o.DPI / 72.0
	}
	if zoom <= 0 {
		zoom = 1
	}

	// Resolve colorspace kind: 0=rgb, 1=gray, 2=cmyk.
	csKind := C.int(0)
	if o.CMYK {
		csKind = 2
	} else if o.Gray {
		csKind = 1
	}

	alpha := C.int(0)
	if o.Alpha {
		alpha = 1
	}

	haveClip := C.int(0)
	var clipx0, clipy0, clipx1, clipy1 float64
	if o.Clip != nil {
		haveClip = 1
		clipx0, clipy0, clipx1, clipy1 = o.Clip.X0, o.Clip.Y0, o.Clip.X1, o.Clip.Y1
	}

	noAnnots := C.int(0)
	if o.NoAnnots {
		noAnnots = 1
	}

	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	var outLen C.int
	ptr := C.gomupdf_render(
		d.ctx, d.doc, C.int(pageNo),
		C.float(zoom), csKind, alpha,
		C.float(clipx0), C.float(clipy0), C.float(clipx1), C.float(clipy1),
		haveClip, noAnnots,
		&outLen, errBuf, errBufLen,
	)
	if ptr == nil {
		return nil, errors.New("gomupdf: render: " + errText(errBuf))
	}
	defer C.free(unsafe.Pointer(ptr))
	return C.GoBytes(unsafe.Pointer(ptr), outLen), nil
}
