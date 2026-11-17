//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include <mupdf/pdf.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

// gomupdf_layer_dict reports whether ref (an indirect object) is nothing but
// an optional-content group (/Type /OCG with no keys other than /Type, /Name,
// /Intent and /Usage) or membership dictionary (/Type /OCMD with no keys
// other than /Type, /OCGs, /P and /VE). The key check keeps ShowAllLayers
// from destroying an object that has another role as well.
static int gomupdf_layer_dict(fz_context *ctx, pdf_obj *ref) {
    static const char *ocg[] = {"Type", "Name", "Intent", "Usage", NULL};
    static const char *ocmd[] = {"Type", "OCGs", "P", "VE", NULL};
    if (!pdf_is_dict(ctx, ref) || pdf_is_stream(ctx, ref)) return 0;
    pdf_obj *t = pdf_dict_get(ctx, ref, PDF_NAME(Type));
    const char **keys = pdf_name_eq(ctx, t, PDF_NAME(OCG)) ? ocg : pdf_name_eq(ctx, t, PDF_NAME(OCMD)) ? ocmd : NULL;
    if (!keys) return 0;
    for (int i = 0; i < pdf_dict_len(ctx, ref); i++) {
        const char *k = pdf_to_name(ctx, pdf_dict_get_key(ctx, ref, i)), **a = keys;
        while (*a && strcmp(*a, k)) a++;
        if (!*a) return 0;
    }
    return 1;
}

// gomupdf_show_all_layers deletes the catalog /OCProperties and destroys
// every layer dictionary (gomupdf_layer_dict), then drops MuPDF's cached
// optional-content descriptor, so the next lookup reads the configuration
// again, finds none and treats every group as visible. pdf_drop_ocg frees
// the descriptor but leaves doc->ocg pointing at it (MuPDF 1.25.6 and
// 1.26.11 alike; MuPDF's own callers clear the pointer after it), and
// pdf_drop_document drops it again, so the pointer is cleared here, whether
// or not the edits succeeded. Every object is loaded first, as Scrub does,
// so an xref repair happens before the sweep.
static int gomupdf_show_all_layers(fz_context *ctx, fz_document *doc, char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    int failed = 0;
    fz_try(ctx) {
        for (int i = 1; i < pdf_xref_len(ctx, pdf); i++) {
            fz_try(ctx) pdf_cache_object(ctx, pdf, i);
            fz_catch(ctx) {
                fz_rethrow_if(ctx, FZ_ERROR_SYSTEM);
                fz_ignore_error(ctx);
            }
        }
        pdf_obj *root = pdf_dict_get(ctx, pdf_trailer(ctx, pdf), PDF_NAME(Root));
        if (pdf_dict_get(ctx, root, PDF_NAME(OCProperties))) pdf_dict_del(ctx, root, PDF_NAME(OCProperties));
        int n = pdf_xref_len(ctx, pdf);
        for (int i = 1; i < n; i++) {
            pdf_obj *ref = pdf_new_indirect(ctx, pdf, i, 0), *empty = NULL;
            fz_var(empty);
            fz_try(ctx) {
                if (gomupdf_layer_dict(ctx, ref)) {
                    pdf_delete_object(ctx, pdf, i);
                    // As in Scrub: one created since the file was opened (by
                    // InsertPDF, say) becomes an empty dictionary, or MuPDF
                    // 1.25 could not save the document.
                    if (pdf_get_xref_entry_no_null(ctx, pdf, i)->type == 0) {
                        empty = pdf_new_dict(ctx, pdf, 0);
                        pdf_update_object(ctx, pdf, i, empty);
                    }
                }
            }
            fz_always(ctx) {
                pdf_drop_obj(ctx, empty);
                pdf_drop_obj(ctx, ref);
            }
            fz_catch(ctx) fz_rethrow(ctx);
        }
    }
    fz_always(ctx) {
        pdf_drop_ocg(ctx, pdf);
        pdf->ocg = NULL;
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        failed = 1;
    }
    return failed ? -1 : 0;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func (d *mupdfDoc) showAllLayers() error {
	if d.doc == nil {
		return errors.New("gomupdf: document closed")
	}
	errBuf := (*C.char)(C.malloc(errBufLen))
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_show_all_layers(d.ctx, d.doc, errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: show all layers: " + C.GoString(errBuf))
	}
	return nil
}
