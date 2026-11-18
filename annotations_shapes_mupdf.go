//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include "gomupdf_bigstack.h"
#include <mupdf/pdf.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

static void apply_annot_style(fz_context *ctx, pdf_annot *annot,
                               float *stroke_rgb, float *fill_rgb,
                               float width, float opacity) {
    pdf_set_annot_color(ctx, annot, 3, stroke_rgb);
    if (fill_rgb) {
        pdf_set_annot_interior_color(ctx, annot, 3, fill_rgb);
    }
    pdf_set_annot_border_width(ctx, annot, width);
    pdf_set_annot_opacity(ctx, annot, opacity);
}

static int gomupdf_add_line_run(fz_context *ctx, fz_document *doc, int pageno,
                             float ax, float ay, float bx, float by,
                             float *stroke_rgb, float width, float opacity,
                             char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    pdf_annot *annot = NULL;
    fz_var(page);
    fz_var(annot);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        annot = pdf_create_annot(ctx, page, PDF_ANNOT_LINE);
        fz_point pa = { ax, ay };
        fz_point pb = { bx, by };
        pdf_set_annot_line(ctx, annot, pa, pb);
        apply_annot_style(ctx, annot, stroke_rgb, NULL, width, opacity);
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

static int gomupdf_add_circle_run(fz_context *ctx, fz_document *doc, int pageno,
                               float x0, float y0, float x1, float y1,
                               float *stroke_rgb, float *fill_rgb,
                               float width, float opacity,
                               char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    pdf_annot *annot = NULL;
    fz_var(page);
    fz_var(annot);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        annot = pdf_create_annot(ctx, page, PDF_ANNOT_CIRCLE);
        fz_rect r = { x0, y0, x1, y1 };
        pdf_set_annot_rect(ctx, annot, r);
        apply_annot_style(ctx, annot, stroke_rgb, fill_rgb, width, opacity);
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

static int gomupdf_add_polygon_run(fz_context *ctx, fz_document *doc, int pageno,
                                float *pts, int npts,
                                float *stroke_rgb, float *fill_rgb,
                                float width, float opacity,
                                char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    pdf_annot *annot = NULL;
    fz_var(page);
    fz_var(annot);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        annot = pdf_create_annot(ctx, page, PDF_ANNOT_POLYGON);
        for (int i = 0; i < npts; i++) {
            fz_point p = { pts[i*2], pts[i*2+1] };
            pdf_add_annot_vertex(ctx, annot, p);
        }
        apply_annot_style(ctx, annot, stroke_rgb, fill_rgb, width, opacity);
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

static int gomupdf_add_polyline_run(fz_context *ctx, fz_document *doc, int pageno,
                                 float *pts, int npts,
                                 float *stroke_rgb, float width, float opacity,
                                 char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    pdf_annot *annot = NULL;
    fz_var(page);
    fz_var(annot);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        annot = pdf_create_annot(ctx, page, PDF_ANNOT_POLY_LINE);
        for (int i = 0; i < npts; i++) {
            fz_point p = { pts[i*2], pts[i*2+1] };
            pdf_add_annot_vertex(ctx, annot, p);
        }
        apply_annot_style(ctx, annot, stroke_rgb, NULL, width, opacity);
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

static int gomupdf_add_ink_run(fz_context *ctx, fz_document *doc, int pageno,
                            float *flat_pts, int *counts, int nstrokes,
                            float *stroke_rgb, float width, float opacity,
                            char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    pdf_annot *annot = NULL;
    fz_point *fpts = NULL;
    fz_var(page);
    fz_var(annot);
    fz_var(fpts);
    fz_try(ctx) {
        int total = 0;
        for (int i = 0; i < nstrokes; i++) total += counts[i];
        fpts = (fz_point *)fz_malloc(ctx, total * sizeof(fz_point));
        for (int i = 0; i < total; i++) {
            fpts[i].x = flat_pts[i*2];
            fpts[i].y = flat_pts[i*2+1];
        }
        page = pdf_load_page(ctx, pdf, pageno);
        annot = pdf_create_annot(ctx, page, PDF_ANNOT_INK);
        pdf_set_annot_ink_list(ctx, annot, nstrokes, counts, fpts);
        apply_annot_style(ctx, annot, stroke_rgb, NULL, width, opacity);
        pdf_update_annot(ctx, annot);
    }
    fz_always(ctx) {
        fz_free(ctx, fpts);
        if (annot) pdf_drop_annot(ctx, annot);
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

static int gomupdf_add_freetext_run(fz_context *ctx, fz_document *doc, int pageno,
                                 float x0, float y0, float x1, float y1,
                                 const char *text, float size,
                                 float *stroke_rgb,
                                 char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    pdf_annot *annot = NULL;
    fz_var(page);
    fz_var(annot);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        annot = pdf_create_annot(ctx, page, PDF_ANNOT_FREE_TEXT);
        fz_rect r = { x0, y0, x1, y1 };
        pdf_set_annot_rect(ctx, annot, r);
        pdf_set_annot_contents(ctx, annot, text);
        pdf_set_annot_default_appearance(ctx, annot, "Helv", size, 3, stroke_rgb);
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

static int gomupdf_add_textnote_run(fz_context *ctx, fz_document *doc, int pageno,
                                 float x, float y, const char *text,
                                 char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    pdf_annot *annot = NULL;
    fz_var(page);
    fz_var(annot);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        annot = pdf_create_annot(ctx, page, PDF_ANNOT_TEXT);
        fz_rect r = { x, y, x + 20.0f, y + 20.0f };
        pdf_set_annot_rect(ctx, annot, r);
        pdf_set_annot_contents(ctx, annot, text);
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

// gomupdf_add_circle runs gomupdf_add_circle_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    float x0;
    float y0;
    float x1;
    float y1;
    float *stroke_rgb;
    float *fill_rgb;
    float width;
    float opacity;
    char *err;
    int errlen;
    int ret;
} gomupdf_add_circle_args;

static void gomupdf_add_circle_thunk(void *p) {
    gomupdf_add_circle_args *a = p;
    a->ret = gomupdf_add_circle_run(a->ctx, a->doc, a->pageno, a->x0, a->y0, a->x1, a->y1, a->stroke_rgb, a->fill_rgb, a->width, a->opacity, a->err, a->errlen);
}

static int gomupdf_add_circle(fz_context *ctx, fz_document *doc, int pageno, float x0, float y0, float x1, float y1, float *stroke_rgb, float *fill_rgb, float width, float opacity, char *err, int errlen) {
    gomupdf_add_circle_args a = {ctx, doc, pageno, x0, y0, x1, y1, stroke_rgb, fill_rgb, width, opacity, err, errlen};
    int e = gomupdf_call_bigstack(gomupdf_add_circle_thunk, &a);
    if (e) {
        gomupdf_bigstack_err(e, err, errlen);
        return -1;
    }
    return a.ret;
}

// gomupdf_add_freetext runs gomupdf_add_freetext_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    float x0;
    float y0;
    float x1;
    float y1;
    const char *text;
    float size;
    float *stroke_rgb;
    char *err;
    int errlen;
    int ret;
} gomupdf_add_freetext_args;

static void gomupdf_add_freetext_thunk(void *p) {
    gomupdf_add_freetext_args *a = p;
    a->ret = gomupdf_add_freetext_run(a->ctx, a->doc, a->pageno, a->x0, a->y0, a->x1, a->y1, a->text, a->size, a->stroke_rgb, a->err, a->errlen);
}

static int gomupdf_add_freetext(fz_context *ctx, fz_document *doc, int pageno, float x0, float y0, float x1, float y1, const char *text, float size, float *stroke_rgb, char *err, int errlen) {
    gomupdf_add_freetext_args a = {ctx, doc, pageno, x0, y0, x1, y1, text, size, stroke_rgb, err, errlen};
    int e = gomupdf_call_bigstack(gomupdf_add_freetext_thunk, &a);
    if (e) {
        gomupdf_bigstack_err(e, err, errlen);
        return -1;
    }
    return a.ret;
}

// gomupdf_add_ink runs gomupdf_add_ink_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    float *flat_pts;
    int *counts;
    int nstrokes;
    float *stroke_rgb;
    float width;
    float opacity;
    char *err;
    int errlen;
    int ret;
} gomupdf_add_ink_args;

static void gomupdf_add_ink_thunk(void *p) {
    gomupdf_add_ink_args *a = p;
    a->ret = gomupdf_add_ink_run(a->ctx, a->doc, a->pageno, a->flat_pts, a->counts, a->nstrokes, a->stroke_rgb, a->width, a->opacity, a->err, a->errlen);
}

static int gomupdf_add_ink(fz_context *ctx, fz_document *doc, int pageno, float *flat_pts, int *counts, int nstrokes, float *stroke_rgb, float width, float opacity, char *err, int errlen) {
    gomupdf_add_ink_args a = {ctx, doc, pageno, flat_pts, counts, nstrokes, stroke_rgb, width, opacity, err, errlen};
    int e = gomupdf_call_bigstack(gomupdf_add_ink_thunk, &a);
    if (e) {
        gomupdf_bigstack_err(e, err, errlen);
        return -1;
    }
    return a.ret;
}

// gomupdf_add_line runs gomupdf_add_line_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    float ax;
    float ay;
    float bx;
    float by;
    float *stroke_rgb;
    float width;
    float opacity;
    char *err;
    int errlen;
    int ret;
} gomupdf_add_line_args;

static void gomupdf_add_line_thunk(void *p) {
    gomupdf_add_line_args *a = p;
    a->ret = gomupdf_add_line_run(a->ctx, a->doc, a->pageno, a->ax, a->ay, a->bx, a->by, a->stroke_rgb, a->width, a->opacity, a->err, a->errlen);
}

static int gomupdf_add_line(fz_context *ctx, fz_document *doc, int pageno, float ax, float ay, float bx, float by, float *stroke_rgb, float width, float opacity, char *err, int errlen) {
    gomupdf_add_line_args a = {ctx, doc, pageno, ax, ay, bx, by, stroke_rgb, width, opacity, err, errlen};
    int e = gomupdf_call_bigstack(gomupdf_add_line_thunk, &a);
    if (e) {
        gomupdf_bigstack_err(e, err, errlen);
        return -1;
    }
    return a.ret;
}

// gomupdf_add_polygon runs gomupdf_add_polygon_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    float *pts;
    int npts;
    float *stroke_rgb;
    float *fill_rgb;
    float width;
    float opacity;
    char *err;
    int errlen;
    int ret;
} gomupdf_add_polygon_args;

static void gomupdf_add_polygon_thunk(void *p) {
    gomupdf_add_polygon_args *a = p;
    a->ret = gomupdf_add_polygon_run(a->ctx, a->doc, a->pageno, a->pts, a->npts, a->stroke_rgb, a->fill_rgb, a->width, a->opacity, a->err, a->errlen);
}

static int gomupdf_add_polygon(fz_context *ctx, fz_document *doc, int pageno, float *pts, int npts, float *stroke_rgb, float *fill_rgb, float width, float opacity, char *err, int errlen) {
    gomupdf_add_polygon_args a = {ctx, doc, pageno, pts, npts, stroke_rgb, fill_rgb, width, opacity, err, errlen};
    int e = gomupdf_call_bigstack(gomupdf_add_polygon_thunk, &a);
    if (e) {
        gomupdf_bigstack_err(e, err, errlen);
        return -1;
    }
    return a.ret;
}

// gomupdf_add_polyline runs gomupdf_add_polyline_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    float *pts;
    int npts;
    float *stroke_rgb;
    float width;
    float opacity;
    char *err;
    int errlen;
    int ret;
} gomupdf_add_polyline_args;

static void gomupdf_add_polyline_thunk(void *p) {
    gomupdf_add_polyline_args *a = p;
    a->ret = gomupdf_add_polyline_run(a->ctx, a->doc, a->pageno, a->pts, a->npts, a->stroke_rgb, a->width, a->opacity, a->err, a->errlen);
}

static int gomupdf_add_polyline(fz_context *ctx, fz_document *doc, int pageno, float *pts, int npts, float *stroke_rgb, float width, float opacity, char *err, int errlen) {
    gomupdf_add_polyline_args a = {ctx, doc, pageno, pts, npts, stroke_rgb, width, opacity, err, errlen};
    int e = gomupdf_call_bigstack(gomupdf_add_polyline_thunk, &a);
    if (e) {
        gomupdf_bigstack_err(e, err, errlen);
        return -1;
    }
    return a.ret;
}

// gomupdf_add_textnote runs gomupdf_add_textnote_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    float x;
    float y;
    const char *text;
    char *err;
    int errlen;
    int ret;
} gomupdf_add_textnote_args;

static void gomupdf_add_textnote_thunk(void *p) {
    gomupdf_add_textnote_args *a = p;
    a->ret = gomupdf_add_textnote_run(a->ctx, a->doc, a->pageno, a->x, a->y, a->text, a->err, a->errlen);
}

static int gomupdf_add_textnote(fz_context *ctx, fz_document *doc, int pageno, float x, float y, const char *text, char *err, int errlen) {
    gomupdf_add_textnote_args a = {ctx, doc, pageno, x, y, text, err, errlen};
    int e = gomupdf_call_bigstack(gomupdf_add_textnote_thunk, &a);
    if (e) {
        gomupdf_bigstack_err(e, err, errlen);
        return -1;
    }
    return a.ret;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

// resolveAnnotStyle applies defaults to an AnnotStyle and returns resolved values.
func resolveAnnotStyle(s AnnotStyle) (stroke [3]float64, hasFill bool, fill [3]float64, width, opacity float64) {
	if s.Stroke != nil {
		stroke = *s.Stroke
	} // else zero → black [0,0,0]
	if s.Fill != nil {
		hasFill = true
		fill = *s.Fill
	}
	width = s.Width
	if width <= 0 {
		width = 1
	}
	opacity = s.Opacity
	if opacity <= 0 {
		opacity = 1
	}
	return
}

func cStrokeArr(v [3]float64) [3]C.float {
	return [3]C.float{C.float(v[0]), C.float(v[1]), C.float(v[2])}
}

func float32ToC(pts []float32) []C.float {
	flat := make([]C.float, len(pts))
	for i, v := range pts {
		flat[i] = C.float(v)
	}
	return flat
}

func (d *mupdfDoc) addLineAnnot(pageNo int, a, b [2]float64, style AnnotStyle) error {
	if err := d.writable(); err != nil {
		return err
	}
	stroke, _, _, width, opacity := resolveAnnotStyle(style)
	cStroke := cStrokeArr(stroke)
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_add_line(d.ctx, d.doc, C.int(pageNo),
		C.float(a[0]), C.float(a[1]), C.float(b[0]), C.float(b[1]),
		&cStroke[0], C.float(width), C.float(opacity),
		errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: add line: " + errText(errBuf))
	}
	return nil
}

func (d *mupdfDoc) addCircleAnnot(pageNo int, rect [4]float64, style AnnotStyle) error {
	if err := d.writable(); err != nil {
		return err
	}
	stroke, hasFill, fill, width, opacity := resolveAnnotStyle(style)
	cStroke := cStrokeArr(stroke)
	var fillPtr *C.float
	var cFill [3]C.float
	if hasFill {
		cFill = cStrokeArr(fill)
		fillPtr = &cFill[0]
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_add_circle(d.ctx, d.doc, C.int(pageNo),
		C.float(rect[0]), C.float(rect[1]), C.float(rect[2]), C.float(rect[3]),
		&cStroke[0], fillPtr, C.float(width), C.float(opacity),
		errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: add circle: " + errText(errBuf))
	}
	return nil
}

func (d *mupdfDoc) addPolyAnnot(pageNo int, closed bool, pts []float32, style AnnotStyle) error {
	if err := d.writable(); err != nil {
		return err
	}
	stroke, hasFill, fill, width, opacity := resolveAnnotStyle(style)
	flat := float32ToC(pts)
	npts := len(pts) / 2
	cStroke := cStrokeArr(stroke)
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if closed {
		var fillPtr *C.float
		var cFill [3]C.float
		if hasFill {
			cFill = cStrokeArr(fill)
			fillPtr = &cFill[0]
		}
		if C.gomupdf_add_polygon(d.ctx, d.doc, C.int(pageNo),
			(*C.float)(unsafe.Pointer(&flat[0])), C.int(npts),
			&cStroke[0], fillPtr, C.float(width), C.float(opacity),
			errBuf, errBufLen) != 0 {
			return errors.New("gomupdf: add polygon: " + errText(errBuf))
		}
		return nil
	}
	if C.gomupdf_add_polyline(d.ctx, d.doc, C.int(pageNo),
		(*C.float)(unsafe.Pointer(&flat[0])), C.int(npts),
		&cStroke[0], C.float(width), C.float(opacity),
		errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: add polyline: " + errText(errBuf))
	}
	return nil
}

func (d *mupdfDoc) addInkAnnot(pageNo int, counts []int32, pts []float32, style AnnotStyle) error {
	if err := d.writable(); err != nil {
		return err
	}
	stroke, _, _, width, opacity := resolveAnnotStyle(style)
	flat := float32ToC(pts)
	cCounts := make([]C.int, len(counts))
	for i, c := range counts {
		cCounts[i] = C.int(c)
	}
	cStroke := cStrokeArr(stroke)
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_add_ink(d.ctx, d.doc, C.int(pageNo),
		(*C.float)(unsafe.Pointer(&flat[0])),
		(*C.int)(unsafe.Pointer(&cCounts[0])), C.int(len(counts)),
		&cStroke[0], C.float(width), C.float(opacity),
		errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: add ink: " + errText(errBuf))
	}
	return nil
}

func (d *mupdfDoc) addFreeText(pageNo int, rect [4]float64, text string, size float64, style AnnotStyle) error {
	if err := d.writable(); err != nil {
		return err
	}
	stroke, _, _, _, _ := resolveAnnotStyle(style)
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	cStroke := cStrokeArr(stroke)
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_add_freetext(d.ctx, d.doc, C.int(pageNo),
		C.float(rect[0]), C.float(rect[1]), C.float(rect[2]), C.float(rect[3]),
		cText, C.float(size), &cStroke[0],
		errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: add freetext: " + errText(errBuf))
	}
	return nil
}

func (d *mupdfDoc) addTextNote(pageNo int, at [2]float64, text string) error {
	if err := d.writable(); err != nil {
		return err
	}
	cText := C.CString(text)
	defer C.free(unsafe.Pointer(cText))
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_add_textnote(d.ctx, d.doc, C.int(pageNo),
		C.float(at[0]), C.float(at[1]), cText,
		errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: add text note: " + errText(errBuf))
	}
	return nil
}
