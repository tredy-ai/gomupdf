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
    int exclude;  // a glyph at this origin whose first character is this is not matched (0: none)
    int c;        // the character extraction found here
} gomupdf_origin;

typedef struct {
    int n;
    gomupdf_region *r;
    int no;                // origins of the characters extraction found under a region,
    gomupdf_origin *o;     // sorted by x
    int nk, capk;          // the other characters (exclude unused), sorted
    gomupdf_origin *k;     // by x; nk = -1: not all recorded
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

// gomupdf_origin_hit reports whether a glyph at (x, y) whose characters are
// ucsbuf matches a recorded origin.
static int gomupdf_origin_hit(const gomupdf_regions *rs, double x, double y, const int *ucsbuf, int ucslen) {
    double tol = GOMUPDF_ORIGIN_TOL + 1e-6 * (fabs(x) + fabs(y));
    int lo = 0, hi = rs->no, i;
    if (!(x == x) || !(y == y)) return 0; // NaN: left to the box rule
    while (lo < hi) { // first origin with ox >= x - tol
        int mid = lo + (hi - lo) / 2;
        if (rs->o[mid].x < x - tol) lo = mid + 1;
        else hi = mid;
    }
    for (i = lo; i < rs->no && rs->o[i].x <= x + tol; i++)
        if (fabs(rs->o[i].y - y) <= tol &&
            !(rs->o[i].exclude && ucslen > 0 && ucsbuf[0] == rs->o[i].exclude))
            return 1;
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
    (void)ctx;
    // Extraction put a character under a region at this glyph's origin.
    if (gomupdf_origin_hit(rs, m.e, m.f, ucsbuf, ucslen))
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
//
// A character with no advance (zero extent along its line) can sit at
// another glyph's origin: a ligature's trailing components (extraction
// expands "fi" into f, with the glyph's quad and origin, and a zero-width i
// at the next glyph's origin), or a zero-advance mark (its origin is where
// the next glyph starts). Its origin then identifies that next glyph too.
// So when such a character's neighbour on the line (next, else previous;
// not a space extraction made up) has the same origin and an advance, its
// origin is recorded excluding glyphs whose first character is that
// neighbour's: the mark's own glyph still matches, the neighbour does not.
// A genuinely zero-width glyph (a font with /Widths 0) has no such
// neighbour, so its origin matches as before.
static int gomupdf_zero_advance(const fz_stext_char *ch) {
    return hypot(ch->quad.ur.x - ch->quad.ul.x, ch->quad.ur.y - ch->quad.ul.y) < 1e-3 &&
        hypot(ch->quad.lr.x - ch->quad.ll.x, ch->quad.lr.y - ch->quad.ll.y) < 1e-3;
}

// gomupdf_shares_origin reports whether n is a real character with an
// advance at ch's origin.
static int gomupdf_shares_origin(const fz_stext_char *ch, const fz_stext_char *n) {
    return n && !(n->flags & FZ_STEXT_SYNTHETIC) && !gomupdf_zero_advance(n) &&
        fabs(n->origin.x - ch->origin.x) < 0.01 && fabs(n->origin.y - ch->origin.y) < 0.01;
}

// gomupdf_char_origin returns ch's origin as the text_filter sees it, in
// PDF user space (inv undoes the page transform).
static fz_point gomupdf_char_origin(const fz_stext_char *ch, const fz_stext_line *l, fz_matrix inv) {
    fz_point o = ch->origin;
    if (l->wmode) {
        o.x += ch->quad.lr.x - ch->quad.ll.x;
        o.y += ch->quad.lr.y - ch->quad.ll.y;
    }
    return fz_transform_point(o, inv);
}

// gomupdf_keep_char records a character no region hits (rs->k).
static void gomupdf_keep_char(fz_context *ctx, gomupdf_regions *rs, fz_point o, int c) {
    if (rs->nk == rs->capk) {
        rs->capk = rs->capk ? rs->capk * 2 : 64;
        rs->k = fz_realloc_array(ctx, rs->k, rs->capk, gomupdf_origin);
    }
    rs->k[rs->nk].x = o.x;
    rs->k[rs->nk].y = o.y;
    rs->k[rs->nk].exclude = 0;
    rs->k[rs->nk].c = c;
    rs->nk++;
}

// A character after redaction is where it was before when its origin
// agrees to GOMUPDF_MOVED_TOL. Not GOMUPDF_ORIGIN_TOL: the rewritten content
// stream rounds coordinates, and kept text shifts by a few hundredths of a
// point (0.028 on pdfjs issue3062.pdf).
#define GOMUPDF_MOVED_TOL 0.25

// gomupdf_kept reports whether character c at o was recorded by
// gomupdf_keep_char (same character, same origin to GOMUPDF_MOVED_TOL).
static int gomupdf_kept(const gomupdf_regions *rs, fz_point o, int c) {
    double tol = GOMUPDF_MOVED_TOL;
    int lo = 0, hi = rs->nk, i;
    while (lo < hi) {
        int mid = lo + (hi - lo) / 2;
        if (rs->k[mid].x < o.x - tol) lo = mid + 1;
        else hi = mid;
    }
    for (i = lo; i < rs->nk && rs->k[i].x <= o.x + tol; i++)
        if (rs->k[i].c == c && fabs(rs->k[i].y - o.y) <= tol)
            return 1;
    return 0;
}

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
    rs->nk = rs->capk = 0;
    rs->k = NULL;
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
                fz_stext_char *ch, *prev = NULL;
                for (ch = l->first_char; ch; ch = ch->next) {
                    fz_point o = gomupdf_char_origin(ch, l, inv);
                    fz_stext_char *next = ch->next, *last = prev;
                    int exclude = 0;
                    if (!(ch->flags & FZ_STEXT_SYNTHETIC))
                        prev = ch;
                    if (!gomupdf_char_hits(rs, fz_transform_quad(ch->quad, inv))) {
                        if (!(ch->flags & FZ_STEXT_SYNTHETIC))
                            gomupdf_keep_char(ctx, rs, o, ch->c);
                        continue;
                    }
                    while (next && (next->flags & FZ_STEXT_SYNTHETIC))
                        next = next->next;
                    if (gomupdf_zero_advance(ch)) {
                        if (gomupdf_shares_origin(ch, next))
                            exclude = next->c;
                        else if (gomupdf_shares_origin(ch, last))
                            exclude = last->c;
                    }
                    if (rs->no == cap) {
                        if (cap >= GOMUPDF_MAX_REDACT_ORIGINS) { over = 1; break; }
                        cap = cap ? cap * 2 : 64;
                        rs->o = fz_realloc_array(ctx, rs->o, cap, gomupdf_origin);
                    }
                    rs->o[rs->no].x = o.x;
                    rs->o[rs->no].y = o.y;
                    rs->o[rs->no].exclude = exclude;
                    rs->o[rs->no].c = ch->c;
                    rs->no++;
                }
            }
        }
        if (rs->no > 1)
            qsort(rs->o, rs->no, sizeof *rs->o, gomupdf_cmp_origin);
        if (over)
            rs->nk = -1;
        else if (rs->nk > 1)
            qsort(rs->k, rs->nk, sizeof *rs->k, gomupdf_cmp_origin);
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

