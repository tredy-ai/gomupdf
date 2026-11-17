//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include <mupdf/pdf.h>
#include <math.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <stdatomic.h>
#include "gomupdf_stext.h"

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

// --- per-glyph text removal ------------------------------------------------
//
// pdf_redact_page tests each glyph's axis-aligned box against each quad's
// axis-aligned bounds, so a quad redaction over rotated text also removes
// neighbours; and its glyph box (the font's own advance) is not always where
// extraction puts the character, so a redaction drawn from extracted geometry
// can miss its glyph. gomupdf_redact_text runs MuPDF's sanitize filter over the
// page contents (the same filter and recursion pdf_redact_page uses) with its
// own text_filter, which removes a glyph when its box hits a region (by shape
// for quads) or when extraction placed a character at the glyph's origin
// under a region. Everything is compared in PDF user space: the page is
// filtered with the identity transform, as pdf_redact_page does, /QuadPoints
// and /Rect are read raw (pdf_to_quad, pdf_dict_get_rect), and extraction's
// page-space geometry is brought back with the inverse page transform.

// A redaction region, as a ring of corners (ul ur lr ll) and its bounds.
// poly = 1: a convex quad, tested by shape. poly = 0: tested by bounds with
// MuPDF's own rule (rect redactions, and quads that are degenerate,
// self-crossing or not finite); the ring is then the bounds' corners.
typedef struct {
    int poly;
    fz_rect bounds;
    double x[4], y[4];
} gomupdf_region;

typedef struct {
    double x, y;
} gomupdf_origin;

typedef struct {
    int n;
    gomupdf_region *r;
    int no;                // origins of the characters extraction found under a region,
    gomupdf_origin *o;     // sorted by x
} gomupdf_regions;

// GOMUPDF_MAX_REDACT_REGIONS bounds the regions (quads plus rect-only
// redactions) one page's redaction annotations may hold. Annotations in a
// file can share one huge /QuadPoints array, so the sum is unbounded, and
// both our pass and pdf_redact_page cost memory and time per region; a page
// over the limit is refused (gomupdf_count_regions) before anything changes.
#define GOMUPDF_MAX_REDACT_REGIONS (1 << 16)

// gomupdf_count_regions returns the regions the redaction annotations in
// the page object's /Annots hold, from array lengths only (a shared array
// counts once per annotation), as pdf_redact_page reads them. It reads the
// page dictionary directly: loading the page loads its annotations, which
// for redactions holding millions of quads already costs seconds.
static size_t gomupdf_count_regions(fz_context *ctx, pdf_obj *pageobj) {
    pdf_obj *annots = pdf_dict_get(ctx, pageobj, PDF_NAME(Annots));
    size_t total = 0;
    int i, n = pdf_array_len(ctx, annots);
    for (i = 0; i < n; i++) {
        pdf_obj *obj = pdf_array_get(ctx, annots, i);
        if (pdf_dict_get(ctx, obj, PDF_NAME(Subtype)) == PDF_NAME(Redact)) {
            int k = pdf_array_len(ctx, pdf_dict_get(ctx, obj, PDF_NAME(QuadPoints)));
            total += k > 0 ? ((size_t)k + 7) / 8 : 1;
        }
    }
    return total;
}

// GOMUPDF_MAX_REDACT_ORIGINS bounds the characters under redactions one page
// may record; beyond it the page takes MuPDF's own text removal.
#define GOMUPDF_MAX_REDACT_ORIGINS (1 << 20)

// Two origins match when they agree to GOMUPDF_ORIGIN_TOL user units plus a
// relative 1e-6. Both come from the same text matrices; only float rounding
// (and the page transform and its inverse) separates them.
#define GOMUPDF_ORIGIN_TOL 0.01

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

// gomupdf_separated_on reports whether the axis (nx, ny) separates rings a
// and b. Touching is not separation, and a NaN never separates.
static int gomupdf_separated_on(double nx, double ny, const double *ax, const double *ay,
                                const double *bx, const double *by) {
    double amin, amax, bmin, bmax;
    int k;
    amin = amax = ax[0] * nx + ay[0] * ny;
    bmin = bmax = bx[0] * nx + by[0] * ny;
    for (k = 1; k < 4; k++) {
        double pa = ax[k] * nx + ay[k] * ny, pb = bx[k] * nx + by[k] * ny;
        if (pa < amin) amin = pa;
        if (pa > amax) amax = pa;
        if (pb < bmin) bmin = pb;
        if (pb > bmax) bmax = pb;
    }
    return amax < bmin || bmax < amin;
}

// gomupdf_separated reports whether rings a and b are disjoint: the x and y
// axes first (a cheap bounds test), then every edge normal of either ring
// (separating axis theorem, exact for convex sets, including degenerate
// rings such as segments and points). Zero-length edges give no axis. Doubt
// counts as overlap.
static int gomupdf_separated(const double *ax, const double *ay, const double *bx, const double *by) {
    const double *px[2] = { ax, bx }, *py[2] = { ay, by };
    int s, i;
    if (gomupdf_separated_on(1, 0, ax, ay, bx, by) || gomupdf_separated_on(0, 1, ax, ay, bx, by))
        return 1;
    for (s = 0; s < 2; s++) {
        for (i = 0; i < 4; i++) {
            int j = (i + 1) % 4;
            double nx = py[s][i] - py[s][j], ny = px[s][j] - px[s][i];
            if (nx == 0 && ny == 0) continue;
            if (gomupdf_separated_on(nx, ny, ax, ay, bx, by)) return 1;
        }
    }
    return 0;
}

static int gomupdf_cmp_origin(const void *a, const void *b) {
    double x = ((const gomupdf_origin *)a)->x, y = ((const gomupdf_origin *)b)->x;
    return x < y ? -1 : x > y ? 1 : 0;
}

