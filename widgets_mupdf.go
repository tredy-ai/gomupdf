//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include "gomupdf_bigstack.h"
#include <mupdf/pdf.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

static char *gomupdf_list_widgets_run(fz_context *ctx, fz_document *doc, int pageno,
                                  char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return NULL; }
    pdf_page *page = NULL;
    char *result = NULL;
    fz_var(page);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        size_t cap = 4096;
        result = (char *)malloc(cap);
        if (!result) fz_throw(ctx, FZ_ERROR_GENERIC, "out of memory");
        result[0] = '\0';
        size_t used = 0;

        pdf_annot *w;
        for (w = pdf_first_widget(ctx, page); w; w = pdf_next_widget(ctx, w)) {
            enum pdf_widget_type wt = pdf_widget_type(ctx, w);
            const char *tname;
            switch (wt) {
                case PDF_WIDGET_TYPE_TEXT:        tname = "text"; break;
                case PDF_WIDGET_TYPE_CHECKBOX:    tname = "checkbox"; break;
                case PDF_WIDGET_TYPE_RADIOBUTTON: tname = "radiobutton"; break;
                case PDF_WIDGET_TYPE_LISTBOX:     tname = "listbox"; break;
                case PDF_WIDGET_TYPE_COMBOBOX:    tname = "combobox"; break;
                case PDF_WIDGET_TYPE_SIGNATURE:   tname = "signature"; break;
                case PDF_WIDGET_TYPE_BUTTON:      tname = "button"; break;
                default:                          tname = "unknown"; break;
            }

            fz_rect r = pdf_bound_widget(ctx, w);

            pdf_obj *fobj = pdf_annot_obj(ctx, w);
            const char *val = pdf_field_value(ctx, fobj);
            if (!val) val = "";

            char *name = pdf_load_field_name(ctx, fobj);

            char safe_name[512];
            if (name) {
                size_t ni = 0;
                for (size_t j = 0; name[j] && ni < sizeof(safe_name)-1; j++) {
                    char c = name[j];
                    safe_name[ni++] = (c == '\t' || c == '\n' || c == '\r') ? ' ' : c;
                }
                safe_name[ni] = '\0';
                fz_free(ctx, name);
            } else {
                safe_name[0] = '\0';
            }
            char safe_val[512];
            {
                size_t vi = 0;
                for (size_t j = 0; val[j] && vi < sizeof(safe_val)-1; j++) {
                    char c = val[j];
                    safe_val[vi++] = (c == '\t' || c == '\n' || c == '\r') ? ' ' : c;
                }
                safe_val[vi] = '\0';
            }

            char line[2048];
            int n = snprintf(line, sizeof(line), "%s\t%g\t%g\t%g\t%g\t%s\t%s\n",
                             tname, r.x0, r.y0, r.x1, r.y1, safe_val, safe_name);
            if (n < 0) n = 0;
            if (used + (size_t)n + 1 > cap) {
                cap = cap * 2 + (size_t)n + 1;
                char *tmp = (char *)realloc(result, cap);
                if (!tmp) fz_throw(ctx, FZ_ERROR_GENERIC, "out of memory");
                result = tmp;
            }
            memcpy(result + used, line, (size_t)n);
            used += (size_t)n;
            result[used] = '\0';
        }
    }
    fz_always(ctx) {
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        if (result) { free(result); result = NULL; }
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return NULL;
    }
    return result;
}