// Bounds on the structure and form walks. They are all checked by
// gomupdf_plan_page before the page changes; past one, ApplyRedactions
// refuses the page. The ancestor walk just stops (only a cycle or a
// pathological tree is that deep).
#define GOMUPDF_MD_MAX_DEPTH 32          // form XObjects nested in form XObjects
#define GOMUPDF_MD_MAX_FORMS (1 << 16)   // paths to form XObjects from the page
#define GOMUPDF_MD_MAX_TREE_DEPTH 64     // /ParentTree levels
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

// --- the plan: what redacting the page will touch -------------------------
//
// Redaction draws a filtered copy of every form XObject the page uses
// (MuPDF's instancing: pdf_copy_dict of the form, so the copy keeps its
// /StructParents and /StructParent, with a new stream and resources). A
// structure element can name the original form's stream in an MCR (/Stm) or
// the form itself in an OBJR (/Obj); that reference keeps the unredacted
// original in the saved file. And structure elements can declare text
// (/ActualText, /Alt, /E) for the page's content.
//
// gomupdf_plan_page reads, without changing anything, everything the page's
// redaction will touch in the structure and the forms, and checks every
// limit, so that a page that cannot be handled is refused untouched:
//   - the forms the page reaches (each once), counting paths and depth;
//   - the structure elements under the page's and those forms'
//     /StructParents and /StructParent, with their ancestors (whose declared
//     text is stripped);
//   - every MCR or OBJR naming one of those forms on this page (its /Pg, or
//     its element's, is this page or absent), from those elements' /K and
//     from one walk of the whole structure tree.
// After the passes only gomupdf_finish_page runs, which cannot fail on the
// file's structure. Each reference is moved to the form's copy when the page
// drew the form once, and dropped otherwise (an MCR loses /Stm, an OBJR is
// removed). The originals stay for other pages. To find each original's
// copies, the originals carry a private key (GOMUPDF_FORM_TAG = their index)
// from just before the passes until the end; copies inherit it.

#define GOMUPDF_FORM_TAG "GomupdfRedactSrc"

typedef struct {
    int key;
    pdf_obj *val;
} gomupdf_ptentry;

typedef struct {
    pdf_obj *item;   // the MCR or OBJR dictionary
    pdf_obj *owner;  // the element whose /K holds it
    pdf_obj *array;  // the /K array holding it, or NULL
    int form;        // index of the original it names
} gomupdf_ref;

typedef struct {
    int nforms, capforms;
    pdf_obj **forms;    // originals the page reaches, each once (kept)
    int *formnum;       // their object numbers
    int *copies;        // final copies of each on the page
    pdf_obj **copy;     // the last of them (borrowed)
    int nelems, capelems;
    pdf_obj **elems;    // structure elements whose declared text goes (kept)
    int nrefs, caprefs;
    gomupdf_ref *refs;  // references to the originals on this page (kept)
    int xlen;           // the xref length the plan was made at
    unsigned char *isform; // isform[num] = 1 + index, for num < xlen (0: none)
    int *formidx;
    int tagged;         // the originals carry GOMUPDF_FORM_TAG
    int ptbuilt, npt;   // every /ParentTree entry, by key, once a lookup by
    gomupdf_ptentry *pt; // /Limits has failed (values borrowed)
} gomupdf_plan;

static void gomupdf_plan_drop(fz_context *ctx, gomupdf_plan *p) {
    int i;
    for (i = 0; i < p->nforms; i++)
        pdf_drop_obj(ctx, p->forms[i]);
    for (i = 0; i < p->nelems; i++)
        pdf_drop_obj(ctx, p->elems[i]);
    for (i = 0; i < p->nrefs; i++) {
        pdf_drop_obj(ctx, p->refs[i].item);
        pdf_drop_obj(ctx, p->refs[i].owner);
        pdf_drop_obj(ctx, p->refs[i].array);
    }
    fz_free(ctx, p->forms);
    fz_free(ctx, p->formnum);
    fz_free(ctx, p->copies);
    fz_free(ctx, p->copy);
    fz_free(ctx, p->elems);
    fz_free(ctx, p->refs);
    fz_free(ctx, p->isform);
    fz_free(ctx, p->formidx);
    fz_free(ctx, p->pt);
    memset(p, 0, sizeof *p);
}