// gomupdf_origin_hit reports whether (x, y) matches a recorded origin.
static int gomupdf_origin_hit(const gomupdf_regions *rs, double x, double y) {
    double tol = GOMUPDF_ORIGIN_TOL + 1e-6 * (fabs(x) + fabs(y));
    int lo = 0, hi = rs->no, i;
    if (!(x == x) || !(y == y)) return 0; // NaN: left to the box rule
    while (lo < hi) { // first origin with ox >= x - tol
        int mid = lo + (hi - lo) / 2;
        if (rs->o[mid].x < x - tol) lo = mid + 1;
        else hi = mid;
    }
    for (i = lo; i < rs->no && rs->o[i].x <= x + tol; i++)
        if (fabs(rs->o[i].y - y) <= tol) return 1;
    return 0;
}

// gomupdf_glyph_filter is the sanitize text_filter: non-zero removes the
// glyph. bbox is the glyph's box in glyph space (advance by
// descender..ascender), trm and ctm take it to PDF user space.
//
// MuPDF 1.27 added three arguments to the callback (text render mode and the
// fill and stroke alpha). They do not affect whether a glyph is redacted.
#if FZ_VERSION_MAJOR > 1 || FZ_VERSION_MINOR >= 27
static int gomupdf_glyph_filter(fz_context *ctx, void *opaque, int *ucsbuf, int ucslen,
                                fz_matrix trm, fz_matrix ctm, fz_rect bbox,
                                int tr, float ca, float CA) {
#else
static int gomupdf_glyph_filter(fz_context *ctx, void *opaque, int *ucsbuf, int ucslen,
                                fz_matrix trm, fz_matrix ctm, fz_rect bbox) {
#endif
    const gomupdf_regions *rs = opaque;
    fz_matrix m = fz_concat(trm, ctm);
    // MuPDF's rule: the glyph's transformed bounds, shrunk by a tenth of
    // their width and height on each side.
    fz_rect r = fz_transform_rect(bbox, m);
    float w = r.x1 - r.x0, h = r.y1 - r.y0;
    double gx[4], gy[4], bx0, by0, bx1, by1;
    int i;
    (void)ctx; (void)ucsbuf; (void)ucslen;
    // Extraction put a character under a region at this glyph's origin.
    if (gomupdf_origin_hit(rs, m.e, m.f))
        return 1;
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

// gomupdf_collect_regions fills rs->r from the page's redaction annotations,
// mirroring pdf_redact_page's reading of them: /QuadPoints when the array is
// non-empty (8 numbers per quad, pdf_to_quad's ul ur ll lr order), else
// /Rect. Returns the number of regions, or -1 (building nothing) if the page
// has more than GOMUPDF_MAX_REDACT_REGIONS (the caller refused such pages
// already). Caller frees rs->r.
static int gomupdf_collect_regions(fz_context *ctx, pdf_page *page, gomupdf_regions *rs) {
    pdf_annot *a;
    size_t cap = 0;
    rs->n = 0;
    rs->r = NULL;
    for (a = pdf_first_annot(ctx, page); a; a = pdf_next_annot(ctx, a)) {
        pdf_obj *obj = pdf_annot_obj(ctx, a);
        if (pdf_dict_get(ctx, obj, PDF_NAME(Subtype)) == PDF_NAME(Redact)) {
            int n = pdf_array_len(ctx, pdf_dict_get(ctx, obj, PDF_NAME(QuadPoints)));
            cap += n > 0 ? ((size_t)n + 7) / 8 : 1;
            if (cap > GOMUPDF_MAX_REDACT_REGIONS)
                return -1;
        }
    }
    if (cap == 0)
        return 0;
    rs->r = fz_malloc_array(ctx, (int)cap, gomupdf_region);
    for (a = pdf_first_annot(ctx, page); a; a = pdf_next_annot(ctx, a)) {
        pdf_obj *obj = pdf_annot_obj(ctx, a);
        pdf_obj *qp;
        int n, i;
        if (pdf_dict_get(ctx, obj, PDF_NAME(Subtype)) != PDF_NAME(Redact))
            continue;
        qp = pdf_dict_get(ctx, obj, PDF_NAME(QuadPoints));
        n = pdf_array_len(ctx, qp);
        // The second walk sees what the first counted; the bound only
        // guards against that changing. Fall back rather than drop regions.
        if ((size_t)rs->n + (n > 0 ? ((size_t)n + 7) / 8 : 1) > cap)
            return -1;
        if (n <= 0) {
            gomupdf_region *g = &rs->r[rs->n++];
            fz_rect b = pdf_dict_get_rect(ctx, obj, PDF_NAME(Rect));
            memset(g, 0, sizeof *g);
            g->bounds = b;
            g->x[0] = b.x0; g->y[0] = b.y0;
            g->x[1] = b.x1; g->y[1] = b.y0;
            g->x[2] = b.x1; g->y[2] = b.y1;
            g->x[3] = b.x0; g->y[3] = b.y1;
            continue;
        }
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
            if (!g->poly) {
                fz_rect b = g->bounds;
                g->x[0] = b.x0; g->y[0] = b.y0;
                g->x[1] = b.x1; g->y[1] = b.y0;
                g->x[2] = b.x1; g->y[2] = b.y1;
                g->x[3] = b.x0; g->y[3] = b.y1;
            }
        }
    }
    return rs->n;
}

// gomupdf_char_hits reports whether an extracted character's quad (PDF user
// space), shrunk towards its centre by a tenth of each side, overlaps a
// region. A zero-width or zero-height quad lying in or on a region counts.
static int gomupdf_char_hits(const gomupdf_regions *rs, fz_quad q) {
    // Bilinear in (u, v) with u, v in [0.1, 0.9]: for the parallelograms
    // extraction produces, a tenth off each side.
    static const double uv[4][2] = { {0.1, 0.1}, {0.9, 0.1}, {0.9, 0.9}, {0.1, 0.9} };
    double cx[4], cy[4];
    int i;
    for (i = 0; i < 4; i++) {
        double u = uv[i][0], v = uv[i][1];
        cx[i] = (1 - u) * (1 - v) * q.ul.x + u * (1 - v) * q.ur.x + (1 - u) * v * q.ll.x + u * v * q.lr.x;
        cy[i] = (1 - u) * (1 - v) * q.ul.y + u * (1 - v) * q.ur.y + (1 - u) * v * q.ll.y + u * v * q.lr.y;
    }
    for (i = 0; i < rs->n; i++)
        if (!gomupdf_separated(cx, cy, rs->r[i].x, rs->r[i].y))
            return 1;
    return 0;
}

// gomupdf_collect_origins extracts the page's text as RawDict does by default
// (no flags, /ActualText ignored) and records, in PDF user space, the origin
// of every character whose quad hits a region: the point the text_filter
// sees as the glyph's origin (trm's translation). For vertical writing,
// extraction reports the point the glyph advanced from, so the origin is that
// point moved along the advance (the quad's ll->lr edge). Returns 0, or -1
// (recording nothing usable) past GOMUPDF_MAX_REDACT_ORIGINS. Caller frees
// rs->o.
static int gomupdf_collect_origins(fz_context *ctx, pdf_page *page, gomupdf_regions *rs) {
    fz_stext_page *text = NULL;
    fz_stext_options so;
    fz_matrix page_ctm, inv;
    int cap = 0, over = 0;
    fz_var(text);
    fz_var(cap);
    fz_var(over);
    memset(&so, 0, sizeof so);
    so.flags = FZ_STEXT_IGNORE_ACTUALTEXT;
    rs->no = 0;
    rs->o = NULL;
    fz_try(ctx) {
        fz_stext_block *b;
        pdf_page_transform(ctx, page, NULL, &page_ctm);
        inv = fz_invert_matrix(page_ctm);
        text = gomupdf_new_stext_page(ctx, (fz_page *)page, &so);
        for (b = text->first_block; b && !over; b = b->next) {
            fz_stext_line *l;
            if (b->type != FZ_STEXT_BLOCK_TEXT)
                continue;
            for (l = b->u.t.first_line; l && !over; l = l->next) {
                fz_stext_char *ch;
                for (ch = l->first_char; ch; ch = ch->next) {
                    fz_point o = ch->origin;
                    if (!gomupdf_char_hits(rs, fz_transform_quad(ch->quad, inv)))
                        continue;
                    if (l->wmode) {
                        o.x += ch->quad.lr.x - ch->quad.ll.x;
                        o.y += ch->quad.lr.y - ch->quad.ll.y;
                    }
                    o = fz_transform_point(o, inv);
                    if (rs->no == cap) {
                        if (cap >= GOMUPDF_MAX_REDACT_ORIGINS) { over = 1; break; }
                        cap = cap ? cap * 2 : 64;
                        rs->o = fz_realloc_array(ctx, rs->o, cap, gomupdf_origin);
                    }
                    rs->o[rs->no].x = o.x;
                    rs->o[rs->no].y = o.y;
                    rs->no++;
                }
            }
        }
        if (rs->no > 1)
            qsort(rs->o, rs->no, sizeof *rs->o, gomupdf_cmp_origin);
    }
    fz_always(ctx) {
        fz_drop_stext_page(ctx, text);
    }
    fz_catch(ctx) {
        fz_rethrow(ctx);
    }
    return over ? -1 : 0;
}

// --- declared text on redacted pages ----------------------------------------
//
// Marked content can declare text for the glyphs it wraps: /ActualText (the
// replacement text), /Alt (alternate description) and /E (expansion), in an
// inline BDC/DP property dictionary, a named one in /Resources /Properties, or
// a structure element reached through /MCID. MuPDF's redaction leaves inline
// and named ones as they are, and edits structure elements only partly, so a
// redacted word survives there. On a page we redact text on, all three keys
// are removed from all of these, wherever the redaction pass reaches.

typedef void (gomupdf_md_op)(fz_context *ctx, pdf_processor *proc, const char *tag, pdf_obj *raw, pdf_obj *cooked);

// The buffer processor's own BDC and DP, set by gomupdf_md_filter. Every
// buffer processor has the same two functions, so concurrent stores write
// the same values; atomics keep that race-free.
static _Atomic(gomupdf_md_op *) gomupdf_md_orig_BDC;
static _Atomic(gomupdf_md_op *) gomupdf_md_orig_DP;

// gomupdf_declares reports whether obj is a dictionary with declared text.
static int gomupdf_declares(fz_context *ctx, pdf_obj *obj) {
    return pdf_is_dict(ctx, obj) &&
        (pdf_dict_get(ctx, obj, PDF_NAME(ActualText)) ||
         pdf_dict_get(ctx, obj, PDF_NAME(Alt)) ||
         pdf_dict_get(ctx, obj, PDF_NAME(E)));
}

static void gomupdf_undeclare(fz_context *ctx, pdf_obj *dict) {
    pdf_dict_del(ctx, dict, PDF_NAME(ActualText));
    pdf_dict_del(ctx, dict, PDF_NAME(Alt));
    pdf_dict_del(ctx, dict, PDF_NAME(E));
}

// gomupdf_md_forward passes a marked-content operator on to orig, with an
// inline property dictionary that declares text replaced by a stripped copy
// (the parser's dictionary is not ours to edit).
static void gomupdf_md_forward(fz_context *ctx, gomupdf_md_op *orig, pdf_processor *proc,
                               const char *tag, pdf_obj *raw, pdf_obj *cooked) {
    pdf_obj *copy = NULL;
    if (!orig)
        return;
    if (!gomupdf_declares(ctx, raw)) {
        orig(ctx, proc, tag, raw, cooked);
        return;
    }
    copy = pdf_copy_dict(ctx, raw);
    fz_try(ctx) {
        gomupdf_undeclare(ctx, copy);
        orig(ctx, proc, tag, copy, copy);
    }
    fz_always(ctx) {
        pdf_drop_obj(ctx, copy);
    }
    fz_catch(ctx) {
        fz_rethrow(ctx);
    }
}

static void gomupdf_md_BDC(fz_context *ctx, pdf_processor *proc, const char *tag, pdf_obj *raw, pdf_obj *cooked) {
    gomupdf_md_forward(ctx, atomic_load(&gomupdf_md_orig_BDC), proc, tag, raw, cooked);
}

static void gomupdf_md_DP(fz_context *ctx, pdf_processor *proc, const char *tag, pdf_obj *raw, pdf_obj *cooked) {
    gomupdf_md_forward(ctx, atomic_load(&gomupdf_md_orig_DP), proc, tag, raw, cooked);
}

// gomupdf_md_filter is a pdf_filter_factory_fn for the last place in the
// filter list, so chain is the buffer processor that writes the new content
// stream (pdf_filter_content_stream makes one per stream). It reroutes that
// processor's BDC and DP through gomupdf_md_BDC / gomupdf_md_DP and returns
// it with a reference for the caller's filter list.
static pdf_processor *gomupdf_md_filter(fz_context *ctx, pdf_document *doc, pdf_processor *chain,
                                        int struct_parents, fz_matrix transform,
                                        pdf_filter_options *options, void *factory_options) {
    (void)doc; (void)struct_parents; (void)transform; (void)options; (void)factory_options;
    if (chain->op_BDC && chain->op_BDC != gomupdf_md_BDC) {
        atomic_store(&gomupdf_md_orig_BDC, chain->op_BDC);
        chain->op_BDC = gomupdf_md_BDC;
    }
    if (chain->op_DP && chain->op_DP != gomupdf_md_DP) {
        atomic_store(&gomupdf_md_orig_DP, chain->op_DP);
        chain->op_DP = gomupdf_md_DP;
    }
    return pdf_keep_processor(ctx, chain);
}

// Bounds on the walks below: form XObjects nested deeper than this, more
// forms than this per page, number-tree levels, and structure ancestors.
// Past the first three the walk throws (the caller reports an error rather
// than leave declared text behind); the ancestor walk just stops (only a
// cycle or a pathological tree is that deep).
#define GOMUPDF_MD_MAX_DEPTH 32
#define GOMUPDF_MD_MAX_FORMS (1 << 16)
#define GOMUPDF_MD_MAX_TREE_DEPTH 64
#define GOMUPDF_MD_MAX_ANCESTORS 256

// gomupdf_lookup_number finds key in a number tree without recursion,
// depth-bounded: binary search over /Kids by /Limits, then /Nums (sorted,
// then a linear scan, as MuPDF's own lookup allows unsorted arrays).
static pdf_obj *gomupdf_lookup_number(fz_context *ctx, pdf_obj *node, int key) {
    int depth;
    for (depth = 0; node; depth++) {
        pdf_obj *kids = pdf_dict_get(ctx, node, PDF_NAME(Kids));
        pdf_obj *nums = pdf_dict_get(ctx, node, PDF_NAME(Nums));
        pdf_obj *next = NULL;
        int l, r;
        if (depth > GOMUPDF_MD_MAX_TREE_DEPTH)
            fz_throw(ctx, FZ_ERROR_LIMIT, "structure parent tree too deep");
        if (pdf_is_array(ctx, nums)) {
            int n = pdf_array_len(ctx, nums) / 2;
            for (l = 0, r = n - 1; l <= r;) {
                int m = l + (r - l) / 2, k = pdf_array_get_int(ctx, nums, 2 * m);
                if (key < k) r = m - 1;
                else if (key > k) l = m + 1;
                else return pdf_array_get(ctx, nums, 2 * m + 1);
            }
            for (l = 0; l < n; l++)
                if (pdf_array_get_int(ctx, nums, 2 * l) == key)
                    return pdf_array_get(ctx, nums, 2 * l + 1);
        }
        if (!pdf_is_array(ctx, kids))
            return NULL;
        for (l = 0, r = pdf_array_len(ctx, kids) - 1; l <= r;) {
            int m = l + (r - l) / 2;
            pdf_obj *kid = pdf_array_get(ctx, kids, m);
            pdf_obj *lim = pdf_dict_get(ctx, kid, PDF_NAME(Limits));
            if (key < pdf_array_get_int(ctx, lim, 0)) r = m - 1;
            else if (key > pdf_array_get_int(ctx, lim, 1)) l = m + 1;
            else { next = kid; break; }
        }
        node = next;
    }
    return NULL;
}

// gomupdf_undeclare_struct strips the structure elements that own marked
// content under struct-parents key (an MCID array, or one element), and
// their ancestors up to the structure tree root. An ancestor that spans
// other pages loses its declared text there too.
static void gomupdf_undeclare_struct(fz_context *ctx, pdf_document *pdf, int key) {
    pdf_obj *root = pdf_dict_getp(ctx, pdf_trailer(ctx, pdf), "Root/StructTreeRoot");
    pdf_obj *entry;
    int i, n;
    if (!pdf_is_dict(ctx, root))
        return;
    entry = gomupdf_lookup_number(ctx, pdf_dict_get(ctx, root, PDF_NAME(ParentTree)), key);
    n = pdf_is_array(ctx, entry) ? pdf_array_len(ctx, entry) : 1;
    for (i = 0; i < n; i++) {
        pdf_obj *e = pdf_is_array(ctx, entry) ? pdf_array_get(ctx, entry, i) : entry;
        int up;
        for (up = 0; up < GOMUPDF_MD_MAX_ANCESTORS && pdf_is_dict(ctx, e); up++) {
            if (pdf_objcmp(ctx, e, root) == 0 ||
                pdf_dict_get(ctx, e, PDF_NAME(Type)) == PDF_NAME(StructTreeRoot))
                break;
            gomupdf_undeclare(ctx, e);
            e = pdf_dict_get(ctx, e, PDF_NAME(P));
        }
    }
}

// gomupdf_undeclare_res strips the named properties in res (each that
// declares text is replaced, in this /Properties dictionary only, by a
// stripped copy, so other users of the original keep it), then walks the
// form XObjects res names, and strips the structure elements of each form
// with /StructParents. After the redaction passes, res and the forms in it
// are this page's own instances, made by MuPDF's filter.
static void gomupdf_undeclare_res(fz_context *ctx, pdf_document *pdf, pdf_obj *res, int depth, int *forms) {
    pdf_obj *props = pdf_dict_get(ctx, res, PDF_NAME(Properties));
    pdf_obj *xobjs = pdf_dict_get(ctx, res, PDF_NAME(XObject));
    int i, n;
    if (depth > GOMUPDF_MD_MAX_DEPTH)
        fz_throw(ctx, FZ_ERROR_LIMIT, "form XObjects nested too deep");
    n = pdf_dict_len(ctx, props);
    for (i = 0; i < n; i++) {
        pdf_obj *v = pdf_dict_get_val(ctx, props, i);
        if (gomupdf_declares(ctx, v)) {
            pdf_obj *copy = pdf_copy_dict(ctx, v);
            fz_try(ctx) {
                gomupdf_undeclare(ctx, copy);
                if (pdf_is_indirect(ctx, v)) {
                    pdf_obj *ind = pdf_add_object(ctx, pdf, copy);
                    pdf_drop_obj(ctx, copy);
                    copy = ind;
                }
                pdf_dict_put(ctx, props, pdf_dict_get_key(ctx, props, i), copy);
            }
            fz_always(ctx) {
                pdf_drop_obj(ctx, copy);
            }
            fz_catch(ctx) {
                fz_rethrow(ctx);
            }
        }
    }
    n = pdf_dict_len(ctx, xobjs);
    for (i = 0; i < n; i++) {
        pdf_obj *x = pdf_dict_get_val(ctx, xobjs, i);
        pdf_obj *sp;
        if (pdf_dict_get(ctx, x, PDF_NAME(Subtype)) != PDF_NAME(Form))
            continue;
        if (++*forms > GOMUPDF_MD_MAX_FORMS)
            fz_throw(ctx, FZ_ERROR_LIMIT, "too many form XObjects");
        sp = pdf_dict_get(ctx, x, PDF_NAME(StructParents));
        if (pdf_is_int(ctx, sp))
            gomupdf_undeclare_struct(ctx, pdf, pdf_to_int(ctx, sp));
        gomupdf_undeclare_res(ctx, pdf, pdf_dict_get(ctx, x, PDF_NAME(Resources)), depth + 1, forms);
    }
}

// gomupdf_undeclare_page strips declared text from the page's named
// properties, its forms and its structure elements. Inline property
// dictionaries were stripped by gomupdf_md_filter during the text pass.
static void gomupdf_undeclare_page(fz_context *ctx, pdf_document *pdf, pdf_page *page) {
    int forms = 0;
    pdf_obj *sp = pdf_dict_get(ctx, page->obj, PDF_NAME(StructParents));
    if (pdf_is_int(ctx, sp))
        gomupdf_undeclare_struct(ctx, pdf, pdf_to_int(ctx, sp));
    gomupdf_undeclare_res(ctx, pdf, pdf_dict_get(ctx, page->obj, PDF_NAME(Resources)), 0, &forms);
}

// --- structure references to replaced forms ---------------------------------
//
// Redaction draws a filtered copy of every form XObject the page uses
// (MuPDF's instancing: pdf_copy_dict of the form, so the copy keeps its
// /StructParents, with a new stream and resources). A structure element can
// name the original form's stream in an MCR (/Stm) or the form itself in an
// OBJR (/Obj); that reference keeps the unredacted original in the saved
// file. Before the passes, each form the page reaches is tagged with a
// private key (GOMUPDF_FORM_TAG = its index); copies inherit it, so after the
// passes each final copy names its original. Then the page's own references
// are moved to the copy when the page drew the form once, and dropped
// otherwise. The originals are left in place, untagged, for other pages.

#define GOMUPDF_FORM_TAG "GomupdfRedactSrc"

typedef struct {
    int n, cap;
    pdf_obj **orig;   // tagged originals (kept)
    int *copies;      // final copies of each on the page
    pdf_obj **copy;   // the last of them (borrowed)
} gomupdf_forms;

// gomupdf_tag_forms tags every form XObject reachable from res (depth- and
// count-bounded; a form already tagged is not walked again).
static void gomupdf_tag_forms(fz_context *ctx, pdf_obj *res, int depth, gomupdf_forms *f) {
    pdf_obj *xobjs = pdf_dict_get(ctx, res, PDF_NAME(XObject));
    int i, n = pdf_dict_len(ctx, xobjs);
    if (depth > GOMUPDF_MD_MAX_DEPTH)
        fz_throw(ctx, FZ_ERROR_LIMIT, "form XObjects nested too deep");
    for (i = 0; i < n; i++) {
        pdf_obj *x = pdf_dict_get_val(ctx, xobjs, i);
        if (pdf_dict_get(ctx, x, PDF_NAME(Subtype)) != PDF_NAME(Form) || pdf_dict_gets(ctx, x, GOMUPDF_FORM_TAG))
            continue;
        if (f->n == f->cap) {
            int cap = f->cap ? f->cap * 2 : 16;
            if (f->cap >= GOMUPDF_MD_MAX_FORMS)
                fz_throw(ctx, FZ_ERROR_LIMIT, "too many form XObjects");
            f->orig = fz_realloc_array(ctx, f->orig, cap, pdf_obj *);
            f->copies = fz_realloc_array(ctx, f->copies, cap, int);
            f->copy = fz_realloc_array(ctx, f->copy, cap, pdf_obj *);
            f->cap = cap;
        }
        f->orig[f->n] = pdf_keep_obj(ctx, x);
        f->copies[f->n] = 0;
        f->copy[f->n] = NULL;
        f->n++;
        pdf_dict_puts_drop(ctx, x, GOMUPDF_FORM_TAG, pdf_new_int(ctx, f->n - 1));
        gomupdf_tag_forms(ctx, pdf_dict_get(ctx, x, PDF_NAME(Resources)), depth + 1, f);
    }
}

// gomupdf_form_index returns the tag of a tagged form, or -1.
static int gomupdf_form_index(fz_context *ctx, pdf_obj *x, const gomupdf_forms *f) {
    pdf_obj *tag = pdf_dict_gets(ctx, x, GOMUPDF_FORM_TAG);
    int i;
    if (!pdf_is_int(ctx, tag))
        return -1;
    i = pdf_to_int(ctx, tag);
    return i >= 0 && i < f->n ? i : -1;
}

// gomupdf_count_copies walks the forms the page now draws, counts the copies
// of each original and untags them.
static void gomupdf_count_copies(fz_context *ctx, pdf_obj *res, int depth, int *seen, gomupdf_forms *f) {
    pdf_obj *xobjs = pdf_dict_get(ctx, res, PDF_NAME(XObject));
    int i, n = pdf_dict_len(ctx, xobjs);
    if (depth > GOMUPDF_MD_MAX_DEPTH)
        fz_throw(ctx, FZ_ERROR_LIMIT, "form XObjects nested too deep");
    for (i = 0; i < n; i++) {
        pdf_obj *x = pdf_dict_get_val(ctx, xobjs, i);
        int k;
        if (pdf_dict_get(ctx, x, PDF_NAME(Subtype)) != PDF_NAME(Form))
            continue;
        if (++*seen > GOMUPDF_MD_MAX_FORMS)
            fz_throw(ctx, FZ_ERROR_LIMIT, "too many form XObjects");
        k = gomupdf_form_index(ctx, x, f);
        if (k >= 0 && pdf_to_num(ctx, x) != pdf_to_num(ctx, f->orig[k])) {
            f->copies[k]++;
            f->copy[k] = x;
            pdf_dict_dels(ctx, x, GOMUPDF_FORM_TAG);
        } else if (k >= 0) {
            // Not copied (MuPDF draws the original itself, e.g. in a cycle).
            f->copies[k] = 2;
        }
        gomupdf_count_copies(ctx, pdf_dict_get(ctx, x, PDF_NAME(Resources)), depth + 1, seen, f);
    }
}

// gomupdf_same_obj reports whether a and b are the same indirect object.
static int gomupdf_same_obj(fz_context *ctx, pdf_obj *a, pdf_obj *b) {
    return pdf_is_indirect(ctx, a) && pdf_is_indirect(ctx, b) && pdf_to_num(ctx, a) == pdf_to_num(ctx, b);
}

// gomupdf_retarget fixes one /K item of element e: an MCR (/Stm) or OBJR
// (/Obj) naming a tagged original, on this page (its /Pg, else e's /Pg, is
// this page or absent). Returns 1 if the item must be removed from /K.
static int gomupdf_retarget(fz_context *ctx, pdf_obj *item, pdf_obj *e, pdf_obj *pageobj, const gomupdf_forms *f) {
    pdf_obj *target, *pg;
    const char *key;
    int k;
    if (!pdf_is_dict(ctx, item))
        return 0;
    key = pdf_dict_gets(ctx, item, "Stm") ? "Stm" : "Obj";
    target = pdf_dict_gets(ctx, item, key);
    if (!target || (k = gomupdf_form_index(ctx, target, f)) < 0)
        return 0;
    pg = pdf_dict_gets(ctx, item, "Pg");
    if (!pg)
        pg = pdf_dict_gets(ctx, e, "Pg");
    if (pg && !gomupdf_same_obj(ctx, pg, pageobj))
        return 0; // another page's reference
    if (f->copies[k] == 1) {
        pdf_dict_puts(ctx, item, key, f->copy[k]);
        return 0;
    }
    if (key[0] == 'S') {
        pdf_dict_dels(ctx, item, key);
        return 0;
    }
    return 1; // an OBJR without its object means nothing
}

// gomupdf_retarget_struct fixes the /K of the elements under struct-parents
// key.
static void gomupdf_retarget_struct(fz_context *ctx, pdf_document *pdf, pdf_obj *pageobj, int key, const gomupdf_forms *f) {
    pdf_obj *root = pdf_dict_getp(ctx, pdf_trailer(ctx, pdf), "Root/StructTreeRoot");
    pdf_obj *entry;
    int i, n;
    if (!pdf_is_dict(ctx, root))
        return;
    entry = gomupdf_lookup_number(ctx, pdf_dict_get(ctx, root, PDF_NAME(ParentTree)), key);
    n = pdf_is_array(ctx, entry) ? pdf_array_len(ctx, entry) : 1;
    for (i = 0; i < n; i++) {
        pdf_obj *e = pdf_is_array(ctx, entry) ? pdf_array_get(ctx, entry, i) : entry;
        pdf_obj *kids = pdf_dict_get(ctx, e, PDF_NAME(K));
        if (pdf_is_array(ctx, kids)) {
            int j;
            for (j = pdf_array_len(ctx, kids) - 1; j >= 0; j--)
                if (gomupdf_retarget(ctx, pdf_array_get(ctx, kids, j), e, pageobj, f))
                    pdf_array_delete(ctx, kids, j);
        } else if (gomupdf_retarget(ctx, kids, e, pageobj, f)) {
            pdf_dict_del(ctx, e, PDF_NAME(K));
        }
    }
}

// gomupdf_retarget_forms_res walks the forms the page now draws and fixes
// the elements under each one's /StructParents.
static void gomupdf_retarget_forms_res(fz_context *ctx, pdf_document *pdf, pdf_obj *pageobj, pdf_obj *res,
                                       int depth, int *seen, const gomupdf_forms *f) {
    pdf_obj *xobjs = pdf_dict_get(ctx, res, PDF_NAME(XObject));
    int i, n = pdf_dict_len(ctx, xobjs);
    if (depth > GOMUPDF_MD_MAX_DEPTH)
        fz_throw(ctx, FZ_ERROR_LIMIT, "form XObjects nested too deep");
    for (i = 0; i < n; i++) {
        pdf_obj *x = pdf_dict_get_val(ctx, xobjs, i);
        pdf_obj *sp;
        if (pdf_dict_get(ctx, x, PDF_NAME(Subtype)) != PDF_NAME(Form))
            continue;
        if (++*seen > GOMUPDF_MD_MAX_FORMS)
            fz_throw(ctx, FZ_ERROR_LIMIT, "too many form XObjects");
        sp = pdf_dict_get(ctx, x, PDF_NAME(StructParents));
        if (pdf_is_int(ctx, sp))
            gomupdf_retarget_struct(ctx, pdf, pageobj, pdf_to_int(ctx, sp), f);
        // A form that is a structure object itself: its element.
        sp = pdf_dict_get(ctx, x, PDF_NAME(StructParent));
        if (pdf_is_int(ctx, sp))
            gomupdf_retarget_struct(ctx, pdf, pageobj, pdf_to_int(ctx, sp), f);
        gomupdf_retarget_forms_res(ctx, pdf, pageobj, pdf_dict_get(ctx, x, PDF_NAME(Resources)), depth + 1, seen, f);
    }
}

// gomupdf_tree_budget is the node budget of a whole-structure-tree walk.
static size_t gomupdf_tree_budget(fz_context *ctx, pdf_document *pdf) {
    return 16 * (size_t)pdf_xref_len(ctx, pdf) + 65536;
}

typedef struct {
    pdf_obj *obj;    // a /K value: element, MCR, OBJR, MCID or array of them
    pdf_obj *owner;  // the element whose /K holds it
    pdf_obj *array;  // the /K array holding it, or NULL
} gomupdf_tree_item;

// gomupdf_retarget_tree walks the whole structure tree from the root's /K
// (iteratively; indirect objects visited once, marks sized by the xref; at
// most gomupdf_tree_budget items, else it throws) and fixes every MCR or
// OBJR naming a tagged original on this page. It finds references the
// /ParentTree does not lead to, such as an MCR naming a form without
// /StructParents.
static void gomupdf_retarget_tree(fz_context *ctx, pdf_document *pdf, pdf_obj *pageobj, const gomupdf_forms *f) {
    pdf_obj *root = pdf_dict_getp(ctx, pdf_trailer(ctx, pdf), "Root/StructTreeRoot");
    size_t budget = gomupdf_tree_budget(ctx, pdf), used = 0;
    int xlen = pdf_xref_len(ctx, pdf), n = 0, cap = 0;
    unsigned char *seen = NULL;
    gomupdf_tree_item *stack = NULL;
    if (!pdf_is_dict(ctx, root) || f->n == 0)
        return;
    fz_var(seen);
    fz_var(stack);
    fz_try(ctx) {
        seen = fz_calloc(ctx, xlen > 0 ? xlen : 1, 1);
        cap = 64;
        stack = fz_malloc_array(ctx, cap, gomupdf_tree_item);
        stack[n].obj = pdf_dict_get(ctx, root, PDF_NAME(K));
        stack[n].owner = NULL;
        stack[n].array = NULL;
        n++;
        while (n > 0) {
            gomupdf_tree_item it = stack[--n];
            int num = pdf_to_num(ctx, it.obj);
            if (++used > budget)
                fz_throw(ctx, FZ_ERROR_LIMIT, "structure tree too large");
            if (pdf_is_indirect(ctx, it.obj) && num > 0 && num < xlen) {
                if (seen[num])
                    continue;
                seen[num] = 1;
            }
            if (pdf_is_array(ctx, it.obj)) {
                int i, len = pdf_array_len(ctx, it.obj);
                for (i = 0; i < len; i++) {
                    if (n == cap) {
                        cap *= 2;
                        stack = fz_realloc_array(ctx, stack, cap, gomupdf_tree_item);
                    }
                    stack[n].obj = pdf_array_get(ctx, it.obj, i);
                    stack[n].owner = it.owner;
                    stack[n].array = it.obj;
                    n++;
                }
            } else if (pdf_is_dict(ctx, it.obj)) {
                if (pdf_dict_gets(ctx, it.obj, "Stm") || pdf_dict_gets(ctx, it.obj, "Obj")) {
                    if (it.owner && gomupdf_retarget(ctx, it.obj, it.owner, pageobj, f)) {
                        if (it.array) {
                            int i;
                            for (i = pdf_array_len(ctx, it.array) - 1; i >= 0; i--)
                                if (pdf_array_get(ctx, it.array, i) == it.obj) {
                                    pdf_array_delete(ctx, it.array, i);
                                    break;
                                }
                        } else {
                            pdf_dict_del(ctx, it.owner, PDF_NAME(K));
                        }
                    }
                } else {
                    if (n == cap) {
                        cap *= 2;
                        stack = fz_realloc_array(ctx, stack, cap, gomupdf_tree_item);
                    }
                    stack[n].obj = pdf_dict_get(ctx, it.obj, PDF_NAME(K));
                    stack[n].owner = it.obj;
                    stack[n].array = NULL;
                    n++;
                }
            }
        }
    }
    fz_always(ctx) {
        fz_free(ctx, seen);
        fz_free(ctx, stack);
    }
    fz_catch(ctx) {
        fz_rethrow(ctx);
    }
}

// gomupdf_retarget_page moves this page's structure references from the
// tagged originals to their copies (see above).
static void gomupdf_retarget_page(fz_context *ctx, pdf_document *pdf, pdf_page *page, gomupdf_forms *f) {
    pdf_obj *res = pdf_dict_get(ctx, page->obj, PDF_NAME(Resources));
    pdf_obj *sp = pdf_dict_get(ctx, page->obj, PDF_NAME(StructParents));
    int seen = 0;
    if (f->n == 0)
        return;
    gomupdf_count_copies(ctx, res, 0, &seen, f);
    if (pdf_is_int(ctx, sp))
        gomupdf_retarget_struct(ctx, pdf, page->obj, pdf_to_int(ctx, sp), f);
    seen = 0;
    gomupdf_retarget_forms_res(ctx, pdf, page->obj, res, 0, &seen, f);
    gomupdf_retarget_tree(ctx, pdf, page->obj, f);
}

// gomupdf_untag_forms removes the tags from the originals and releases f.
// It must not throw (it runs in fz_always).
static void gomupdf_untag_forms(fz_context *ctx, gomupdf_forms *f) {
    int i;
    for (i = 0; i < f->n; i++) {
        fz_try(ctx)
            pdf_dict_dels(ctx, f->orig[i], GOMUPDF_FORM_TAG);
        fz_catch(ctx)
            fz_ignore_error(ctx);
        pdf_drop_obj(ctx, f->orig[i]);
    }
    fz_free(ctx, f->orig);
    fz_free(ctx, f->copies);
    fz_free(ctx, f->copy);
    memset(f, 0, sizeof *f);
}

// gomupdf_redact_text rewrites the page's contents (and the form XObjects
// they draw) through MuPDF's sanitize filter, stripping declared text from
// inline marked-content properties, and removes the text under the page's
// redaction annotations glyph by glyph. Returns 1 if it removed the text, 0
// if the page has more characters under redactions than
// GOMUPDF_MAX_REDACT_ORIGINS and needs MuPDF's own text removal (the
// declared text is stripped either way). Throws.
static int gomupdf_redact_text(fz_context *ctx, pdf_document *pdf, pdf_page *page) {
    gomupdf_regions rs;
    int per_glyph = 0;
    memset(&rs, 0, sizeof rs);
    fz_var(rs);
    fz_var(per_glyph);
    fz_try(ctx) {
        pdf_sanitize_filter_options sopts;
        pdf_filter_factory filters[3];
        pdf_filter_options fopts;
        if (gomupdf_collect_regions(ctx, page, &rs) < 0)
            fz_throw(ctx, FZ_ERROR_LIMIT, "too many redaction regions");
        per_glyph = gomupdf_collect_origins(ctx, page, &rs) == 0;
        memset(&sopts, 0, sizeof sopts);
        memset(filters, 0, sizeof filters);
        memset(&fopts, 0, sizeof fopts);
        sopts.opaque = &rs;
        if (per_glyph)
            sopts.text_filter = gomupdf_glyph_filter;
        filters[0].filter = pdf_new_sanitize_filter;
        filters[0].options = &sopts;
        filters[1].filter = gomupdf_md_filter;
        // As pdf_redact_page: page contents and (instanced) form XObjects;
        // not patterns, soft masks, Type3 glyphs or annotation appearances.
        fopts.recurse = 0;
        fopts.instance_forms = 1;
        fopts.ascii = 1;
        fopts.filters = filters;
        pdf_filter_page_contents(ctx, pdf, page, &fopts);
    }
    fz_always(ctx) {
        fz_free(ctx, rs.r);
        fz_free(ctx, rs.o);
    }
    fz_catch(ctx) {
        fz_rethrow(ctx);
    }
    return per_glyph;
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
    size_t regions = 0;
    gomupdf_forms forms;
    memset(&forms, 0, sizeof forms);
    fz_var(page);
    fz_var(regions);
    fz_var(forms);
    fz_try(ctx) {
        // Refuse a page whose redactions hold too many regions, in every
        // text mode, before changing (or even loading) anything.
        regions = gomupdf_count_regions(ctx, pdf_lookup_page_obj(ctx, pdf, pageno));
        if (regions <= GOMUPDF_MAX_REDACT_REGIONS) {
            pdf_annot *a;
            page = pdf_load_page(ctx, pdf, pageno);
            for (a = pdf_first_annot(ctx, page); a; a = pdf_next_annot(ctx, a)) {
                if (pdf_annot_type(ctx, a) == PDF_ANNOT_REDACT) {
                    count++;
                }
            }
        }
        if (count > 0) {
            // Our pass removes the text glyph by glyph and pdf_redact_page
            // does the rest (boxes, images, line art, links, FreeText,
            // deleting the annotations) without its own text removal. Past
            // the character limit, MuPDF's text removal stays on. Declared
            // text goes from every page whose text we redact.
            // Every mode replaces the page's forms with redacted copies, so
            // the page's structure references follow them.
            int undeclare = opts.text == PDF_REDACT_TEXT_REMOVE;
            gomupdf_tag_forms(ctx, pdf_dict_get(ctx, page->obj, PDF_NAME(Resources)), 0, &forms);
            if (undeclare && gomupdf_redact_text(ctx, pdf, page))
                opts.text = PDF_REDACT_TEXT_NONE;
            pdf_redact_page(ctx, pdf, page, &opts);
            if (undeclare)
                gomupdf_undeclare_page(ctx, pdf, page);
            gomupdf_retarget_page(ctx, pdf, page, &forms);
        }
    }
    fz_always(ctx) {
        gomupdf_untag_forms(ctx, &forms);
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    if (regions > GOMUPDF_MAX_REDACT_REGIONS) {
        snprintf(err, errlen, "too many redaction regions on page %d: %zu, limit %d",
                 pageno, regions, GOMUPDF_MAX_REDACT_REGIONS);
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