static int gomupdf_set_text_field_run(fz_context *ctx, fz_document *doc, int pageno,
                                  int index, const char *value, char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    fz_var(page);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        pdf_annot *w = pdf_first_widget(ctx, page);
        int i = 0;
        while (w && i < index) {
            w = pdf_next_widget(ctx, w);
            i++;
        }
        if (!w) fz_throw(ctx, FZ_ERROR_GENERIC, "widget index out of range");
        if (pdf_widget_type(ctx, w) != PDF_WIDGET_TYPE_TEXT)
            fz_throw(ctx, FZ_ERROR_GENERIC, "widget is not a text field");
        pdf_set_text_field_value(ctx, w, value);
    }
    fz_always(ctx) {
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

static int gomupdf_set_checkbox_run(fz_context *ctx, fz_document *doc, int pageno,
                                int index, int checked, char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    fz_var(page);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);
        pdf_annot *w = pdf_first_widget(ctx, page);
        int i = 0;
        while (w && i < index) {
            w = pdf_next_widget(ctx, w);
            i++;
        }
        if (!w) fz_throw(ctx, FZ_ERROR_GENERIC, "widget index out of range");
        if (pdf_widget_type(ctx, w) != PDF_WIDGET_TYPE_CHECKBOX)
            fz_throw(ctx, FZ_ERROR_GENERIC, "widget is not a checkbox");

        pdf_obj *fobj = pdf_annot_obj(ctx, w);
        const char *cur = pdf_field_value(ctx, fobj);
        int is_checked = (cur && strcmp(cur, "Off") != 0 && strcmp(cur, "") != 0);
        if ((checked && !is_checked) || (!checked && is_checked)) {
            pdf_toggle_widget(ctx, w);
        }
    }
    fz_always(ctx) {
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

static int gomupdf_add_text_field_run(fz_context *ctx, fz_document *doc, int pageno,
                                  const char *name, float x0, float y0, float x1, float y1,
                                  const char *value, char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    pdf_page *page = NULL;
    pdf_annot *widget = NULL;
    fz_var(page);
    fz_var(widget);
    fz_try(ctx) {
        page = pdf_load_page(ctx, pdf, pageno);

        pdf_obj *trailer = pdf_trailer(ctx, pdf);
        pdf_obj *root = pdf_dict_get(ctx, trailer, PDF_NAME(Root));
        pdf_obj *acro = pdf_dict_get(ctx, root, PDF_NAME(AcroForm));
        if (!acro || pdf_is_null(ctx, acro)) {
            acro = pdf_dict_put_dict(ctx, root, PDF_NAME(AcroForm), 2);
        }
        pdf_obj *fields = pdf_dict_get(ctx, acro, PDF_NAME(Fields));
        if (!fields || !pdf_is_array(ctx, fields)) {
            fields = pdf_new_array(ctx, pdf, 4);
            pdf_dict_put_drop(ctx, acro, PDF_NAME(Fields), fields);
            fields = pdf_dict_get(ctx, acro, PDF_NAME(Fields));
        }

        if (!pdf_dict_get(ctx, acro, PDF_NAME(DA))) {
            pdf_dict_put_text_string(ctx, acro, PDF_NAME(DA), "/Helv 0 Tf 0 g");
        }

        widget = pdf_create_annot(ctx, page, PDF_ANNOT_WIDGET);
        pdf_obj *wobj = pdf_annot_obj(ctx, widget);

        pdf_dict_put(ctx, wobj, PDF_NAME(FT), PDF_NAME(Tx));
        pdf_dict_put_text_string(ctx, wobj, PDF_NAME(T), name);
        pdf_dict_put_text_string(ctx, wobj, PDF_NAME(DA), "/Helv 0 Tf 0 g");

        fz_rect rect = { x0, y0, x1, y1 };
        pdf_set_annot_rect(ctx, widget, rect);

        pdf_array_push(ctx, fields, wobj);

        pdf_set_text_field_value(ctx, widget, value);
    }
    fz_always(ctx) {
        if (widget) pdf_drop_annot(ctx, widget);
        fz_drop_page(ctx, (fz_page *)page);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

// gomupdf_add_text_field runs gomupdf_add_text_field_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    const char *name;
    float x0;
    float y0;
    float x1;
    float y1;
    const char *value;
    char *err;
    int errlen;
    int ret;
} gomupdf_add_text_field_args;

static void gomupdf_add_text_field_thunk(void *p) {
    gomupdf_add_text_field_args *a = p;
    a->ret = gomupdf_add_text_field_run(a->ctx, a->doc, a->pageno, a->name, a->x0, a->y0, a->x1, a->y1, a->value, a->err, a->errlen);
}

static int gomupdf_add_text_field(fz_context *ctx, fz_document *doc, int pageno, const char *name, float x0, float y0, float x1, float y1, const char *value, char *err, int errlen) {
    gomupdf_add_text_field_args a = {ctx, doc, pageno, name, x0, y0, x1, y1, value, err, errlen};
    gomupdf_call_bigstack(gomupdf_add_text_field_thunk, &a);
    return a.ret;
}

// gomupdf_list_widgets runs gomupdf_list_widgets_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    char *err;
    int errlen;
    char *ret;
} gomupdf_list_widgets_args;

static void gomupdf_list_widgets_thunk(void *p) {
    gomupdf_list_widgets_args *a = p;
    a->ret = gomupdf_list_widgets_run(a->ctx, a->doc, a->pageno, a->err, a->errlen);
}

static char *gomupdf_list_widgets(fz_context *ctx, fz_document *doc, int pageno, char *err, int errlen) {
    gomupdf_list_widgets_args a = {ctx, doc, pageno, err, errlen};
    gomupdf_call_bigstack(gomupdf_list_widgets_thunk, &a);
    return a.ret;
}

// gomupdf_set_checkbox runs gomupdf_set_checkbox_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    int index;
    int checked;
    char *err;
    int errlen;
    int ret;
} gomupdf_set_checkbox_args;

