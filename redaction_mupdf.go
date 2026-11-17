//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include <mupdf/pdf.h>
#include <math.h>
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

// --- per-glyph text removal for quad redactions ---------------------------
//
// pdf_redact_page tests each glyph's axis-aligned box against each quad's
// axis-aligned bounds, so a quad redaction over rotated text also removes
// neighbours. gomupdf_redact_text_per_glyph runs MuPDF's sanitize filter over
// the page contents (the same filter and recursion pdf_redact_page uses) with
// its own text_filter, which tests each glyph's own quad against each quad's
// polygon. Everything is compared in PDF user space: the page is filtered
// with the identity transform, as pdf_redact_page does, and /QuadPoints and
// /Rect are read raw (pdf_to_quad, pdf_dict_get_rect), not through the page
// transform.

// A redaction region. poly = 1: a convex quad, corners as a ring
// (ul ur lr ll), tested exactly. poly = 0: bounds only, with MuPDF's own rule
// (rect redactions, and quads that are degenerate, self-crossing or not
// finite).
typedef struct {
    int poly;
    fz_rect bounds;
    double x[4], y[4];
} gomupdf_region;

typedef struct {
    int n;
    gomupdf_region *r;
} gomupdf_regions;

// gomupdf_ring_convex reports whether the ring is a strictly convex
// quadrilateral (either winding) with finite corners.
static int gomupdf_ring_convex(const double *x, const double *y) {
    int pos = 0, neg = 0, i;
    for (i = 0; i < 4; i++)
        if (!isfinite(x[i]) || !isfinite(y[i])) return 0;
    for (i = 0; i < 4; i++) {
        int j = (i + 1) % 4, k = (i + 2) % 4;
        double c = (x[j] - x[i]) * (y[k] - y[j]) - (y[j] - y[i]) * (x[k] - x[j]);
        if (c > 0) pos++;
        else if (c < 0) neg++;
    }
    return pos == 4 || neg == 4;
}

// gomupdf_separated reports whether some edge normal of ring a or ring b
// separates them (separating axis theorem, exact for convex sets, including
// degenerate rings such as segments and points). Zero-length edges give no
// axis. Touching is not separation, and a NaN never separates, so doubt
// counts as overlap.
static int gomupdf_separated(const double *ax, const double *ay, const double *bx, const double *by) {
    const double *px[2] = { ax, bx }, *py[2] = { ay, by };
    int s, i, k;
    for (s = 0; s < 2; s++) {
        for (i = 0; i < 4; i++) {
            int j = (i + 1) % 4;
            double nx = py[s][i] - py[s][j], ny = px[s][j] - px[s][i];
            double amin, amax, bmin, bmax;
            if (nx == 0 && ny == 0) continue;
            amin = amax = ax[0] * nx + ay[0] * ny;
            bmin = bmax = bx[0] * nx + by[0] * ny;
            for (k = 1; k < 4; k++) {
                double pa = ax[k] * nx + ay[k] * ny, pb = bx[k] * nx + by[k] * ny;
                if (pa < amin) amin = pa;
                if (pa > amax) amax = pa;
                if (pb < bmin) bmin = pb;
                if (pb > bmax) bmax = pb;
            }
            if (amax < bmin || bmax < amin) return 1;
        }
    }
    return 0;
}

// gomupdf_glyph_filter is the sanitize text_filter: non-zero removes the
// glyph. bbox is the glyph's box in glyph space (advance by
// descender..ascender), trm and ctm take it to PDF user space.
static int gomupdf_glyph_filter(fz_context *ctx, void *opaque, int *ucsbuf, int ucslen,
                                fz_matrix trm, fz_matrix ctm, fz_rect bbox) {
    const gomupdf_regions *rs = opaque;
    fz_matrix m = fz_concat(trm, ctm);
    // MuPDF's rule: the glyph's transformed bounds, shrunk by a tenth of
    // their width and height on each side.
    fz_rect r = fz_transform_rect(bbox, m);
    float w = r.x1 - r.x0, h = r.y1 - r.y0;
    double gx[4], gy[4], bx0, by0, bx1, by1;
    int i;
    r.x0 += w / 10; r.x1 -= w / 10;
    r.y0 += h / 10; r.y1 -= h / 10;
    // The glyph's own quad, shrunk the same way in glyph space (a tenth of
    // its advance and of its height off each side), so glyphs that merely
    // touch a region are not hit. Its bounds equal MuPDF's shrunk rect.
    bx0 = bbox.x0 + (bbox.x1 - bbox.x0) / 10.0;
    bx1 = bbox.x1 - (bbox.x1 - bbox.x0) / 10.0;
    by0 = bbox.y0 + (bbox.y1 - bbox.y0) / 10.0;
    by1 = bbox.y1 - (bbox.y1 - bbox.y0) / 10.0;
    {
        double cx[4] = { bx0, bx1, bx1, bx0 }, cy[4] = { by0, by0, by1, by1 };
        for (i = 0; i < 4; i++) {
            gx[i] = cx[i] * m.a + cy[i] * m.c + m.e;
            gy[i] = cx[i] * m.b + cy[i] * m.d + m.f;
        }
    }
    (void)ctx; (void)ucsbuf; (void)ucslen;
    for (i = 0; i < rs->n; i++) {
        const gomupdf_region *g = &rs->r[i];
        if (g->poly) {
            if (!gomupdf_separated(gx, gy, g->x, g->y)) return 1;
        } else if (fz_is_valid_rect(fz_intersect_rect(r, g->bounds))) {
            // A valid (not merely non-empty) intersection, as MuPDF tests,
            // so zero-width glyphs count.
            return 1;
        }
    }
    return 0;
}

