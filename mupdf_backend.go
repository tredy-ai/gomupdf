//go:build !nomupdf

package gomupdf

// MuPDF backend (cgo). This file holds the core lifecycle helpers and the
// mupdfDoc/mupdfFont/mupdfDriver types implementing the package SPI in
// backend.go. The C helpers that were historically scattered across the
// per-operation files now live in matching <name>_mupdf.go files as methods on
// *mupdfDoc / *mupdfFont. cgo dedups C types across same-package files, so
// d.ctx (a *C.fz_context) created here is usable by C functions declared in any
// tagged file's preamble.

/*

#include <mupdf/fitz.h>
#include "gomupdf_bigstack.h"
#include <mupdf/pdf.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

#if FZ_VERSION_MAJOR < 1 || (FZ_VERSION_MAJOR == 1 && FZ_VERSION_MINOR < 26)
#error "gomupdf (tredy-ai) requires MuPDF >= 1.26"
#endif

// Create a context with document handlers registered. Returns NULL on failure.
static fz_context *gomupdf_new_context(void) {
    fz_context *ctx = fz_new_context(NULL, NULL, FZ_STORE_DEFAULT);
    if (!ctx) return NULL;
    fz_set_error_callback(ctx, gomupdf_error_print, NULL); // counts swallowed overflows (gomupdf_bigstack.h)
    fz_try(ctx) {
        fz_register_document_handlers(ctx);
    }
    fz_catch(ctx) {
        fz_drop_context(ctx);
        return NULL;
    }
    return ctx;
}

// Open a document from a memory buffer using an explicit magic/filetype hint
// (e.g. "png", ".pdf", "application/epub+zip"). The buffer must outlive the
// returned doc. On error returns NULL and writes the MuPDF message into err.
static fz_document *gomupdf_open_magic_run(fz_context *ctx, const unsigned char *data,
                                       size_t len, const char *magic,
                                       char *err, int errlen) {
    fz_document *doc = NULL;
    fz_stream *stream = NULL;
    fz_var(stream);
    fz_var(doc);
    fz_try(ctx) {
        stream = fz_open_memory(ctx, data, len);
        doc = fz_open_document_with_stream(ctx, magic, stream);
    }
    fz_always(ctx) {
        fz_drop_stream(ctx, stream);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return NULL;
    }
    return doc;
}

// Open a document from a file path; MuPDF reads the file lazily.
static fz_document *gomupdf_open_path_run(fz_context *ctx, const char *path,
                                      char *err, int errlen) {
    fz_document *doc = NULL;
    fz_var(doc);
    fz_try(ctx) {
        doc = fz_open_document(ctx, path);
        // Open is PDF-only (like OpenStream); text, HTML and the rest go through OpenAny.
        if (!pdf_specifics(ctx, doc))
            fz_throw(ctx, FZ_ERROR_GENERIC, "not a PDF document");
    }
    fz_catch(ctx) {
        fz_drop_document(ctx, doc);
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return NULL;
    }
    return doc;
}

static int gomupdf_needs_password(fz_context *ctx, fz_document *doc) {
    int r = 0;
    fz_try(ctx) { r = fz_needs_password(ctx, doc); }
    fz_catch(ctx) { return 0; }
    return r;
}

// Returns 1 on success (or if no password needed), 0 on wrong password.
static int gomupdf_authenticate_run(fz_context *ctx, fz_document *doc, const char *pw) {
    int r = 0;
    fz_try(ctx) { r = fz_authenticate_password(ctx, doc, pw); }
    fz_catch(ctx) { return 0; }
    return r;
}

static int gomupdf_count_pages(fz_context *ctx, fz_document *doc, char *err, int errlen) {
    int n = -1;
    fz_try(ctx) { n = fz_count_pages(ctx, doc); }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return n;
}

// Create a new empty PDF, returned as an fz_document.
static fz_document *gomupdf_new_pdf(fz_context *ctx, char *err, int errlen) {
    pdf_document *pdf = NULL;
    fz_try(ctx) { pdf = pdf_create_document(ctx); }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return NULL;
    }
    return (fz_document *)pdf;
}

// Load a base-14 PDF font by name and return both the fz_font and the context
// that owns it. On error, writes to err and returns NULL (ctx is dropped).
static fz_font *gomupdf_new_base14(const char *name, fz_context **out_ctx,
                                    char *err, int errlen) {
    fz_context *ctx = fz_new_context(NULL, NULL, FZ_STORE_DEFAULT);
    if (!ctx) {
        snprintf(err, errlen, "gomupdf: failed to create context");
        return NULL;
    }
    fz_font *font = NULL;
    fz_try(ctx) {
        font = fz_new_base14_font(ctx, name);
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        fz_drop_context(ctx);
        return NULL;
    }
    if (!font) {
        snprintf(err, errlen, "gomupdf: unknown base-14 font");
        fz_drop_context(ctx);
        return NULL;
    }
    *out_ctx = ctx;
    return font;
}

// Return the horizontal advance of a single unicode codepoint at 1 em
// (i.e. in em units). Returns 0 if the glyph is not found.
static float gomupdf_glyph_advance(fz_context *ctx, fz_font *font, int codepoint) {
    int gid = fz_encode_character(ctx, font, codepoint);
    if (gid <= 0) return 0.0f;
    return fz_advance_glyph(ctx, font, gid, 0);
}

// Fill *asc and *desc with the font's ascender and descender in em units.
static void gomupdf_font_vmetrics(fz_context *ctx, fz_font *font,
                                   float *asc, float *desc) {
    *asc  = fz_font_ascender(ctx, font);
    *desc = fz_font_descender(ctx, font);
}

// Return the font name (points into font internals; no need to free).
static const char *gomupdf_font_name_str(fz_context *ctx, fz_font *font) {
    return fz_font_name(ctx, font);
}

// gomupdf_authenticate runs gomupdf_authenticate_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    fz_document *doc;
    const char *pw;
    int ret;
} gomupdf_authenticate_args;

static void gomupdf_authenticate_thunk(void *p) {
    gomupdf_authenticate_args *a = p;
    a->ret = gomupdf_authenticate_run(a->ctx, a->doc, a->pw);
}

static int gomupdf_authenticate(fz_context *ctx, fz_document *doc, const char *pw) {
    gomupdf_authenticate_args a = {ctx, doc, pw};
    gomupdf_call_bigstack(gomupdf_authenticate_thunk, &a);
    return a.ret;
}

// gomupdf_open_magic runs gomupdf_open_magic_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    const unsigned char *data;
    size_t len;
    const char *magic;
    char *err;
    int errlen;
    fz_document *ret;
} gomupdf_open_magic_args;

static void gomupdf_open_magic_thunk(void *p) {
    gomupdf_open_magic_args *a = p;
    a->ret = gomupdf_open_magic_run(a->ctx, a->data, a->len, a->magic, a->err, a->errlen);
}

static fz_document *gomupdf_open_magic(fz_context *ctx, const unsigned char *data, size_t len, const char *magic, char *err, int errlen) {
    gomupdf_open_magic_args a = {ctx, data, len, magic, err, errlen};
    gomupdf_call_bigstack(gomupdf_open_magic_thunk, &a);
    return a.ret;
}

// gomupdf_open_path runs gomupdf_open_path_run on a large stack (gomupdf_bigstack.h).
typedef struct {
    fz_context *ctx;
    const char *path;
    char *err;
    int errlen;
    fz_document *ret;
} gomupdf_open_path_args;

static void gomupdf_open_path_thunk(void *p) {
    gomupdf_open_path_args *a = p;
    a->ret = gomupdf_open_path_run(a->ctx, a->path, a->err, a->errlen);
}

static fz_document *gomupdf_open_path(fz_context *ctx, const char *path, char *err, int errlen) {
    gomupdf_open_path_args a = {ctx, path, err, errlen};
    gomupdf_call_bigstack(gomupdf_open_path_thunk, &a);
    return a.ret;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

const errBufLen = 512

// newErrBuf allocates an empty error buffer for a C helper; the caller frees
// it. Empty, so a helper that fails without writing a message cannot leave
// stale bytes to be read as one.
func newErrBuf() *C.char {
	b := (*C.char)(C.malloc(errBufLen))
	*b = 0
	return b
}

// errText returns the message a C helper wrote to buf, or says that it
// wrote none: an error message is never empty.
func errText(buf *C.char) string {
	if s := C.GoString(buf); s != "" {
		return s
	}
	return "unknown error (MuPDF gave no message)"
}

func init() {
	defaultDriver = mupdfDriver{}
}

// mupdfDriver is the MuPDF implementation of the driver SPI. lazy (tests
// only) skips loading every object at open, so that a repair can still
// happen later.
type mupdfDriver struct{ lazy bool }

func (mupdfDriver) name() string { return "mupdf" }

func (drv mupdfDriver) open(data []byte, magic string) (docBackend, bool, error) {
	if magic == "" {
		magic = ".pdf"
	}
	ctx := C.gomupdf_new_context()
	if ctx == nil {
		return nil, false, errors.New("gomupdf: failed to create context")
	}
	cdata := C.CBytes(data) // malloc'd copy; freed in close
	cmagic := C.CString(magic)
	defer C.free(unsafe.Pointer(cmagic))
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))

	doc := C.gomupdf_open_magic(ctx, (*C.uchar)(cdata), C.size_t(len(data)), cmagic, errBuf, errBufLen)
	if doc == nil {
		C.free(cdata)
		C.fz_drop_context(ctx)
		return nil, false, errors.New("gomupdf: open failed: " + errText(errBuf))
	}
	needsPass := C.gomupdf_needs_password(ctx, doc) != 0
	return drv.opened(&mupdfDoc{ctx: ctx, doc: doc, data: cdata, dataLen: len(data)}, needsPass)
}

func (drv mupdfDriver) openPath(path string) (docBackend, bool, error) {
	ctx := C.gomupdf_new_context()
	if ctx == nil {
		return nil, false, errors.New("gomupdf: failed to create context")
	}
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	doc := C.gomupdf_open_path(ctx, cpath, errBuf, errBufLen)
	if doc == nil {
		C.fz_drop_context(ctx)
		return nil, false, errors.New("gomupdf: open failed: " + errText(errBuf))
	}
	needsPass := C.gomupdf_needs_password(ctx, doc) != 0
	return drv.opened(&mupdfDoc{ctx: ctx, doc: doc}, needsPass)
}

// opened stabilises a document that is not locked (see stabilise), so that
// page count, extraction and every change see the document MuPDF will
// save. A locked document is stabilised when authenticate succeeds: objects
// loaded before that would be decrypted with the wrong key.
func (drv mupdfDriver) opened(d *mupdfDoc, needsPass bool) (docBackend, bool, error) {
	d.locked = needsPass
	// Nothing has changed yet, so a repair this makes loses nothing.
	if !needsPass && !drv.lazy {
		if err := d.stabilise(); err != nil {
			d.close()
			return nil, false, err
		}
	}
	return d, needsPass, nil
}

func (mupdfDriver) newPDF() (docBackend, error) {
	ctx := C.gomupdf_new_context()
	if ctx == nil {
		return nil, errors.New("gomupdf: failed to create context")
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	doc := C.gomupdf_new_pdf(ctx, errBuf, errBufLen)
	if doc == nil {
		C.fz_drop_context(ctx)
		return nil, errors.New("gomupdf: new pdf: " + errText(errBuf))
	}
	return &mupdfDoc{ctx: ctx, doc: doc, stable: true}, nil
}

func (mupdfDriver) newFont(name string) (fontBackend, error) {
	cname := C.CString(name)
	defer C.free(unsafe.Pointer(cname))
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	var ctx *C.fz_context
	cfont := C.gomupdf_new_base14(cname, &ctx, errBuf, C.int(errBufLen))
	if cfont == nil {
		return nil, errors.New("gomupdf: NewFont: " + errText(errBuf))
	}
	return &mupdfFont{ctx: ctx, font: cfont}, nil
}

// mupdfDoc is one open MuPDF document. Methods are NOT internally synchronized;
// the public Document serializes them under its mutex.
type mupdfDoc struct {
	ctx     *C.fz_context
	doc     *C.fz_document
	data    unsafe.Pointer // C-malloc'd copy backing the in-memory stream
	dataLen int            // length of the original bytes at data (incremental save)
	stable  bool           // every object loaded once (see stabilise)
	locked  bool           // needs a password Authenticate has not given
}

// readable returns an error unless the document is open and not locked.
// Every method that loads document content starts with it: an object
// loaded before Authenticate is decrypted with the wrong key and stays
// cached that way (MuPDF cannot reload it), so even a read would leave
// garbage (a title, an outline entry) behind for after Authenticate.
func (d *mupdfDoc) readable() error { return d.writable() }

// writable returns an error unless the document is open and not locked.
// Every method that changes or saves the document starts with it.
func (d *mupdfDoc) writable() error {
	if d.doc == nil {
		return errors.New("gomupdf: document closed")
	}
	if d.locked {
		return errLocked
	}
	return nil
}

func (d *mupdfDoc) close() {
	if d.doc != nil {
		C.fz_drop_document(d.ctx, d.doc)
		d.doc = nil
	}
	if d.ctx != nil {
		C.fz_drop_context(d.ctx)
		d.ctx = nil
	}
	if d.data != nil {
		C.free(d.data)
		d.data = nil
	}
}

func (d *mupdfDoc) authenticate(password string) bool {
	if d.doc == nil {
		return false
	}
	cpw := C.CString(password)
	defer C.free(unsafe.Pointer(cpw))
	if C.gomupdf_authenticate(d.ctx, d.doc, cpw) == 0 {
		return false
	}
	// Load every object now that the key is right (see opened). If that
	// fails (only running out of memory can), the document stays locked.
	wasLocked := d.locked
	d.locked = false
	if err := d.stabilise(); err != nil {
		d.locked = wasLocked
		return false
	}
	return true
}

// pageCount is 0 while the document is locked: counting loads the catalog,
// and with it any string inline there (a page label prefix, say).
func (d *mupdfDoc) pageCount() int {
	if d.readable() != nil {
		return 0
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	n := C.gomupdf_count_pages(d.ctx, d.doc, errBuf, errBufLen)
	if n < 0 {
		return 0
	}
	return int(n)
}

// mupdfFont is a loaded base-14 font owning its own context.
type mupdfFont struct {
	ctx  *C.fz_context
	font *C.fz_font
}

func (f *mupdfFont) close() {
	if f.font != nil {
		C.fz_drop_font(f.ctx, f.font)
		f.font = nil
	}
	if f.ctx != nil {
		C.fz_drop_context(f.ctx)
		f.ctx = nil
	}
}

func (f *mupdfFont) fontName() string {
	if f.font == nil {
		return ""
	}
	return C.GoString(C.gomupdf_font_name_str(f.ctx, f.font))
}

func (f *mupdfFont) advance(r rune) float64 {
	if f.font == nil {
		return 0
	}
	return float64(C.gomupdf_glyph_advance(f.ctx, f.font, C.int(r)))
}

func (f *mupdfFont) ascender() float64 {
	if f.font == nil {
		return 0
	}
	var asc, desc C.float
	C.gomupdf_font_vmetrics(f.ctx, f.font, &asc, &desc)
	return float64(asc)
}

func (f *mupdfFont) descender() float64 {
	if f.font == nil {
		return 0
	}
	var asc, desc C.float
	C.gomupdf_font_vmetrics(f.ctx, f.font, &asc, &desc)
	return float64(desc)
}

// ensure interface satisfaction
var (
	_ driver      = mupdfDriver{}
	_ docBackend  = (*mupdfDoc)(nil)
	_ fontBackend = (*mupdfFont)(nil)
)