static int gomupdf_cmp_ptentry(const void *a, const void *b) {
    int x = ((const gomupdf_ptentry *)a)->key, y = ((const gomupdf_ptentry *)b)->key;
    return x < y ? -1 : x > y;
}

typedef struct {
    pdf_obj *node;
    int depth;
} gomupdf_ptnode;

// gomupdf_plan_build_pt walks the whole number tree once (iteratively,
// indirect nodes visited once, at most GOMUPDF_MD_MAX_TREE_DEPTH levels and
// 16 nodes or entries per xref entry plus 65536, else it throws) and keeps
// every /Nums entry, sorted by key. It is the fallback for a tree whose
// /Kids are out of order or whose /Limits are wrong.
static void gomupdf_plan_build_pt(fz_context *ctx, gomupdf_plan *p, pdf_obj *tree) {
    size_t budget = 16 * (size_t)p->xlen + 65536, used = 0;
    int n = 0, cap = 0, ptcap = 0;
    unsigned char *seen = NULL;
    gomupdf_ptnode *stack = NULL;
    fz_var(seen);
    fz_var(stack);
    p->ptbuilt = 1;
    fz_try(ctx) {
        seen = fz_calloc(ctx, p->xlen > 0 ? p->xlen : 1, 1);
        cap = 64;
        stack = fz_malloc_array(ctx, cap, gomupdf_ptnode);
        stack[n].node = tree;
        stack[n].depth = 0;
        n++;
        while (n > 0) {
            gomupdf_ptnode it = stack[--n];
            int num = pdf_is_indirect(ctx, it.node) ? pdf_to_num(ctx, it.node) : 0;
            pdf_obj *kids, *nums;
            int i, len;
            if (++used > budget)
                fz_throw(ctx, FZ_ERROR_LIMIT, "structure parent tree too large");
            if (it.depth > GOMUPDF_MD_MAX_TREE_DEPTH)
                fz_throw(ctx, FZ_ERROR_LIMIT, "structure parent tree too deep");
            if (num > 0 && num < p->xlen) {
                if (seen[num])
                    continue;
                seen[num] = 1;
            }
            nums = pdf_dict_get(ctx, it.node, PDF_NAME(Nums));
            len = pdf_array_len(ctx, nums) / 2;
            for (i = 0; i < len; i++) {
                if (++used > budget)
                    fz_throw(ctx, FZ_ERROR_LIMIT, "structure parent tree too large");
                if (p->npt == ptcap) {
                    ptcap = ptcap ? ptcap * 2 : 64;
                    p->pt = fz_realloc_array(ctx, p->pt, ptcap, gomupdf_ptentry);
                }
                p->pt[p->npt].key = pdf_array_get_int(ctx, nums, 2 * i);
                p->pt[p->npt].val = pdf_array_get(ctx, nums, 2 * i + 1);
                p->npt++;
            }
            kids = pdf_dict_get(ctx, it.node, PDF_NAME(Kids));
            len = pdf_array_len(ctx, kids);
            for (i = 0; i < len; i++) {
                if (n == cap) {
                    cap *= 2;
                    stack = fz_realloc_array(ctx, stack, cap, gomupdf_ptnode);
                }
                stack[n].node = pdf_array_get(ctx, kids, i);
                stack[n].depth = it.depth + 1;
                n++;
            }
        }
        if (p->npt > 1)
            qsort(p->pt, p->npt, sizeof *p->pt, gomupdf_cmp_ptentry);
    }
    fz_always(ctx) {
        fz_free(ctx, seen);
        fz_free(ctx, stack);
    }
    fz_catch(ctx) {
        fz_rethrow(ctx);
    }
}

// gomupdf_plan_lookup finds key in the /ParentTree: by /Limits first, then,
// if that finds nothing, in the whole tree.
static pdf_obj *gomupdf_plan_lookup(fz_context *ctx, gomupdf_plan *p, pdf_obj *tree, int key) {
    pdf_obj *v = gomupdf_lookup_number(ctx, tree, key);
    int lo = 0, hi;
    if (v || !tree)
        return v;
    if (!p->ptbuilt)
        gomupdf_plan_build_pt(ctx, p, tree);
    hi = p->npt;
    while (lo < hi) {
        int mid = lo + (hi - lo) / 2;
        if (p->pt[mid].key < key) lo = mid + 1;
        else hi = mid;
    }
    return lo < p->npt && p->pt[lo].key == key ? p->pt[lo].val : NULL;
}

// gomupdf_plan_form_index returns the index of the original obj is, or -1.
static int gomupdf_plan_form_index(fz_context *ctx, const gomupdf_plan *p, pdf_obj *obj) {
    int num = pdf_is_indirect(ctx, obj) ? pdf_to_num(ctx, obj) : 0;
    if (num <= 0 || num >= p->xlen || !p->isform[num])
        return -1;
    return p->formidx[num];
}

static void gomupdf_plan_add_form(fz_context *ctx, gomupdf_plan *p, pdf_obj *x, int num) {
    if (p->nforms == p->capforms) {
        int cap = p->capforms ? p->capforms * 2 : 16;
        p->forms = fz_realloc_array(ctx, p->forms, cap, pdf_obj *);
        p->formnum = fz_realloc_array(ctx, p->formnum, cap, int);
        p->copies = fz_realloc_array(ctx, p->copies, cap, int);
        p->copy = fz_realloc_array(ctx, p->copy, cap, pdf_obj *);
        p->capforms = cap;
    }
    p->forms[p->nforms] = pdf_keep_obj(ctx, x);
    p->formnum[p->nforms] = num;
    p->copies[p->nforms] = 0;
    p->copy[p->nforms] = NULL;
    p->isform[num] = 1;
    p->formidx[num] = p->nforms;
    p->nforms++;
}

