#ifndef GOMUPDF_BIGSTACK_H
#define GOMUPDF_BIGSTACK_H

#include <mupdf/fitz.h>
#include <mupdf/pdf.h>
#include <pthread.h>
#include <signal.h>
#include <stdio.h>
#include <string.h>

/*
 * Running deeply recursive MuPDF calls on a large stack.
 *
 * A cgo call runs on the calling thread's own stack. Threads the Go runtime
 * creates get the C library's default thread stack, which on musl (Alpine) is
 * 128 KiB, and MuPDF recurses once per indirect reference hop when it saves
 * (pdf-write.c markobj) and once per nesting level when it interprets
 * content: a Garbage 1 save of a few thousand sibling outline items (a /Next
 * chain) overflowed that stack and killed the process with SIGSEGV.
 *
 * gomupdf_call_bigstack runs fn(arg) on a new thread whose stack is
 * gomupdf_bigstack_size bytes (64 MiB of address space, committed only as
 * it is used), waits for it and returns 0. If the thread cannot be created
 * (an address-space limit, say) it does not run fn at all and returns the
 * error number: the caller's own stack is the one known to overflow, so the
 * wrapped call fails instead (gomupdf_bigstack_err). Already on such a
 * thread (gomupdf_on_bigstack) it runs fn(arg) there, so nested calls create
 * no further threads.
 * The calling thread does nothing meanwhile, so the fz_context fn uses is
 * never used by two threads at once; fn must not let a MuPDF exception
 * escape (setjmp and longjmp must not cross threads), which
 * gomupdf_bigstack arranges. The new thread blocks every signal (the Go
 * runtime's handlers run on its own threads; a fault still kills the
 * process), and touches no thread-local state of Go's. Memory fn uses stays
 * valid because the caller, and so the cgo call that passed it, waits.
 */
extern size_t gomupdf_bigstack_size;
extern __thread int gomupdf_on_bigstack;

/*
 * Exception-stack overflows MuPDF swallowed. MuPDF's fz_try nests at most
 * about 250 deep (the exception stack in fz_context); a recursion that goes
 * deeper fails with "exception stack overflow!", and pdf_resolve_indirect
 * (pdf-xref.c) catches that like any load error, reports it and returns
 * NULL, so the caller sees a null object and carries on: a graft of a
 * 20,000-long reference chain copied 124 links and succeeded.
 * gomupdf_error_print, every document context's error callback, counts each
 * report of it on the thread MuPDF runs on; a helper whose success must mean
 * a complete result zeroes gomupdf_overflows before its work and fails if it
 * is not zero after (gomupdf_check_overflows). The callback then prints the
 * message as MuPDF's default does.
 */
extern __thread int gomupdf_overflows;

static inline void gomupdf_error_print(void *user, const char *message) {
    if (strstr(message, "exception stack overflow")) gomupdf_overflows++;
    fz_default_error_callback(user, message);
}

static inline void gomupdf_check_overflows(fz_context *ctx) {
    if (gomupdf_overflows)
        fz_throw(ctx, FZ_ERROR_LIMIT, "exception stack overflow: the file nests deeper than MuPDF can follow, "
                                      "so the result would be incomplete");
}

typedef struct {
    void (*fn)(void *);
    void *arg;
} gomupdf_bigstack_job;

static inline void *gomupdf_bigstack_start(void *p) {
    gomupdf_bigstack_job *j = p;
    gomupdf_on_bigstack = 1;
    j->fn(j->arg);
    return NULL;
}

static inline int gomupdf_call_bigstack(void (*fn)(void *), void *arg) {
    gomupdf_bigstack_job job = {fn, arg};
    pthread_attr_t attr;
    pthread_t th;
    sigset_t all, old;
    if (gomupdf_on_bigstack) {
        fn(arg);
        return 0;
    }
    int e = pthread_attr_init(&attr);
    if (e) return e;
    e = pthread_attr_setstacksize(&attr, gomupdf_bigstack_size);
    if (!e) {
        // The new thread inherits this mask; the Go runtime creates its
        // own threads the same way.
        sigfillset(&all);
        pthread_sigmask(SIG_SETMASK, &all, &old);
        e = pthread_create(&th, &attr, gomupdf_bigstack_start, &job);
        pthread_sigmask(SIG_SETMASK, &old, NULL);
    }
    pthread_attr_destroy(&attr);
    if (!e) pthread_join(th, NULL);
    return e;
}