static void gomupdf_set_checkbox_thunk(void *p) {
    gomupdf_set_checkbox_args *a = p;
    a->ret = gomupdf_set_checkbox_run(a->ctx, a->doc, a->pageno, a->index, a->checked, a->err, a->errlen);
}

static int gomupdf_set_checkbox(fz_context *ctx, fz_document *doc, int pageno, int index, int checked, char *err, int errlen) {
    gomupdf_set_checkbox_args a = {ctx, doc, pageno, index, checked, err, errlen};
    gomupdf_call_bigstack(gomupdf_set_checkbox_thunk, &a);
    return a.ret;
}

// gomupdf_set_text_field runs gomupdf_set_text_field_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    int pageno;
    int index;
    const char *value;
    char *err;
    int errlen;
    int ret;
} gomupdf_set_text_field_args;

static void gomupdf_set_text_field_thunk(void *p) {
    gomupdf_set_text_field_args *a = p;
    a->ret = gomupdf_set_text_field_run(a->ctx, a->doc, a->pageno, a->index, a->value, a->err, a->errlen);
}

static int gomupdf_set_text_field(fz_context *ctx, fz_document *doc, int pageno, int index, const char *value, char *err, int errlen) {
    gomupdf_set_text_field_args a = {ctx, doc, pageno, index, value, err, errlen};
    gomupdf_call_bigstack(gomupdf_set_text_field_thunk, &a);
    return a.ret;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func (d *mupdfDoc) widgetsRaw(pageNo int) (string, error) {
	if err := d.readable(); err != nil {
		return "", err
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	cstr := C.gomupdf_list_widgets(d.ctx, d.doc, C.int(pageNo), errBuf, errBufLen)
	if cstr == nil {
		return "", errors.New("gomupdf: widgets: " + errText(errBuf))
	}
	defer C.free(unsafe.Pointer(cstr))
	return C.GoString(cstr), nil
}

func (d *mupdfDoc) setTextField(pageNo, index int, value string) error {
	if err := d.writable(); err != nil {
		return err
	}
	cvalue := C.CString(value)
	defer C.free(unsafe.Pointer(cvalue))
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_set_text_field(d.ctx, d.doc, C.int(pageNo), C.int(index), cvalue, errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: set text field: " + errText(errBuf))
	}
	return nil
}

func (d *mupdfDoc) setCheckbox(pageNo, index int, checked bool) error {
	if err := d.writable(); err != nil {
		return err
	}
	var cChecked C.int
	if checked {
		cChecked = 1
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_set_checkbox(d.ctx, d.doc, C.int(pageNo), C.int(index), cChecked, errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: set checkbox: " + errText(errBuf))
	}
	return nil
}

func (d *mupdfDoc) addTextField(pageNo int, name string, rect [4]float64, value string) error {
	if err := d.writable(); err != nil {
		return err
	}
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	cvalue := C.CString(value)
	defer C.free(unsafe.Pointer(cvalue))
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_add_text_field(d.ctx, d.doc, C.int(pageNo),
		cname,
		C.float(rect[0]), C.float(rect[1]), C.float(rect[2]), C.float(rect[3]),
		cvalue, errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: add text field: " + errText(errBuf))
	}
	return nil
}