// gomupdf_plan_forms records the forms reachable from res (each once) and
// counts the paths to them, which bounds the copies MuPDF's instancing
// makes. Recursion is capped at GOMUPDF_MD_MAX_DEPTH; a form already on the
// path (a cycle) is not followed.
static void gomupdf_plan_forms(fz_context *ctx, gomupdf_plan *p, pdf_obj *res, int depth, int *path, int *paths) {
    pdf_obj *xobjs = pdf_dict_get(ctx, res, PDF_NAME(XObject));
    int i, j, n = pdf_dict_len(ctx, xobjs);
    for (i = 0; i < n; i++) {
        pdf_obj *x = pdf_dict_get_val(ctx, xobjs, i);
        int num = pdf_to_num(ctx, x), cycle = 0;
        if (pdf_dict_get(ctx, x, PDF_NAME(Subtype)) != PDF_NAME(Form))
            continue;
        for (j = 0; j < depth; j++)
            if (path[j] == num)
                cycle = 1;
        if (cycle)
            continue;
        if (++*paths > GOMUPDF_MD_MAX_FORMS)
            fz_throw(ctx, FZ_ERROR_LIMIT, "too many form XObjects");
        if (depth >= GOMUPDF_MD_MAX_DEPTH)
            fz_throw(ctx, FZ_ERROR_LIMIT, "form XObjects nested too deep");
        if (num > 0 && num < p->xlen && !p->isform[num])
            gomupdf_plan_add_form(ctx, p, x, num);
        path[depth] = num;
        gomupdf_plan_forms(ctx, p, pdf_dict_get(ctx, x, PDF_NAME(Resources)), depth + 1, path, paths);
    }
}

static void gomupdf_plan_add_elem(fz_context *ctx, gomupdf_plan *p, pdf_obj *e) {
    if (p->nelems == p->capelems) {
        int cap = p->capelems ? p->capelems * 2 : 16;
        p->elems = fz_realloc_array(ctx, p->elems, cap, pdf_obj *);
        p->capelems = cap;
    }
    p->elems[p->nelems++] = pdf_keep_obj(ctx, e);
}

static int gomupdf_same_obj(fz_context *ctx, pdf_obj *a, pdf_obj *b) {
    return pdf_is_indirect(ctx, a) && pdf_is_indirect(ctx, b) && pdf_to_num(ctx, a) == pdf_to_num(ctx, b);
}

// gomupdf_plan_ref records item (a /K entry of owner, in array or alone) if
// it is an MCR or OBJR naming a planned form on this page.
static void gomupdf_plan_ref(fz_context *ctx, gomupdf_plan *p, pdf_obj *item, pdf_obj *owner,
                             pdf_obj *array, pdf_obj *pageobj) {
    pdf_obj *target, *pg;
    int k;
    gomupdf_ref *r;
    if (!pdf_is_dict(ctx, item) || !owner)
        return;
    target = pdf_dict_gets(ctx, item, "Stm");
    if (!target)
        target = pdf_dict_gets(ctx, item, "Obj");
    if (!target || (k = gomupdf_plan_form_index(ctx, p, target)) < 0)
        return;
    pg = pdf_dict_gets(ctx, item, "Pg");
    if (!pg)
        pg = pdf_dict_gets(ctx, owner, "Pg");
    if (pg && !gomupdf_same_obj(ctx, pg, pageobj))
        return; // another page's reference
    if (p->nrefs == p->caprefs) {
        int cap = p->caprefs ? p->caprefs * 2 : 16;
        p->refs = fz_realloc_array(ctx, p->refs, cap, gomupdf_ref);
        p->caprefs = cap;
    }
    r = &p->refs[p->nrefs++];
    r->item = pdf_keep_obj(ctx, item);
    r->owner = pdf_keep_obj(ctx, owner);
    r->array = pdf_keep_obj(ctx, array);
    r->form = k;
}

// gomupdf_plan_struct records the elements under struct-parents key (an
// MCID array, or one element): their references to planned forms, and, with
// undeclare, the elements and their ancestors up to the root.
static void gomupdf_plan_struct(fz_context *ctx, gomupdf_plan *p, pdf_obj *root, pdf_obj *pageobj,
                                int key, int undeclare) {
    pdf_obj *entry = gomupdf_plan_lookup(ctx, p, pdf_dict_get(ctx, root, PDF_NAME(ParentTree)), key);
    int i, n = pdf_is_array(ctx, entry) ? pdf_array_len(ctx, entry) : 1;
    for (i = 0; i < n; i++) {
        pdf_obj *e = pdf_is_array(ctx, entry) ? pdf_array_get(ctx, entry, i) : entry;
        pdf_obj *kids = pdf_dict_get(ctx, e, PDF_NAME(K));
        int up;
        if (!pdf_is_dict(ctx, e))
            continue;
        if (pdf_is_array(ctx, kids)) {
            int j, len = pdf_array_len(ctx, kids);
            for (j = 0; j < len; j++)
                gomupdf_plan_ref(ctx, p, pdf_array_get(ctx, kids, j), e, kids, pageobj);
        } else {
            gomupdf_plan_ref(ctx, p, kids, e, NULL, pageobj);
        }
        for (up = 0; undeclare && up < GOMUPDF_MD_MAX_ANCESTORS && pdf_is_dict(ctx, e); up++) {
            if (gomupdf_same_obj(ctx, e, root) ||
                pdf_dict_get(ctx, e, PDF_NAME(Type)) == PDF_NAME(StructTreeRoot))
                break;
            gomupdf_plan_add_elem(ctx, p, e);
            e = pdf_dict_get(ctx, e, PDF_NAME(P));
        }
    }
}

