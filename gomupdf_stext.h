#ifndef GOMUPDF_STEXT_H
#define GOMUPDF_STEXT_H

#include <mupdf/fitz.h>
#include "gomupdf_bigstack.h"

/*
 * gomupdf_new_stext_page is fz_new_stext_page_from_page with one difference:
 * when opts->flags has FZ_STEXT_IGNORE_ACTUALTEXT, the stext device's metatext
 * callbacks are dropped.
 *
 * Why: on MuPDF 1.26.11 that flag keeps the drawn glyphs but still appends the
 * declared /ActualText as extra zero-width characters, so the declared text
 * leaks into the output. The /ActualText replacement is delivered to the device
 * only as begin/end metatext, so a device without those callbacks never sees
 * it. The stext device uses metatext for nothing else we extract.
 *
 * Like the MuPDF function it replaces, it may throw; call it inside fz_try.
 */
static inline fz_stext_page *
gomupdf_new_stext_page(fz_context *ctx, fz_page *page, const fz_stext_options *opts)
{
    fz_stext_page *text = NULL;
    fz_device *dev = NULL;
    fz_var(text);
    fz_var(dev);
    fz_try(ctx) {
        text = fz_new_stext_page(ctx, fz_bound_page(ctx, page));
        dev = fz_new_stext_device(ctx, text, opts);
        if (opts && (opts->flags & FZ_STEXT_IGNORE_ACTUALTEXT)) {
            dev->begin_metatext = NULL;
            dev->end_metatext = NULL;
        }
        gomupdf_run_page_big(ctx, page, dev, 1); // on a large stack: content nesting recurses
        fz_close_device(ctx, dev);
    }
    fz_always(ctx) {
        if (dev)
            fz_drop_device(ctx, dev);
    }
    fz_catch(ctx) {
        fz_drop_stext_page(ctx, text);
        fz_rethrow(ctx);
    }
    return text;
}

#endif