// gomupdf_bigstack_err writes why gomupdf_call_bigstack could not create
// its thread (error number e) into err, for a wrapper to fail with.
static inline void gomupdf_bigstack_err(int e, char *err, int errlen) {
    snprintf(err, errlen, "cannot create a thread with a large stack: %s", strerror(e));
}

typedef struct {
    fz_context *ctx;
    void (*fn)(fz_context *, void *);
    void *arg;
    int code;
    char msg[256];
} gomupdf_bigstack_call;

static inline void gomupdf_bigstack_run(void *p) {
    gomupdf_bigstack_call *c = p;
    fz_try(c->ctx) c->fn(c->ctx, c->arg);
    fz_catch(c->ctx) {
        c->code = fz_caught(c->ctx);
        snprintf(c->msg, sizeof c->msg, "%s", fz_caught_message(c->ctx));
        fz_ignore_error(c->ctx);
    }
}

/*
 * gomupdf_bigstack runs fn(ctx, arg), which may throw, on a large stack
 * (gomupdf_call_bigstack) and throws its error again on the calling thread,
 * with the same code and message; call it inside fz_try like the MuPDF call
 * it wraps.
 */
static inline void gomupdf_bigstack(fz_context *ctx, void (*fn)(fz_context *, void *), void *arg) {
    gomupdf_bigstack_call c = {ctx, fn, arg, FZ_ERROR_NONE, ""};
    int e = gomupdf_call_bigstack(gomupdf_bigstack_run, &c);
    if (e) fz_throw(ctx, FZ_ERROR_SYSTEM, "cannot create a thread with a large stack: %s", strerror(e));
    if (c.code != FZ_ERROR_NONE) fz_throw(ctx, c.code, "%s", c.msg);
}

/* gomupdf_run_page_big is fz_run_page, or fz_run_page_contents with
 * contents_only, on a large stack (gomupdf_bigstack). */
typedef struct {
    fz_page *page;
    fz_device *dev;
    int contents_only;
} gomupdf_run_page_args;

static inline void gomupdf_run_page_work(fz_context *ctx, void *p) {
    gomupdf_run_page_args *a = p;
    if (a->contents_only) fz_run_page_contents(ctx, a->page, a->dev, fz_identity, NULL);
    else fz_run_page(ctx, a->page, a->dev, fz_identity, NULL);
}

static inline void gomupdf_run_page_big(fz_context *ctx, fz_page *page, fz_device *dev, int contents_only) {
    gomupdf_run_page_args a = {page, dev, contents_only};
    gomupdf_bigstack(ctx, gomupdf_run_page_work, &a);
}

/* gomupdf_write_big is pdf_write_document to out, or pdf_save_document to
 * path when out is NULL, on a large stack (gomupdf_bigstack). */
typedef struct {
    pdf_document *pdf;
    fz_output *out;
    const char *path;
    pdf_write_options *opts;
} gomupdf_write_args;

static inline void gomupdf_write_work(fz_context *ctx, void *p) {
    gomupdf_write_args *a = p;
    if (a->out) pdf_write_document(ctx, a->pdf, a->out, a->opts);
    else pdf_save_document(ctx, a->pdf, a->path, a->opts);
}

static inline void gomupdf_write_big(fz_context *ctx, pdf_document *pdf, fz_output *out, const char *path,
                                     pdf_write_options *opts) {
    gomupdf_write_args a = {pdf, out, path, opts};
    gomupdf_bigstack(ctx, gomupdf_write_work, &a);
}

#endif