typedef struct {
    pdf_obj *obj;    // a /K value: element, MCR, OBJR, MCID or array of them
    pdf_obj *owner;  // the element whose /K holds it
    pdf_obj *array;  // the /K array holding it, or NULL
} gomupdf_tree_item;

// gomupdf_plan_tree walks the whole structure tree from the root's /K
// (iteratively; indirect objects visited once; at most 16 items per xref
// entry plus 65536, else it throws) and records every MCR or OBJR naming a
// planned form on this page. It finds references the /ParentTree does not
// lead to, such as an MCR naming a form without /StructParents.
static void gomupdf_plan_tree(fz_context *ctx, gomupdf_plan *p, pdf_obj *root, pdf_obj *pageobj) {
    size_t budget = 16 * (size_t)p->xlen + 65536, used = 0;
    int n = 0, cap = 0;
    unsigned char *seen = NULL;
    gomupdf_tree_item *stack = NULL;
    if (p->nforms == 0)
        return;
    fz_var(seen);
    fz_var(stack);
    fz_try(ctx) {
        seen = fz_calloc(ctx, p->xlen > 0 ? p->xlen : 1, 1);
        cap = 64;
        stack = fz_malloc_array(ctx, cap, gomupdf_tree_item);
        stack[0].obj = pdf_dict_get(ctx, root, PDF_NAME(K));
        stack[0].owner = NULL;
        stack[0].array = NULL;
        n = 1;
        while (n > 0) {
            gomupdf_tree_item it = stack[--n];
            int num = pdf_is_indirect(ctx, it.obj) ? pdf_to_num(ctx, it.obj) : 0;
            if (++used > budget)
                fz_throw(ctx, FZ_ERROR_LIMIT, "structure tree too large");
            if (num > 0 && num < p->xlen) {
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
                    gomupdf_plan_ref(ctx, p, it.obj, it.owner, it.array, pageobj);
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

// gomupdf_plan_page fills p for the page without changing anything; it
// throws if the page is past a limit. undeclare: the page's declared text
// will be stripped.
static void gomupdf_plan_page(fz_context *ctx, pdf_document *pdf, pdf_page *page, gomupdf_plan *p, int undeclare) {
    pdf_obj *root = pdf_dict_getp(ctx, pdf_trailer(ctx, pdf), "Root/StructTreeRoot");
    int path[GOMUPDF_MD_MAX_DEPTH];
    int paths = 0, i;
    pdf_obj *sp;
    p->xlen = pdf_xref_len(ctx, pdf);
    p->isform = fz_calloc(ctx, p->xlen > 0 ? p->xlen : 1, 1);
    p->formidx = fz_malloc_array(ctx, p->xlen > 0 ? p->xlen : 1, int);
    gomupdf_plan_forms(ctx, p, pdf_dict_get(ctx, page->obj, PDF_NAME(Resources)), 0, path, &paths);
    if (!pdf_is_dict(ctx, root))
        return;
    sp = pdf_dict_get(ctx, page->obj, PDF_NAME(StructParents));
    if (pdf_is_int(ctx, sp))
        gomupdf_plan_struct(ctx, p, root, page->obj, pdf_to_int(ctx, sp), undeclare);
    for (i = 0; i < p->nforms; i++) {
        sp = pdf_dict_get(ctx, p->forms[i], PDF_NAME(StructParents));
        if (pdf_is_int(ctx, sp))
            gomupdf_plan_struct(ctx, p, root, page->obj, pdf_to_int(ctx, sp), undeclare);
        // A form that is a structure object itself: its element.
        sp = pdf_dict_get(ctx, p->forms[i], PDF_NAME(StructParent));
        if (pdf_is_int(ctx, sp))
            gomupdf_plan_struct(ctx, p, root, page->obj, pdf_to_int(ctx, sp), undeclare);
    }
    gomupdf_plan_tree(ctx, p, root, page->obj);
}

// gomupdf_tag_forms tags each planned original with its index, replacing
// whatever value the file had under that key (never trusted). Every form a
// copy can come from is planned, so every tag a copy carries is ours.
static void gomupdf_tag_forms(fz_context *ctx, gomupdf_plan *p) {
    int i;
    p->tagged = 1;
    for (i = 0; i < p->nforms; i++)
        pdf_dict_puts_drop(ctx, p->forms[i], GOMUPDF_FORM_TAG, pdf_new_int(ctx, i));
}

// gomupdf_untag_forms removes the tag from the originals and from every
// object made since the plan (the copies, including the first pass's copies
// the second pass orphans), so no file keeps it. It must not throw (it runs
// in fz_always).
static void gomupdf_untag_forms(fz_context *ctx, pdf_document *pdf, gomupdf_plan *p) {
    int i, xlen = 0;
    if (!p->tagged)
        return;
    for (i = 0; i < p->nforms; i++) {
        fz_try(ctx)
            pdf_dict_dels(ctx, p->forms[i], GOMUPDF_FORM_TAG);
        fz_catch(ctx)
            fz_ignore_error(ctx);
    }
    fz_try(ctx)
        xlen = pdf_xref_len(ctx, pdf);
    fz_catch(ctx)
        fz_ignore_error(ctx);
    for (i = p->xlen; i < xlen; i++) {
        pdf_obj *obj = NULL;
        fz_var(obj);
        fz_try(ctx) {
            obj = pdf_new_indirect(ctx, pdf, i, 0);
            if (pdf_is_dict(ctx, obj) && pdf_dict_gets(ctx, obj, GOMUPDF_FORM_TAG))
                pdf_dict_dels(ctx, obj, GOMUPDF_FORM_TAG);
        }
        fz_always(ctx)
            pdf_drop_obj(ctx, obj);
        fz_catch(ctx)
            fz_ignore_error(ctx);
    }
}

// gomupdf_strip_props replaces each named property in res that declares
// text by a stripped copy, in this /Properties dictionary only (after the
// passes it is this page's own), so other users of the original keep it.
static void gomupdf_strip_props(fz_context *ctx, pdf_document *pdf, pdf_obj *res) {
    pdf_obj *props = pdf_dict_get(ctx, res, PDF_NAME(Properties));
    int i, n = pdf_dict_len(ctx, props);
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
}

// gomupdf_finish_page runs after the passes. It walks the resources and
// forms the page now draws (iteratively, each form once: no limits, it only
// visits objects MuPDF made), strips named declared text (undeclare),
// counts the copies of each original and untags them; then strips the
// planned elements (undeclare) and moves or drops the planned references.
// Nothing here depends on limits; it can fail only as MuPDF itself can
// (allocation).
static void gomupdf_finish_page(fz_context *ctx, pdf_document *pdf, pdf_page *page, gomupdf_plan *p, int undeclare) {
    int xlen = pdf_xref_len(ctx, pdf), n = 0, cap = 0, i;
    unsigned char *seen = NULL;
    pdf_obj **stack = NULL;
    fz_var(seen);
    fz_var(stack);
    fz_try(ctx) {
        seen = fz_calloc(ctx, xlen > 0 ? xlen : 1, 1);
        cap = 64;
        stack = fz_malloc_array(ctx, cap, pdf_obj *);
        stack[n++] = pdf_dict_get(ctx, page->obj, PDF_NAME(Resources));
        while (n > 0) {
            pdf_obj *res = stack[--n];
            pdf_obj *xobjs = pdf_dict_get(ctx, res, PDF_NAME(XObject));
            int j, len = pdf_dict_len(ctx, xobjs);
            if (undeclare)
                gomupdf_strip_props(ctx, pdf, res);
            for (j = 0; j < len; j++) {
                pdf_obj *x = pdf_dict_get_val(ctx, xobjs, j);
                pdf_obj *tag;
                int num = pdf_to_num(ctx, x);
                if (pdf_dict_get(ctx, x, PDF_NAME(Subtype)) != PDF_NAME(Form) || num <= 0 || num >= xlen || seen[num])
                    continue;
                seen[num] = 1;
                tag = pdf_dict_gets(ctx, x, GOMUPDF_FORM_TAG);
                if (pdf_is_int(ctx, tag) && pdf_to_int(ctx, tag) >= 0 && pdf_to_int(ctx, tag) < p->nforms) {
                    int k = pdf_to_int(ctx, tag);
                    if (num != p->formnum[k]) {
                        p->copies[k]++;
                        p->copy[k] = x;
                        pdf_dict_dels(ctx, x, GOMUPDF_FORM_TAG);
                    } else {
                        p->copies[k] = 2; // not copied (MuPDF draws the original, e.g. in a cycle)
                    }
                }
                if (n == cap) {
                    cap *= 2;
                    stack = fz_realloc_array(ctx, stack, cap, pdf_obj *);
                }
                stack[n++] = pdf_dict_get(ctx, x, PDF_NAME(Resources));
            }
        }
        if (undeclare)
            for (i = 0; i < p->nelems; i++)
                gomupdf_undeclare(ctx, p->elems[i]);
        for (i = 0; i < p->nrefs; i++) {
            gomupdf_ref *r = &p->refs[i];
            const char *key = pdf_dict_gets(ctx, r->item, "Stm") ? "Stm" : "Obj";
            pdf_obj *target = pdf_dict_gets(ctx, r->item, key);
            if (pdf_to_num(ctx, target) != p->formnum[r->form])
                continue; // already handled (found twice)
            if (p->copies[r->form] == 1) {
                pdf_dict_puts(ctx, r->item, key, p->copy[r->form]);
            } else if (key[0] == 'S') {
                pdf_dict_dels(ctx, r->item, key);
            } else if (r->array) { // an OBJR without its object means nothing
                int j;
                for (j = pdf_array_len(ctx, r->array) - 1; j >= 0; j--)
                    if (pdf_array_get(ctx, r->array, j) == r->item) {
                        pdf_array_delete(ctx, r->array, j);
                        break;
                    }
            } else if (pdf_dict_get(ctx, r->owner, PDF_NAME(K)) == r->item) {
                pdf_dict_del(ctx, r->owner, PDF_NAME(K));
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

// gomupdf_redact_text rewrites the page's contents (and the form XObjects
// they draw) through MuPDF's sanitize filter, stripping declared text from
// inline marked-content properties, and removes the text under the page's
// redaction annotations glyph by glyph. Returns 1 if it removed the text, 0
// if the page has more characters under redactions than
// GOMUPDF_MAX_REDACT_ORIGINS and needs MuPDF's own text removal (the
// declared text is stripped either way). Throws.
// rs holds the page's regions (gomupdf_collect_regions); this fills rs->o.
// The caller frees both.
static int gomupdf_redact_text(fz_context *ctx, pdf_document *pdf, pdf_page *page, gomupdf_regions *rs) {
    int per_glyph = 0;
    fz_var(per_glyph);
    fz_try(ctx) {
        pdf_sanitize_filter_options sopts;
        pdf_filter_factory filters[3];
        pdf_filter_options fopts;
        per_glyph = gomupdf_collect_origins(ctx, page, rs) == 0;
        memset(&sopts, 0, sizeof sopts);
        memset(filters, 0, sizeof filters);
        memset(&fopts, 0, sizeof fopts);
        sopts.opaque = rs;
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
    fz_catch(ctx) {
        fz_rethrow(ctx);
    }
    return per_glyph;
}

// A moved character counts as part of a failed redaction when the same
// character was under a region before the passes and its origin then was
// within GOMUPDF_BAND_ALONG character heights (or advances, whichever is
// larger; at least 1pt) along the moved character's baseline and
// GOMUPDF_BAND_ACROSS across it: a redacted glyph that reappears on its own
// line, as when a wrong font pushes it past its region (corkami quine.pdf:
// two heights is about four average glyph widths). Half a height across
// keeps it to the redacted line: the next line, however tight (0.6pt apart
// on pdfjs issue9940.pdf), is a full line height away. Requiring the same
// character keeps out kept text that MuPDF's content filter shifts next to a
// redaction (pymupdf cms-etc-filled.pdf, whose footer moves by 1.95pt
// beside a redacted page number).
#define GOMUPDF_BAND_ACROSS 0.5
#define GOMUPDF_BAND_ALONG 2.0

// gomupdf_near_hit reports whether character c, now at origin o with quad q
// (PDF user space), was under a region before the passes within the band
// (rs->o, sorted by x).
static int gomupdf_near_hit(const gomupdf_regions *rs, fz_quad q, fz_point o, int c) {
    double ax = q.ur.x - q.ul.x, ay = q.ur.y - q.ul.y; // along the baseline
    double bx = q.ll.x - q.ul.x, by = q.ll.y - q.ul.y; // across it
    double la = hypot(ax, ay), lb = hypot(bx, by), h = fmax(fmax(la, lb), 1.0);
    double ux, uy, vx, vy, reach = (GOMUPDF_BAND_ALONG + GOMUPDF_BAND_ACROSS) * h;
    int lo = 0, hi = rs->no, i;
    if (!isfinite(h) || !isfinite(o.x) || !isfinite(o.y))
        return 1; // doubt counts as near
    if (la > 0) { ux = ax / la; uy = ay / la; }
    else if (lb > 0) { ux = -by / lb; uy = bx / lb; }
    else { ux = 1; uy = 0; }
    vx = -uy; vy = ux;
    while (lo < hi) { // first origin with x >= o.x - reach
        int mid = lo + (hi - lo) / 2;
        if (rs->o[mid].x < o.x - reach) lo = mid + 1;
        else hi = mid;
    }
    for (i = lo; i < rs->no && rs->o[i].x <= o.x + reach; i++) {
        double dx = rs->o[i].x - o.x, dy = rs->o[i].y - o.y;
        if (rs->o[i].c == c && fabs(dx * ux + dy * uy) <= GOMUPDF_BAND_ALONG * h &&
            fabs(dx * vx + dy * vy) <= GOMUPDF_BAND_ACROSS * h)
            return 1;
    }
    return 0;
}

// gomupdf_count_left extracts the page as gomupdf_collect_origins does and
// counts, in left[0], the redacted characters still under a region (by
// gomupdf_char_hits, and gomupdf_near_hit: the same character was under a
// region there before; any, when rs->nk < 0); in left[1], those not under
// one that were not on the page, at that origin, before the passes (rs->k;
// not checked when rs->nk < 0) and are a redacted character reappearing on
// its own line (gomupdf_near_hit); in left[2], kept characters that moved,
// elsewhere or under a box. left[1] catches
// text the passes rewrote at other positions: a form whose font MuPDF's
// filtered copy cannot find is measured, and then drawn, with a fallback
// font, so its glyphs move and the box and origin rules miss the one
// redacted. left[2] is layout MuPDF's filter shifted, not a failed
// redaction. Not counted: spaces, real or made up by extraction for gaps,
// and a character with no advance
// that shares its origin with a real neighbour (a ligature's trailing
// component or a zero-advance mark, which sits at the next glyph's origin;
// see gomupdf_collect_origins).
static void gomupdf_count_left(fz_context *ctx, pdf_page *page, const gomupdf_regions *rs, int *left) {
    fz_stext_page *text = NULL;
    fz_stext_options so;
    fz_matrix page_ctm, inv;
    fz_var(text);
    memset(&so, 0, sizeof so);
    so.flags = FZ_STEXT_IGNORE_ACTUALTEXT;
    fz_try(ctx) {
        fz_stext_block *b;
        pdf_page_transform(ctx, page, NULL, &page_ctm);
        inv = fz_invert_matrix(page_ctm);
        text = gomupdf_new_stext_page(ctx, (fz_page *)page, &so);
        for (b = text->first_block; b; b = b->next) {
            fz_stext_line *l;
            if (b->type != FZ_STEXT_BLOCK_TEXT)
                continue;
            for (l = b->u.t.first_line; l; l = l->next) {
                fz_stext_char *ch, *prev = NULL;
                for (ch = l->first_char; ch; ch = ch->next) {
                    fz_stext_char *next = ch->next, *last = prev;
                    if (ch->flags & FZ_STEXT_SYNTHETIC)
                        continue;
                    prev = ch;
                    while (next && (next->flags & FZ_STEXT_SYNTHETIC))
                        next = next->next;
                    if (gomupdf_zero_advance(ch) &&
                        (gomupdf_shares_origin(ch, next) || gomupdf_shares_origin(ch, last)))
                        continue;
                    // A space says nothing, and real spaces move with the
                    // words around them (pdfjs issue9940.pdf).
                    if (ch->c == 0x20 || ch->c == 0xA0)
                        continue;
                    {
                        fz_quad q = fz_transform_quad(ch->quad, inv);
                        fz_point o = gomupdf_char_origin(ch, l, inv);
                        if (gomupdf_char_hits(rs, q))
                            // Kept text MuPDF's rewriting shifted under a
                            // box is no failed redaction (pymupdf
                            // cms-etc-filled.pdf: a footer's "0" moves
                            // 1.95pt into a redacted word's box).
                            left[rs->nk < 0 || gomupdf_near_hit(rs, q, o, ch->c) ? 0 : 2]++;
                        else if (rs->nk >= 0 && !gomupdf_kept(rs, o, ch->c))
                            left[gomupdf_near_hit(rs, q, o, ch->c) ? 1 : 2]++;
                    }
                }
            }
        }
    }
    fz_always(ctx) {
        fz_drop_stext_page(ctx, text);
    }
    fz_catch(ctx) {
        fz_rethrow(ctx);
    }
}

// gomupdf_apply_redactions applies every redaction annotation on the page.
// image_method, line_art and text carry the Go-side enum values (see
// ApplyOptions); each is mapped to its PDF_REDACT_* constant by name, and an
// unknown value is an error. black_boxes is MuPDF's flag (non-zero = draw).
// check receives gomupdf_count_left's three counts (all 0 unless text was
// removed); the caller decides what they mean.
static int gomupdf_apply_redactions(fz_context *ctx, fz_document *doc, int pageno,
                                    int black_boxes, int image_method,
                                    int line_art, int text,
                                    int *check, char *err, int errlen) {
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
    gomupdf_plan plan;
    gomupdf_regions rs;
    memset(&plan, 0, sizeof plan);
    memset(&rs, 0, sizeof rs);
    fz_var(page);
    fz_var(regions);
    fz_var(plan);
    fz_var(rs);
    check[0] = check[1] = check[2] = 0;
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
            // Read and check everything the structure and forms involve
            // before changing anything: a page past a limit is refused
            // untouched.
            int undeclare = opts.text == PDF_REDACT_TEXT_REMOVE;
            gomupdf_plan_page(ctx, pdf, page, &plan, undeclare);
            // Our pass removes the text glyph by glyph and pdf_redact_page
            // does the rest (boxes, images, line art, links, FreeText,
            // deleting the annotations) without its own text removal. Past
            // the character limit, MuPDF's text removal stays on. Declared
            // text goes from every page whose text we redact. Every mode
            // replaces the page's forms with redacted copies, so the page's
            // structure references follow them.
            if (undeclare && gomupdf_collect_regions(ctx, page, &rs) < 0)
                fz_throw(ctx, FZ_ERROR_LIMIT, "too many redaction regions");
            gomupdf_tag_forms(ctx, &plan);
            if (undeclare && gomupdf_redact_text(ctx, pdf, page, &rs))
                opts.text = PDF_REDACT_TEXT_NONE;
            pdf_redact_page(ctx, pdf, page, &opts);
            gomupdf_finish_page(ctx, pdf, page, &plan, undeclare);
            // Text the passes cannot reach (in a tiling pattern, a soft
            // mask, or a form drawn with a font MuPDF's copy of it cannot
            // find) is still extracted: check, as extraction sees it.
            if (undeclare)
                gomupdf_count_left(ctx, page, &rs, check);
        }
    }
    fz_always(ctx) {
        gomupdf_untag_forms(ctx, pdf, &plan);
        gomupdf_plan_drop(ctx, &plan);
        fz_free(ctx, rs.r);
        fz_free(ctx, rs.o);
        fz_free(ctx, rs.k);
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
	if err := d.stabilise(); err != nil {
		return err
	}
	repaired := d.repaired()
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_add_redaction(d.ctx, d.doc, C.int(pageNo),
		C.float(rect[0]), C.float(rect[1]), C.float(rect[2]), C.float(rect[3]),
		errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: add redaction: " + errText(errBuf))
	}
	return d.lateRepair("add redaction", repaired)
}

// lateRepair is the backstop for a repair that stabilise did not make happen
// first: a repair during the call (repaired is the state before it) rebuilt
// the xref from the file and dropped the document's changes.
func (d *mupdfDoc) lateRepair(op string, repaired bool) error {
	if !repaired && d.repaired() {
		return errors.New("gomupdf: " + op + ": MuPDF repaired the file's cross-reference table during the call, " +
			"which discards the changes made to the document; the page must not be treated as redacted")
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
	if err := d.stabilise(); err != nil {
		return err
	}
	repaired := d.repaired()
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_add_redaction_quads(d.ctx, d.doc, C.int(pageNo),
		(*C.float)(unsafe.Pointer(&quads[0])), C.int(len(quads)/8),
		errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: add redaction quads: " + errText(errBuf))
	}
	return d.lateRepair("add redaction quads", repaired)
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
	if err := d.stabilise(); err != nil {
		return 0, err
	}
	repaired := d.repaired()
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	var check [3]C.int
	n := C.gomupdf_apply_redactions(d.ctx, d.doc, C.int(pageNo), boxes,
		C.int(opts.Images), C.int(opts.LineArt), C.int(opts.Text), &check[0], errBuf, errBufLen)
	if n < 0 {
		return 0, errors.New("gomupdf: apply redactions: " + errText(errBuf))
	}
	if err := d.lateRepair("apply redactions", repaired); err != nil {
		return 0, err
	}
	if check[0] > 0 || check[1] > 0 {
		return 0, &RedactionCheckError{Page: pageNo, Remaining: int(check[0]), Moved: int(check[1]),
			MovedElsewhere: int(check[2])}
	}
	return int(n), nil
}