// gomupdf_collect_regions fills *rs from the page's redaction annotations,
// mirroring pdf_redact_page's reading of them: /QuadPoints when the array is
// non-empty (8 numbers per quad, pdf_to_quad's ul ur ll lr order), else
// /Rect. Returns whether any annotation had /QuadPoints. Caller frees rs->r.
static int gomupdf_collect_regions(fz_context *ctx, pdf_page *page, gomupdf_regions *rs) {
    pdf_annot *a;
    int any_quads = 0, cap = 0;
    for (a = pdf_first_annot(ctx, page); a; a = pdf_next_annot(ctx, a)) {
        pdf_obj *obj = pdf_annot_obj(ctx, a);
        if (pdf_dict_get(ctx, obj, PDF_NAME(Subtype)) == PDF_NAME(Redact)) {
            int n = pdf_array_len(ctx, pdf_dict_get(ctx, obj, PDF_NAME(QuadPoints)));
            cap += n > 0 ? (n + 7) / 8 : 1;
        }
    }
    rs->n = 0;
    rs->r = fz_malloc_array(ctx, cap > 0 ? cap : 1, gomupdf_region);
    for (a = pdf_first_annot(ctx, page); a; a = pdf_next_annot(ctx, a)) {
        pdf_obj *obj = pdf_annot_obj(ctx, a);
        pdf_obj *qp;
        int n, i;
        if (pdf_dict_get(ctx, obj, PDF_NAME(Subtype)) != PDF_NAME(Redact))
            continue;
        qp = pdf_dict_get(ctx, obj, PDF_NAME(QuadPoints));
        n = pdf_array_len(ctx, qp);
        if (n <= 0) {
            gomupdf_region *g = &rs->r[rs->n++];
            memset(g, 0, sizeof *g);
            g->bounds = pdf_dict_get_rect(ctx, obj, PDF_NAME(Rect));
            continue;
        }
        any_quads = 1;
        for (i = 0; i < n; i += 8) {
            fz_quad q = pdf_to_quad(ctx, qp, i);
            gomupdf_region *g = &rs->r[rs->n++];
            memset(g, 0, sizeof *g);
            g->bounds = fz_rect_from_quad(q);
            g->x[0] = q.ul.x; g->y[0] = q.ul.y;
            g->x[1] = q.ur.x; g->y[1] = q.ur.y;
            g->x[2] = q.lr.x; g->y[2] = q.lr.y;
            g->x[3] = q.ll.x; g->y[3] = q.ll.y;
            g->poly = gomupdf_ring_convex(g->x, g->y);
        }
    }
    return any_quads;
}

// gomupdf_redact_text_per_glyph removes the text under the page's redaction
// annotations, glyph by glyph, without touching anything else. Returns 1 if
// it ran (some redaction has /QuadPoints), 0 if it did nothing. Throws.
static int gomupdf_redact_text_per_glyph(fz_context *ctx, pdf_document *pdf, pdf_page *page) {
    gomupdf_regions rs = { 0, NULL };
    int ran = 0;
    fz_var(rs);
    fz_try(ctx) {
        if (gomupdf_collect_regions(ctx, page, &rs)) {
            pdf_sanitize_filter_options sopts;
            pdf_filter_factory filters[2];
            pdf_filter_options fopts;
            memset(&sopts, 0, sizeof sopts);
            memset(filters, 0, sizeof filters);
            memset(&fopts, 0, sizeof fopts);
            sopts.opaque = &rs;
            sopts.text_filter = gomupdf_glyph_filter;
            filters[0].filter = pdf_new_sanitize_filter;
            filters[0].options = &sopts;
            // As pdf_redact_page: page contents and (instanced) form
            // XObjects; not patterns, soft masks, Type3 glyphs or
            // annotation appearances.
            fopts.recurse = 0;
            fopts.instance_forms = 1;
            fopts.ascii = 1;
            fopts.filters = filters;
            pdf_filter_page_contents(ctx, pdf, page, &fopts);
            ran = 1;
        }
    }
    fz_always(ctx) {
        fz_free(ctx, rs.r);
    }
    fz_catch(ctx) {
        fz_rethrow(ctx);
    }
    return ran;
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
            // With quads, our pass removes the text glyph by glyph and
            // pdf_redact_page does the rest (boxes, images, line art, links,
            // FreeText, deleting the annotations) without its coarser text
            // removal.
            if (opts.text == PDF_REDACT_TEXT_REMOVE && gomupdf_redact_text_per_glyph(ctx, pdf, page))
                opts.text = PDF_REDACT_TEXT_NONE;
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
