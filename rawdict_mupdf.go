//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include "gomupdf_bigstack.h"
#include "gomupdf_stext.h"
#include <stdlib.h>
#include <string.h>
#include <stdio.h>

// Floats are printed with %.9g so that every float32 MuPDF holds round-trips.
// fz_write_printf is not libc printf: its %g already prints the shortest
// string that round-trips the float and ignores the precision, so %.9g and %g
// print the same bytes on MuPDF 1.25.6. The .9 states the requirement.

static void gomupdf_rawdict_block(fz_context *ctx, fz_output *out, fz_stext_block *block) {
    fz_write_printf(ctx, out, "B %.9g %.9g %.9g %.9g\n",
        block->bbox.x0, block->bbox.y0, block->bbox.x1, block->bbox.y1);
}

static void gomupdf_rawdict_line(fz_context *ctx, fz_output *out, fz_stext_line *line) {
    fz_write_printf(ctx, out, "L %.9g %.9g %.9g %.9g %.9g %.9g %d\n",
        line->bbox.x0, line->bbox.y0, line->bbox.x1, line->bbox.y1,
        line->dir.x, line->dir.y, line->wmode);
}

// gomupdf_rawdict walks the stext tree emitting one record per line, fields
// separated by single spaces:
//   B x0 y0 x1 y1                      – block start (text blocks only)
//   L x0 y0 x1 y1 dirx diry wmode      – line start
//   C rune ox oy x0 y0 x1 y1 ulx uly urx ury llx lly lrx lry size fontname
//                                      – character: bbox, then quad, then size;
//                                        fontname has no spaces and is "-"
//                                        when the font has no name
// flags are fz_stext_options flags. With clip_enabled, only characters whose
// bbox intersects the clip rect are written (as gomupdf_page_text_opts does),
// and a block or line is written only if one of its characters is.
static char *gomupdf_rawdict_run(fz_context *ctx, fz_document *doc, int pageno,
                             int flags, int clip_enabled,
                             float cx0, float cy0, float cx1, float cy1,
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
        memset(&opts, 0, sizeof(opts));
        opts.flags = flags;
        stext = gomupdf_new_stext_page(ctx, page, &opts);
        buf = fz_new_buffer(ctx, 16384);
        out = fz_new_output_with_buffer(ctx, buf);
        fz_rect clip = fz_make_rect(cx0, cy0, cx1, cy1);

        for (fz_stext_block *block = stext->first_block; block; block = block->next) {
            if (block->type != FZ_STEXT_BLOCK_TEXT) continue;
            // Without a clip every block and line is written, empty or not.
            int need_b = 1;
            if (!clip_enabled) { gomupdf_rawdict_block(ctx, out, block); need_b = 0; }
            for (fz_stext_line *line = block->u.t.first_line; line; line = line->next) {
                int need_l = 1;
                if (!clip_enabled) { gomupdf_rawdict_line(ctx, out, line); need_l = 0; }
                for (fz_stext_char *ch = line->first_char; ch; ch = ch->next) {
                    fz_rect r = fz_rect_from_quad(ch->quad);
                    if (clip_enabled && !fz_is_valid_rect(fz_intersect_rect(r, clip)))
                        continue;
                    if (need_b) { gomupdf_rawdict_block(ctx, out, block); need_b = 0; }
                    if (need_l) { gomupdf_rawdict_line(ctx, out, line); need_l = 0; }
                    const char *fname = "";
                    if (ch->font)
                        fname = fz_font_name(ctx, ch->font);
                    fz_quad q = ch->quad;
                    fz_write_printf(ctx, out, "C %d %.9g %.9g %.9g %.9g %.9g %.9g "
                        "%.9g %.9g %.9g %.9g %.9g %.9g %.9g %.9g %.9g ",
                        ch->c,
                        ch->origin.x, ch->origin.y,
                        r.x0, r.y0, r.x1, r.y1,
                        q.ul.x, q.ul.y, q.ur.x, q.ur.y, q.ll.x, q.ll.y, q.lr.x, q.lr.y,
                        ch->size);
                    // An unnamed font is "-", so the field count stays fixed.
                    if (!*fname)
                        fz_write_byte(ctx, out, '-');
                    for (const char *p = fname; *p; p++) {
                        char c2 = (*p == ' ' || *p == '\t' || *p == '\n') ? '_' : *p;
                        fz_write_byte(ctx, out, (unsigned char)c2);
                    }
                    fz_write_byte(ctx, out, '\n');
                }
            }
        }

        fz_close_output(ctx, out);
        unsigned char *data = NULL;
        size_t n = fz_buffer_storage(ctx, buf, &data);
        result = (char *)malloc(n + 1);
        if (!result) fz_throw(ctx, FZ_ERROR_SYSTEM, "out of memory");
        memcpy(result, data, n); result[n] = '\0';
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

// gomupdf_rawdict runs gomupdf_rawdict_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    int flags;
    int clip_enabled;
    float cx0;
    float cy0;
    float cx1;
    float cy1;
    char *err;
    int errlen;
    char *ret;
} gomupdf_rawdict_args;

static void gomupdf_rawdict_thunk(void *p) {
    gomupdf_rawdict_args *a = p;
    a->ret = gomupdf_rawdict_run(a->ctx, a->doc, a->pageno, a->flags, a->clip_enabled, a->cx0, a->cy0, a->cx1, a->cy1, a->err, a->errlen);
}

static char *gomupdf_rawdict(fz_context *ctx, fz_document *doc, int pageno, int flags, int clip_enabled, float cx0, float cy0, float cx1, float cy1, char *err, int errlen) {
    gomupdf_rawdict_args a = {ctx, doc, pageno, flags, clip_enabled, cx0, cy0, cx1, cy1, err, errlen};
    gomupdf_call_bigstack(gomupdf_rawdict_thunk, &a);
    return a.ret;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func (d *mupdfDoc) rawDictRaw(pageNo int, opts TextOptions) (string, error) {
	if err := d.readable(); err != nil {
		return "", err
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	clipEnabled, cx0, cy0, cx1, cy1 := stextClip(opts)
	cstr := C.gomupdf_rawdict(d.ctx, d.doc, C.int(pageNo),
		stextFlags(opts), clipEnabled, cx0, cy0, cx1, cy1,
		errBuf, errBufLen)
	if cstr == nil {
		return "", errors.New("gomupdf: rawdict: " + errText(errBuf))
	}
	defer C.free(unsafe.Pointer(cstr))
	return C.GoString(cstr), nil
}
