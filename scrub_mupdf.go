//go:build !nomupdf

package gomupdf

/*

#include <mupdf/fitz.h>
#include <mupdf/pdf.h>
#include <limits.h>
#include <stdlib.h>
#include <stdio.h>
#include <string.h>

// Limits for the walks over hidden content. Action, name-tree and field-tree
// walks recurse, and cgo calls from ordinary goroutines run on small thread
// stacks (128 KiB on musl), so the depth cap is what keeps the C stack
// bounded; the budgets bound the work, charging every node and every array
// entry or name-tree pair a walk looks at, so shared subgraphs cannot multiply
// it. Real /Next chains, name trees and field trees are a few levels deep.
// Every walk fails closed on any limit.
#define GOMUPDF_MAX_DEPTH 32
#define GOMUPDF_ACTION_BUDGET (1 << 16) // action nodes per HiddenContent call
#define GOMUPDF_TREE_BUDGET (1 << 16)   // name-tree or field-tree units per walk

// gomupdf_add adds v >= 0 to *x, saturating at INT_MAX instead of wrapping.
static void gomupdf_add(int *x, long long v) {
    long long s = (long long)*x + v;
    *x = s > INT_MAX ? INT_MAX : (int)s;
}

// gomupdf_action_unsafe reports whether act (an action dictionary, or an array
// of them as /Next allows) is, or chains through /Next to, a JavaScript or
// Launch action. A graph too deep or too large for the budget counts as unsafe.
static int gomupdf_action_unsafe(fz_context *ctx, pdf_obj *act, int depth, int *budget) {
    if (depth > GOMUPDF_MAX_DEPTH || --*budget < 0) return 1;
    if (pdf_is_array(ctx, act)) {
        int n = pdf_array_len(ctx, act);
        for (int i = 0; i < n; i++)
            if (gomupdf_action_unsafe(ctx, pdf_array_get(ctx, act, i), depth + 1, budget)) return 1;
        return 0;
    }
    if (!pdf_is_dict(ctx, act)) return 0;
    pdf_obj *s = pdf_dict_get(ctx, act, PDF_NAME(S));
    if (pdf_name_eq(ctx, s, PDF_NAME(JavaScript)) || pdf_name_eq(ctx, s, PDF_NAME(Launch)))
        return 1;
    return gomupdf_action_unsafe(ctx, pdf_dict_get(ctx, act, PDF_NAME(Next)), depth + 1, budget);
}

// gomupdf_open_action_unsafe: an array /OpenAction is a destination and is
// harmless; a dictionary is an action and is walked.
static int gomupdf_open_action_unsafe(fz_context *ctx, pdf_obj *root, int *budget) {
    pdf_obj *oa = pdf_dict_gets(ctx, root, "OpenAction");
    return pdf_is_dict(ctx, oa) && gomupdf_action_unsafe(ctx, oa, 0, budget);
}

// gomupdf_aa_unsafe reports whether any action in an /AA (additional-actions)
// dictionary is unsafe, charging the caller's action budget.
static int gomupdf_aa_unsafe(fz_context *ctx, pdf_obj *aa, int *budget) {
    int n = pdf_dict_len(ctx, aa);
    for (int i = 0; i < n; i++)
        if (gomupdf_action_unsafe(ctx, pdf_dict_get_val(ctx, aa, i), 0, budget)) return 1;
    return 0;
}

// Scrub keeps a short of marks per object number. Before it changes anything
// it marks kept content (GM_KEEP): the trailer /Root and /Encrypt, every
// page-tree node (from /Root /Pages down /Kids, and from every page MuPDF
// counts up /Parent), the full closures of each node's /Contents, /Resources
// and /Group, of /Encrypt and /OCProperties and, with KeepAnnotations, of
// /AcroForm, and the closures of the kept annotations (Links, or all of them
// with KeepAnnotations) and, with KeepOutlines, of /Outlines, except through
// the keys gomupdf_keep_tops skips on those dictionaries themselves. Each role
// has its own queued bit, so an object with several is expanded in each, and
// one reached as content is always walked as content (see gomupdf_walk_skips).
// The closures of the page-tree /Contents, /Resources and /Group, /Encrypt
// and /OCProperties are walked first and also get GM_CONTENT: in them any
// dictionary key can be a name with a meaning (a resource, a crypt filter, an
// optional-content setting), so gomupdf_own never lets Scrub delete a key
// from one of them in place.
// Then it records what it removes (GM_DEL) and finally destroys every GM_DEL
// object that is not GM_KEEP; a kept one is only unlinked. Objects Scrub
// creates (gomupdf_own's copies) are numbered past the marks, so they are
// never marked, recorded or destroyed.
#define GM_KEEP 1
#define GM_DEL 2
#define GM_NODE 4     // the catalog or a page-tree node, queued for node expansion
#define GM_KIDS 8     // an indirect array already expanded as /Kids
#define GM_WALK 16    // queued for the content walk
#define GM_TOP 32     // queued as a kept annotation or outline item
#define GM_PAGE 64    // a page MuPDF counts, which Scrub edits
#define GM_ANNOTS 128 // an indirect array already expanded as /Annots
#define GM_CONTENT 256 // queued for the content walk from a page-content root

enum { GS_WALK, GS_TOP, GS_DOWN, GS_UP, GS_LINK, GS_COUNT };

// Pending objects of the marking walk; every entry holds a reference.
typedef struct { pdf_obj **v; size_t n, cap; } gomupdf_stack;

typedef struct {
    unsigned short *m; // marks, indexed by object number
    int len;          // entries in m
    int root;         // object number of the catalog, which Scrub edits; 0 if none
    int content;      // set while queuing from page-content roots (GM_CONTENT)
    gomupdf_stack s[GS_COUNT];
    pdf_obj *cur;     // the entry being expanded, holding a reference
} gomupdf_marks;

// gomupdf_mark returns the marks of an indirect object, or NULL for a direct
// object, an object number outside the xref, or a NULL mk (HiddenContent).
static unsigned short *gomupdf_mark(fz_context *ctx, gomupdf_marks *mk, pdf_obj *ref) {
    if (!mk || !pdf_is_indirect(ctx, ref)) return NULL;
    int num = pdf_to_num(ctx, ref);
    return num > 0 && num < mk->len ? &mk->m[num] : NULL;
}

static void gomupdf_push(fz_context *ctx, gomupdf_marks *mk, int which, pdf_obj *obj) {
    gomupdf_stack *s = &mk->s[which];
    if (s->n == s->cap) {
        size_t cap = s->cap ? 2 * s->cap : 256;
        s->v = fz_realloc_array(ctx, s->v, cap, pdf_obj *);
        s->cap = cap;
    }
    s->v[s->n++] = pdf_keep_obj(ctx, obj);
}

static void gomupdf_marks_free(fz_context *ctx, gomupdf_marks *mk) {
    for (int i = 0; i < GS_COUNT; i++) {
        gomupdf_stack *s = &mk->s[i];
        while (s->n) pdf_drop_obj(ctx, s->v[--s->n]);
        fz_free(ctx, s->v);
        s->v = NULL;
        s->cap = 0;
    }
    pdf_drop_obj(ctx, mk->cur);
    mk->cur = NULL;
    fz_free(ctx, mk->m);
    mk->m = NULL;
    mk->len = 0;
}

// gomupdf_keep_on marks obj kept and queues it on the WALK stack (keep all it
// references) or the TOP stack (a kept annotation or outline item). Each
// indirect object is queued once per stack; an object number outside the xref
// resolves to null and holds nothing.
static void gomupdf_keep_on(fz_context *ctx, gomupdf_marks *mk, int which, pdf_obj *obj) {
    if (pdf_is_indirect(ctx, obj)) {
        unsigned short *m = gomupdf_mark(ctx, mk, obj);
        unsigned short queued = which == GS_TOP ? GM_TOP : GM_WALK;
        if (!m || (*m & queued)) return;
        *m |= GM_KEEP | queued | (which == GS_WALK && mk->content ? GM_CONTENT : 0);
    } else if (!pdf_is_array(ctx, obj) && !pdf_is_dict(ctx, obj)) {
        return;
    }
    gomupdf_push(ctx, mk, which, obj);
}
#define gomupdf_keep(ctx, mk, obj) gomupdf_keep_on(ctx, mk, GS_WALK, obj)

// gomupdf_keep_tops expands the kept annotations and outline items:
// everything they reference is kept except through their own /Parent, /P,
// /Popup, /AA and /SE, which lead up a tree, to another annotation, to
// actions Scrub removes or into the structure tree (one that is also reached
// as content is walked in full as well). Outline siblings and children are
// expanded the same way.
// Nothing in the loop body resolves an object after o, so o stays valid.
static void gomupdf_keep_tops(fz_context *ctx, gomupdf_marks *mk) {
    gomupdf_stack *s = &mk->s[GS_TOP];
    while (s->n) {
        mk->cur = s->v[--s->n];
        pdf_obj *o = pdf_resolve_indirect(ctx, mk->cur);
        int n = pdf_dict_len(ctx, o);
        for (int i = 0; i < n; i++) {
            pdf_obj *k = pdf_dict_get_key(ctx, o, i);
            if (pdf_name_eq(ctx, k, PDF_NAME(Parent)) || pdf_name_eq(ctx, k, PDF_NAME(P))
                || pdf_name_eq(ctx, k, PDF_NAME(Popup)) || pdf_name_eq(ctx, k, PDF_NAME(AA))
                || !strcmp(pdf_to_name(ctx, k), "SE"))
                continue;
            int item = pdf_name_eq(ctx, k, PDF_NAME(First)) || pdf_name_eq(ctx, k, PDF_NAME(Last))
                || pdf_name_eq(ctx, k, PDF_NAME(Next)) || pdf_name_eq(ctx, k, PDF_NAME(Prev));
            gomupdf_keep_on(ctx, mk, item ? GS_TOP : GS_WALK, pdf_dict_get_val(ctx, o, i));
        }
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = NULL;
    }
}

// gomupdf_walk_skips reports whether the content walk passes over key k of a
// page (is_page) or the catalog (is_root). Only these objects, which Scrub
// edits, skip anything, and only what they lose to Scrub or what has its own
// role: on a page, the keys Scrub removes and /Annots (the annotations that
// stay are kept as such); on the catalog, the keys Scrub removes or edits,
// /Pages and /StructTreeRoot (which reaches removed annotations through
// /OBJR). Every other object, whatever other role it has, is walked through
// every key: in a resource subdictionary any name, /P or /Thumb included, can
// be a resource.
static int gomupdf_walk_skips(fz_context *ctx, int is_page, int is_root, pdf_obj *k) {
    static const char *page[] = {"Thumb", "AA", "AF", "Metadata", "PieceInfo", "Annots", NULL};
    static const char *root[] = {"Metadata", "AA", "AF", "PieceInfo", "AcroForm", "Outlines",
                                 "OpenAction", "Names", "Pages", "StructTreeRoot", NULL};
    const char *name = pdf_to_name(ctx, k);
    for (const char **p = page; is_page && *p; p++)
        if (!strcmp(name, *p)) return 1;
    for (const char **p = root; is_root && *p; p++)
        if (!strcmp(name, *p)) return 1;
    return 0;
}

// gomupdf_keep_walk keeps the transitive closure of the queued objects,
// iteratively: the visited marks bound the work by the size of the file. It
// follows every key, except those gomupdf_walk_skips names on pages and the
// catalog. Nothing in the loop body resolves an object after o, so o stays
// valid.
static void gomupdf_keep_walk(fz_context *ctx, gomupdf_marks *mk) {
    gomupdf_stack *s = &mk->s[GS_WALK];
    while (s->n) {
        mk->cur = s->v[--s->n];
        unsigned short *m = gomupdf_mark(ctx, mk, mk->cur);
        int is_page = m && (*m & GM_PAGE), is_root = m && pdf_to_num(ctx, mk->cur) == mk->root;
        pdf_obj *o = pdf_resolve_indirect(ctx, mk->cur);
        if (pdf_is_array(ctx, o)) {
            int n = pdf_array_len(ctx, o);
            for (int i = 0; i < n; i++) gomupdf_keep(ctx, mk, pdf_array_get(ctx, o, i));
        } else if (pdf_is_dict(ctx, o)) {
            int n = pdf_dict_len(ctx, o);
            for (int i = 0; i < n; i++)
                if (!gomupdf_walk_skips(ctx, is_page, is_root, pdf_dict_get_key(ctx, o, i)))
                    gomupdf_keep(ctx, mk, pdf_dict_get_val(ctx, o, i));
        }
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = NULL;
    }
}

// gomupdf_push_node marks node as a kept page-tree node and queues it on the
// DOWN or UP stack, once.
static void gomupdf_push_node(fz_context *ctx, gomupdf_marks *mk, int which, pdf_obj *node) {
    if (pdf_is_indirect(ctx, node)) {
        unsigned short *m = gomupdf_mark(ctx, mk, node);
        if (!m || (*m & GM_NODE)) return;
        *m |= GM_NODE | GM_KEEP;
    } else if (!pdf_is_dict(ctx, node)) {
        return;
    }
    gomupdf_push(ctx, mk, which, node);
}

// gomupdf_push_entries queues the entries of a /Kids (DOWN) or /Annots (LINK)
// array. An indirect array shared by many nodes is expanded only once in each
// role.
static void gomupdf_push_entries(fz_context *ctx, gomupdf_marks *mk, int which, pdf_obj *arr) {
    unsigned short *m = gomupdf_mark(ctx, mk, arr);
    unsigned short seen = which == GS_LINK ? GM_ANNOTS : GM_KIDS;
    if (m) {
        if (*m & seen) return;
        *m |= seen;
    }
    int n = pdf_array_len(ctx, arr);
    for (int i = 0; i < n; i++) {
        pdf_obj *e = pdf_array_get(ctx, arr, i);
        if (which == GS_LINK) gomupdf_push(ctx, mk, GS_LINK, e);
        else gomupdf_push_node(ctx, mk, which, e);
    }
}

// gomupdf_keep_nodes expands the queued page-tree nodes: it keeps what
// rendering takes from a node (/Contents, /Resources, which the pages below a
// /Pages node inherit, and /Group), queues its /Annots entries as candidate
// kept annotations, and queues /Parent (up) and, on the DOWN stack, /Kids.
// The node is held while expanding it, as pdf_array_len below resolves other
// objects.
static void gomupdf_keep_nodes(fz_context *ctx, gomupdf_marks *mk, int which) {
    gomupdf_stack *s = &mk->s[which];
    while (s->n) {
        mk->cur = s->v[--s->n];
        pdf_obj *o = pdf_keep_obj(ctx, pdf_resolve_indirect(ctx, mk->cur));
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = o;
        if (pdf_is_dict(ctx, o)) {
            gomupdf_keep(ctx, mk, pdf_dict_get(ctx, o, PDF_NAME(Contents)));
            gomupdf_keep(ctx, mk, pdf_dict_get(ctx, o, PDF_NAME(Resources)));
            gomupdf_keep(ctx, mk, pdf_dict_get(ctx, o, PDF_NAME(Group)));
            gomupdf_push_entries(ctx, mk, GS_LINK, pdf_dict_get(ctx, o, PDF_NAME(Annots)));
            if (which == GS_DOWN) gomupdf_push_entries(ctx, mk, GS_DOWN, pdf_dict_get(ctx, o, PDF_NAME(Kids)));
            gomupdf_push_node(ctx, mk, GS_UP, pdf_dict_get(ctx, o, PDF_NAME(Parent)));
        }
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = NULL;
    }
}

static int gomupdf_is_link(fz_context *ctx, pdf_obj *annot) {
    return pdf_name_eq(ctx, pdf_dict_get(ctx, annot, PDF_NAME(Subtype)), PDF_NAME(Link));
}

// gomupdf_keep_pass marks the kept content of the document (see GM_KEEP).
static void gomupdf_keep_pass(fz_context *ctx, pdf_document *pdf, gomupdf_marks *mk,
                              int keep_annots, int keep_outlines) {
    pdf_obj *trailer = pdf_trailer(ctx, pdf);
    pdf_obj *root = pdf_dict_get(ctx, trailer, PDF_NAME(Root));
    unsigned short *m = gomupdf_mark(ctx, mk, root);
    if (m) *m |= GM_NODE | GM_KEEP;
    mk->content = 1;
    mk->root = m ? pdf_to_num(ctx, root) : 0;
    gomupdf_push_node(ctx, mk, GS_DOWN, pdf_dict_get(ctx, root, PDF_NAME(Pages)));
    gomupdf_keep_nodes(ctx, mk, GS_DOWN);
    int npages = pdf_count_pages(ctx, pdf);
    for (int p = 0; p < npages; p++) {
        pdf_obj *page = pdf_lookup_page_obj(ctx, pdf, p);
        if ((m = gomupdf_mark(ctx, mk, page))) *m |= GM_PAGE;
        gomupdf_push_node(ctx, mk, GS_UP, page);
    }
    gomupdf_keep_nodes(ctx, mk, GS_UP);
    gomupdf_keep(ctx, mk, pdf_dict_get(ctx, trailer, PDF_NAME(Encrypt)));
    gomupdf_keep(ctx, mk, pdf_dict_get(ctx, root, PDF_NAME(OCProperties)));
    gomupdf_keep_walk(ctx, mk);
    mk->content = 0;

    // Kept annotations: Links, or every /Annots entry with keep_annots.
    gomupdf_stack *links = &mk->s[GS_LINK];
    while (links->n) {
        mk->cur = links->v[--links->n];
        if (keep_annots || gomupdf_is_link(ctx, mk->cur)) gomupdf_keep_on(ctx, mk, GS_TOP, mk->cur);
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = NULL;
    }
    if (keep_outlines) gomupdf_keep_on(ctx, mk, GS_TOP, pdf_dict_get(ctx, root, PDF_NAME(Outlines)));
    gomupdf_keep_tops(ctx, mk);
    if (keep_annots) gomupdf_keep(ctx, mk, pdf_dict_get(ctx, root, PDF_NAME(AcroForm)));
    gomupdf_keep_walk(ctx, mk);
}

// gomupdf_load_all loads every object in the xref. MuPDF repairs (rebuilds)
// a broken xref at most once per document, and only while loading an object,
// so a repair any object needs happens here, before Scrub marks or changes
// anything, and none can happen later. An object that fails to load is left
// to fail again where it is used. Loading caches the object in the xref, so
// there is no reference to drop.
static void gomupdf_load_all(fz_context *ctx, pdf_document *pdf) {
    for (int i = 1; i < pdf_xref_len(ctx, pdf); i++) {
        fz_try(ctx) pdf_cache_object(ctx, pdf, i);
        fz_catch(ctx) {
            fz_rethrow_if(ctx, FZ_ERROR_SYSTEM);
            fz_ignore_error(ctx);
        }
    }
}

// gomupdf_keep_all allocates the marks and runs gomupdf_keep_pass. A broken
// object can make MuPDF repair (rebuild) the xref while the pass loads it, so
// a pass during which that happened is redone on the repaired xref (a
// backstop: gomupdf_load_all makes any repair happen before). Returns
// pdf->repair_attempted as the marks saw it.
static int gomupdf_keep_all(fz_context *ctx, pdf_document *pdf, gomupdf_marks *mk,
                            int keep_annots, int keep_outlines) {
    for (int attempt = 0;; attempt++) {
        int repaired = pdf->repair_attempted;
        size_t len = (size_t)pdf_xref_len(ctx, pdf);
        fz_free(ctx, mk->m);
        mk->m = NULL;
        mk->len = 0;
        mk->m = fz_calloc(ctx, len ? len : 1, sizeof *mk->m);
        mk->len = (int)len;
        gomupdf_keep_pass(ctx, pdf, mk, keep_annots, keep_outlines);
        if (pdf->repair_attempted == repaired) return repaired;
        if (attempt) fz_throw(ctx, FZ_ERROR_FORMAT, "xref repaired while marking kept content");
    }
}

// Kinds of removed object. gomupdf_is_kind identifies each positively, so a
// removed slot that points at an object of another kind never destroys it
// (defence in depth: the GM_KEEP marks are what protect kept content).
enum {
    GK_INFO, GK_XMP, GK_TREE, GK_FILESPEC, GK_EF, GK_EMBEDDED, GK_JSACTION, GK_SCRIPT,
    GK_ANNOT, GK_FIELD, GK_ACROFORM, GK_THUMB
};

static int gomupdf_is_kind(fz_context *ctx, pdf_obj *o, int kind) {
#define GK_HAS(k) (pdf_dict_get(ctx, o, PDF_NAME(k)) != NULL)
#define GK_IS(v, k) pdf_name_eq(ctx, v, PDF_NAME(k))
    pdf_obj *t = pdf_dict_get(ctx, o, PDF_NAME(Type));
    pdf_obj *st = pdf_dict_get(ctx, o, PDF_NAME(Subtype));
    int stream = pdf_is_stream(ctx, o), dict = !stream && pdf_is_dict(ctx, o);
    if (GK_IS(st, Link)) return 0;
    switch (kind) {
    case GK_INFO: case GK_EF: return dict && !t;
    case GK_ACROFORM: return dict && !t && (GK_HAS(Fields) || GK_HAS(XFA));
    case GK_TREE: return dict && !t && (GK_HAS(Names) || GK_HAS(Kids));
    case GK_FILESPEC:
        return dict && (GK_IS(t, Filespec) || (!t && (GK_HAS(EF) || GK_HAS(F) || GK_HAS(UF))));
    case GK_JSACTION:
        return dict && (!t || GK_IS(t, Action)) && GK_IS(pdf_dict_get(ctx, o, PDF_NAME(S)), JavaScript);
    case GK_ANNOT: return dict && (!t || GK_IS(t, Annot)) && st;
    case GK_FIELD:
        return dict && (!t || GK_IS(t, Annot))
            && (GK_HAS(FT) || GK_HAS(T) || GK_HAS(Kids) || GK_IS(st, Widget));
    case GK_XMP: return stream && (GK_IS(t, Metadata) || GK_IS(st, XML));
    case GK_EMBEDDED:
        return stream && (GK_IS(t, EmbeddedFile)
            || (!t && !GK_IS(st, Form) && !GK_IS(st, Image) && !GK_IS(st, PS)));
    case GK_SCRIPT: return pdf_is_string(ctx, o) || (stream && !t && !st);
    case GK_THUMB:
        return stream && GK_HAS(Width) && (!t || GK_IS(t, XObject)) && (!st || GK_IS(st, Image));
    }
    return 0;
#undef GK_HAS
#undef GK_IS
}

// gomupdf_destroy records obj for destruction if it is an indirect object of
// the given kind. Direct objects go with their parent. A NULL mk
// (HiddenContent) records nothing.
static void gomupdf_destroy(fz_context *ctx, gomupdf_marks *mk, pdf_obj *obj, int kind) {
    unsigned short *m = gomupdf_mark(ctx, mk, obj);
    if (m && gomupdf_is_kind(ctx, obj, kind)) *m |= GM_DEL;
}

// gomupdf_destroy_filespec records a removed /AF entry, /EmbeddedFiles value
// or annotation /FS: the filespec, its /EF dictionary and embedded-file streams.
static void gomupdf_destroy_filespec(fz_context *ctx, gomupdf_marks *mk, pdf_obj *fs) {
    if (!mk || !gomupdf_is_kind(ctx, fs, GK_FILESPEC)) return;
    gomupdf_destroy(ctx, mk, fs, GK_FILESPEC);
    pdf_obj *ef = pdf_dict_get(ctx, fs, PDF_NAME(EF));
    gomupdf_destroy(ctx, mk, ef, GK_EF);
    int n = pdf_dict_len(ctx, ef);
    for (int i = 0; i < n; i++) gomupdf_destroy(ctx, mk, pdf_dict_get_val(ctx, ef, i), GK_EMBEDDED);
}

// gomupdf_destroy_js records a removed /Names/JavaScript value: the
// JavaScript action and its script string or stream.
static void gomupdf_destroy_js(fz_context *ctx, gomupdf_marks *mk, pdf_obj *act) {
    if (!mk || !gomupdf_is_kind(ctx, act, GK_JSACTION)) return;
    gomupdf_destroy(ctx, mk, act, GK_JSACTION);
    gomupdf_destroy(ctx, mk, pdf_dict_get(ctx, act, PDF_NAME(JS)), GK_SCRIPT);
}

// gomupdf_destroy_all deletes every recorded object that is not kept: every
// reference to it that survives elsewhere (a StructElem /OBJR or /AF, say)
// then resolves to null, and a garbage-collecting save rewrites it as null.
static void gomupdf_destroy_all(fz_context *ctx, pdf_document *pdf, gomupdf_marks *mk) {
    for (int i = 1; i < mk->len; i++)
        if ((mk->m[i] & (GM_DEL | GM_KEEP)) == GM_DEL) pdf_delete_object(ctx, pdf, i);
}

// gomupdf_name_tree counts the key/value pairs of a name tree, following
// /Kids, and records every node and value (filespecs, or JavaScript actions
// if js) in del when not NULL. Each node, /Kids entry and /Names pair costs
// one unit of *budget. A node deeper than the depth cap or on a /Kids cycle,
// and everything past the budget, sets *incomplete and is skipped.
static long long gomupdf_name_tree(fz_context *ctx, pdf_obj *node, int depth, pdf_cycle_list *up,
                                   int *budget, int *incomplete, gomupdf_marks *del, int js) {
    pdf_cycle_list here;
    if (!pdf_is_dict(ctx, node)) return 0;
    if (depth > GOMUPDF_MAX_DEPTH || --*budget < 0 || pdf_cycle(ctx, &here, up, node)) {
        *incomplete = 1;
        return 0;
    }
    gomupdf_destroy(ctx, del, node, GK_TREE);
    pdf_obj *names = pdf_dict_get(ctx, node, PDF_NAME(Names));
    int pairs = pdf_array_len(ctx, names) / 2;
    if (pairs > *budget) {
        *incomplete = 1;
        pairs = *budget;
    }
    *budget -= pairs;
    for (int i = 0; i < pairs; i++) {
        pdf_obj *v = pdf_array_get(ctx, names, 2 * i + 1);
        if (js) gomupdf_destroy_js(ctx, del, v);
        else gomupdf_destroy_filespec(ctx, del, v);
    }
    long long count = pairs;
    pdf_obj *kids = pdf_dict_get(ctx, node, PDF_NAME(Kids));
    int n = pdf_array_len(ctx, kids);
    for (int i = 0; i < n; i++) {
        if (--*budget < 0) {
            *incomplete = 1;
            break;
        }
        count += gomupdf_name_tree(ctx, pdf_array_get(ctx, kids, i), depth + 1, &here,
                                   budget, incomplete, del, js);
    }
    return count;
}

// gomupdf_destroy_name_tree records a whole name tree for destruction.
static void gomupdf_destroy_name_tree(fz_context *ctx, gomupdf_marks *del, pdf_obj *tree, int js) {
    int budget = GOMUPDF_TREE_BUDGET, incomplete = 0;
    gomupdf_name_tree(ctx, tree, 0, NULL, &budget, &incomplete, del, js);
}

// gomupdf_destroy_fields records the /AcroForm field tree (fields and their
// widget kids) for destruction, following /Kids within the limits; each
// array entry costs one unit of *budget.
static void gomupdf_destroy_fields(fz_context *ctx, gomupdf_marks *del, pdf_obj *fields,
                                   int depth, pdf_cycle_list *up, int *budget) {
    int n = pdf_array_len(ctx, fields);
    for (int i = 0; i < n && --*budget >= 0; i++) {
        pdf_obj *f = pdf_array_get(ctx, fields, i);
        pdf_cycle_list here;
        if (!pdf_is_dict(ctx, f) || depth > GOMUPDF_MAX_DEPTH || pdf_cycle(ctx, &here, up, f))
            continue;
        gomupdf_destroy(ctx, del, f, GK_FIELD);
        gomupdf_destroy_fields(ctx, del, pdf_dict_get(ctx, f, PDF_NAME(Kids)), depth + 1, &here, budget);
    }
}

// Indices into the out array of gomupdf_hidden_content; scrub.go reads the same
// order.
enum {
    HC_INFO, HC_XMP, HC_ACROFORM, HC_OUTLINES, HC_EMBEDDED, HC_AF, HC_JS,
    HC_NONLINK, HC_WIDGETS, HC_THUMBS, HC_PAGEMETA, HC_PIECEINFO, HC_COUNT
};

static int gomupdf_hidden_content(fz_context *ctx, fz_document *doc, int *out,
                                  char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    for (int i = 0; i < HC_COUNT; i++) out[i] = 0;
    fz_try(ctx) {
        pdf_obj *trailer = pdf_trailer(ctx, pdf);
        pdf_obj *root = pdf_dict_get(ctx, trailer, PDF_NAME(Root));
        pdf_obj *names = pdf_dict_get(ctx, root, PDF_NAME(Names));
        int budget = GOMUPDF_TREE_BUDGET, incomplete = 0;
        // One action budget for the whole call: a large /AA shared by many
        // annotations, or by many keys, is walked against it only once.
        int abudget = GOMUPDF_ACTION_BUDGET;

        out[HC_INFO] = pdf_dict_get(ctx, trailer, PDF_NAME(Info)) != NULL;
        out[HC_XMP] = pdf_dict_get(ctx, root, PDF_NAME(Metadata)) != NULL;
        out[HC_ACROFORM] = pdf_dict_get(ctx, root, PDF_NAME(AcroForm)) != NULL;
        out[HC_OUTLINES] = pdf_dict_get(ctx, root, PDF_NAME(Outlines)) != NULL;
        gomupdf_add(&out[HC_EMBEDDED], gomupdf_name_tree(ctx, pdf_dict_get(ctx, names, PDF_NAME(EmbeddedFiles)),
                                                         0, NULL, &budget, &incomplete, NULL, 0));
        // Fail closed: a tree that could not be walked in full is never "none".
        if (incomplete && out[HC_EMBEDDED] == 0) out[HC_EMBEDDED] = 1;
        out[HC_AF] = pdf_dict_get(ctx, root, PDF_NAME(AF)) != NULL;
        out[HC_PIECEINFO] = pdf_dict_get(ctx, root, PDF_NAME(PieceInfo)) != NULL;
        out[HC_JS] = pdf_dict_get(ctx, names, PDF_NAME(JavaScript)) != NULL
            || gomupdf_open_action_unsafe(ctx, root, &abudget)
            || gomupdf_aa_unsafe(ctx, pdf_dict_get(ctx, root, PDF_NAME(AA)), &abudget);

        int npages = pdf_count_pages(ctx, pdf);
        for (int p = 0; p < npages; p++) {
            pdf_obj *page = pdf_lookup_page_obj(ctx, pdf, p);
            gomupdf_add(&out[HC_THUMBS], pdf_dict_get(ctx, page, PDF_NAME(Thumb)) != NULL);
            gomupdf_add(&out[HC_PAGEMETA], pdf_dict_get(ctx, page, PDF_NAME(Metadata)) != NULL);
            gomupdf_add(&out[HC_AF], pdf_dict_get(ctx, page, PDF_NAME(AF)) != NULL);
            gomupdf_add(&out[HC_PIECEINFO], pdf_dict_get(ctx, page, PDF_NAME(PieceInfo)) != NULL);
            if (!out[HC_JS] && gomupdf_aa_unsafe(ctx, pdf_dict_get(ctx, page, PDF_NAME(AA)), &abudget))
                out[HC_JS] = 1;

            pdf_obj *annots = pdf_dict_get(ctx, page, PDF_NAME(Annots));
            int n = pdf_array_len(ctx, annots);
            for (int i = 0; i < n; i++) {
                pdf_obj *a = pdf_array_get(ctx, annots, i);
                if (!pdf_is_dict(ctx, a)) continue;
                if (!out[HC_JS] && gomupdf_aa_unsafe(ctx, pdf_dict_get(ctx, a, PDF_NAME(AA)), &abudget))
                    out[HC_JS] = 1;
                if (gomupdf_is_link(ctx, a)) continue;
                if (pdf_name_eq(ctx, pdf_dict_get(ctx, a, PDF_NAME(Subtype)), PDF_NAME(Widget)))
                    gomupdf_add(&out[HC_WIDGETS], 1);
                else
                    gomupdf_add(&out[HC_NONLINK], 1);
            }
        }
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

// gomupdf_own returns the value of key in the dictionary parent (or, with a
// NULL key, entry i of the array parent) that Scrub is about to edit in
// place. If that value is an indirect array or dictionary reached from page
// content (GM_CONTENT), parent first gets a private copy of it, so the edit
// never reaches what the page draws: a new indirect object if indirect (an
// annotation that stays, which /Annots must reference indirectly), else a
// direct one. Anything else, including what only kept annotations, /AcroForm
// or outlines reach, is edited in place, so that no other reference keeps
// what Scrub removes from it and the field tree, /IRT and /Popup links stay.
static pdf_obj *gomupdf_own(fz_context *ctx, gomupdf_marks *mk, pdf_obj *parent, pdf_obj *key, int i,
                            int indirect) {
    pdf_obj *v = key ? pdf_dict_get(ctx, parent, key) : pdf_array_get(ctx, parent, i);
    unsigned short *m = gomupdf_mark(ctx, mk, v);
    pdf_obj *o = pdf_resolve_indirect(ctx, v);
    if (!m || !(*m & GM_CONTENT) || !(pdf_is_array(ctx, o) || pdf_is_dict(ctx, o))) return v;
    pdf_obj *copy = pdf_deep_copy_obj(ctx, o);
    // The new object is numbered past the marks (pdf_create_object appends).
    if (indirect) copy = pdf_add_object_drop(ctx, pdf_get_indirect_document(ctx, v), copy);
    fz_try(ctx) {
        if (key) pdf_dict_put(ctx, parent, key, copy);
        else pdf_array_put(ctx, parent, i, copy);
    }
    fz_always(ctx) pdf_drop_obj(ctx, copy);
    fz_catch(ctx) fz_rethrow(ctx);
    return copy; // parent holds it
}

// gomupdf_own_annots applies gomupdf_own to a page's /Annots array and to
// each of its entries, and returns the array. An entry that stays (a Link, or
// any with keep_annots) is copied to a new indirect object; one Scrub removes
// may be copied directly.
static pdf_obj *gomupdf_own_annots(fz_context *ctx, gomupdf_marks *mk, pdf_obj *page, int keep_annots) {
    pdf_obj *arr = gomupdf_own(ctx, mk, page, PDF_NAME(Annots), 0, 0);
    int n = pdf_array_len(ctx, arr);
    for (int i = 0; i < n; i++)
        gomupdf_own(ctx, mk, arr, NULL, i, keep_annots || gomupdf_is_link(ctx, pdf_array_get(ctx, arr, i)));
    return arr;
}

// gomupdf_scrub_annots deletes every non-Link annotation and widget on one page
// through MuPDF, then sweeps the raw /Annots array for anything MuPDF does not
// list (Popup and unknown subtypes, broken entries), keeping only Links. Every
// removed entry, its /Popup, /FS and /AF filespecs and, for a widget, its
// /Parent field chain are recorded in del first. Before any of that, the
// page takes private copies of the /Annots array and entries kept content
// also uses, and resyncs a page MuPDF already has open with them.
static void gomupdf_scrub_annots(fz_context *ctx, pdf_document *pdf, int pageno, gomupdf_marks *del) {
    pdf_page *page = NULL;
    fz_var(page);
    fz_try(ctx) {
        gomupdf_own_annots(ctx, del, pdf_lookup_page_obj(ctx, pdf, pageno), 0);
        page = pdf_load_page(ctx, pdf, pageno);
        pdf_sync_annots(ctx, page);
        pdf_obj *arr = pdf_dict_get(ctx, page->obj, PDF_NAME(Annots));
        for (int i = 0; i < pdf_array_len(ctx, arr); i++) {
            pdf_obj *e = pdf_array_get(ctx, arr, i);
            if (!pdf_is_dict(ctx, e) || gomupdf_is_link(ctx, e)) continue;
            gomupdf_destroy(ctx, del, e, GK_ANNOT);
            gomupdf_destroy_filespec(ctx, del, pdf_dict_get(ctx, e, PDF_NAME(FS)));
            pdf_obj *af = pdf_dict_get(ctx, e, PDF_NAME(AF));
            for (int j = 0; j < pdf_array_len(ctx, af); j++)
                gomupdf_destroy_filespec(ctx, del, pdf_array_get(ctx, af, j));
            pdf_obj *popup = pdf_dict_get(ctx, e, PDF_NAME(Popup));
            if (pdf_name_eq(ctx, pdf_dict_get(ctx, popup, PDF_NAME(Subtype)), PDF_NAME(Popup)))
                gomupdf_destroy(ctx, del, popup, GK_ANNOT);
            // pdf_delete_annot also unlinks the /Popup target from /Annots,
            // even a Link; the sweep below removes real popups instead.
            pdf_dict_del(ctx, e, PDF_NAME(Popup));
            if (!pdf_name_eq(ctx, pdf_dict_get(ctx, e, PDF_NAME(Subtype)), PDF_NAME(Widget))) continue;
            pdf_obj *f = pdf_dict_get(ctx, e, PDF_NAME(Parent));
            for (int d = 0; d < GOMUPDF_MAX_DEPTH && pdf_is_dict(ctx, f); d++) {
                gomupdf_destroy(ctx, del, f, GK_FIELD);
                f = pdf_dict_get(ctx, f, PDF_NAME(Parent));
            }
        }
        pdf_annot *a, *next;
        // Take next before deleting: pdf_delete_annot drops the page's reference.
        for (a = pdf_first_annot(ctx, page); a; a = next) {
            next = pdf_next_annot(ctx, a);
            if (pdf_annot_type(ctx, a) != PDF_ANNOT_LINK) pdf_delete_annot(ctx, page, a);
        }
        for (a = pdf_first_widget(ctx, page); a; a = next) {
            next = pdf_next_widget(ctx, a);
            pdf_delete_annot(ctx, page, a);
        }
        pdf_obj *annots = pdf_dict_get(ctx, page->obj, PDF_NAME(Annots));
        for (int i = pdf_array_len(ctx, annots) - 1; i >= 0; i--) {
            pdf_obj *e = pdf_array_get(ctx, annots, i);
            if (!pdf_is_dict(ctx, e) || !gomupdf_is_link(ctx, e)) pdf_array_delete(ctx, annots, i);
        }
    }
    fz_always(ctx) { fz_drop_page(ctx, (fz_page *)page); }
    fz_catch(ctx) { fz_rethrow(ctx); }
}

static int gomupdf_scrub(fz_context *ctx, fz_document *doc, int keep_annots,
                         int keep_outlines, char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    gomupdf_marks mk;
    memset(&mk, 0, sizeof mk);
    int marked = 0, repaired = 0, failed = 0;
    fz_var(mk);
    fz_var(marked);
    fz_var(repaired);
    fz_try(ctx) {
        // Let any xref repair happen first, then mark kept content before
        // changing anything.
        gomupdf_load_all(ctx, pdf);
        repaired = gomupdf_keep_all(ctx, pdf, &mk, keep_annots, keep_outlines);
        marked = 1;

        pdf_obj *trailer = pdf_trailer(ctx, pdf);
        pdf_obj *root = pdf_dict_get(ctx, trailer, PDF_NAME(Root));
        pdf_obj *names = pdf_dict_get(ctx, root, PDF_NAME(Names));
        pdf_obj *af = pdf_dict_get(ctx, root, PDF_NAME(AF));
        int abudget = GOMUPDF_ACTION_BUDGET;

        // Record what is removed below while it is still linked.
        gomupdf_destroy(ctx, &mk, pdf_dict_get(ctx, trailer, PDF_NAME(Info)), GK_INFO);
        gomupdf_destroy(ctx, &mk, pdf_dict_get(ctx, root, PDF_NAME(Metadata)), GK_XMP);
        for (int i = 0; i < pdf_array_len(ctx, af); i++)
            gomupdf_destroy_filespec(ctx, &mk, pdf_array_get(ctx, af, i));
        gomupdf_destroy_name_tree(ctx, &mk, pdf_dict_get(ctx, names, PDF_NAME(EmbeddedFiles)), 0);
        gomupdf_destroy_name_tree(ctx, &mk, pdf_dict_get(ctx, names, PDF_NAME(JavaScript)), 1);
        if (!keep_annots) {
            pdf_obj *acroform = pdf_dict_get(ctx, root, PDF_NAME(AcroForm));
            int budget = GOMUPDF_TREE_BUDGET;
            gomupdf_destroy(ctx, &mk, acroform, GK_ACROFORM);
            gomupdf_destroy_fields(ctx, &mk, pdf_dict_get(ctx, acroform, PDF_NAME(Fields)), 0, NULL, &budget);
        }

        pdf_dict_del(ctx, trailer, PDF_NAME(Info));
        pdf_dict_del(ctx, root, PDF_NAME(Metadata));
        pdf_dict_del(ctx, root, PDF_NAME(AA));
        pdf_dict_del(ctx, root, PDF_NAME(AF));
        pdf_dict_del(ctx, root, PDF_NAME(PieceInfo));
        // Unlink /AcroForm before the page loop: pdf_delete_annot on a widget
        // walks /Fields recursively with no depth cap and no cycle check on
        // direct /Kids arrays, so a hostile field tree would overflow the stack.
        if (!keep_annots) pdf_dict_del(ctx, root, PDF_NAME(AcroForm));
        if (names) {
            names = gomupdf_own(ctx, &mk, root, PDF_NAME(Names), 0, 0);
            pdf_dict_del(ctx, names, PDF_NAME(EmbeddedFiles));
            pdf_dict_del(ctx, names, PDF_NAME(JavaScript));
        }
        if (gomupdf_open_action_unsafe(ctx, root, &abudget)) pdf_dict_dels(ctx, root, "OpenAction");
        if (!keep_outlines) {
            pdf_dict_del(ctx, root, PDF_NAME(Outlines));
            if (pdf_name_eq(ctx, pdf_dict_get(ctx, root, PDF_NAME(PageMode)), PDF_NAME(UseOutlines)))
                pdf_dict_put_name(ctx, root, PDF_NAME(PageMode), "UseNone");
        }

        int npages = pdf_count_pages(ctx, pdf);
        for (int p = 0; p < npages; p++) {
            if (!keep_annots) gomupdf_scrub_annots(ctx, pdf, p, &mk);
            pdf_obj *page = pdf_lookup_page_obj(ctx, pdf, p);
            gomupdf_destroy(ctx, &mk, pdf_dict_get(ctx, page, PDF_NAME(Thumb)), GK_THUMB);
            gomupdf_destroy(ctx, &mk, pdf_dict_get(ctx, page, PDF_NAME(Metadata)), GK_XMP);
            af = pdf_dict_get(ctx, page, PDF_NAME(AF));
            for (int i = 0; i < pdf_array_len(ctx, af); i++)
                gomupdf_destroy_filespec(ctx, &mk, pdf_array_get(ctx, af, i));
            pdf_dict_del(ctx, page, PDF_NAME(Thumb));
            pdf_dict_del(ctx, page, PDF_NAME(AA));
            pdf_dict_del(ctx, page, PDF_NAME(AF));
            pdf_dict_del(ctx, page, PDF_NAME(Metadata));
            pdf_dict_del(ctx, page, PDF_NAME(PieceInfo));
            // Annotations that stay (Links, or all with keep_annots) lose their
            // /AA so no script survives, matching what HiddenContent checks.
            pdf_obj *annots = gomupdf_own_annots(ctx, &mk, page, keep_annots);
            int n = pdf_array_len(ctx, annots);
            for (int i = 0; i < n; i++) {
                pdf_obj *a = pdf_array_get(ctx, annots, i);
                if (pdf_is_dict(ctx, a)) pdf_dict_del(ctx, a, PDF_NAME(AA));
            }
        }
    }
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        failed = 1;
    }
    // Destroy what was recorded even when the work above failed partway, so a
    // failed Scrub never leaves unlinked content reachable from elsewhere. The
    // kept-content marks were complete before the first change; if the xref
    // was repaired since (a backstop: gomupdf_load_all makes that
    // impossible), object numbers may name different objects, so nothing is
    // destroyed and Scrub fails.
    if (marked) {
        fz_try(ctx) {
            if (pdf->repair_attempted != repaired)
                fz_throw(ctx, FZ_ERROR_FORMAT, "xref repaired during scrub; removed objects were not destroyed");
            gomupdf_destroy_all(ctx, pdf, &mk);
        }
        fz_catch(ctx) {
            if (!failed) snprintf(err, errlen, "%s", fz_caught_message(ctx));
            failed = 1;
        }
    }
    gomupdf_marks_free(ctx, &mk);
    return failed ? -1 : 0;
}
*/
import "C"

import (
	"errors"
	"unsafe"
)

func (d *mupdfDoc) scrub(opts ScrubOptions) error {
	if d.doc == nil {
		return errors.New("gomupdf: document closed")
	}
	errBuf := (*C.char)(C.malloc(errBufLen))
	defer C.free(unsafe.Pointer(errBuf))
	flag := func(b bool) C.int {
		if b {
			return 1
		}
		return 0
	}
	if C.gomupdf_scrub(d.ctx, d.doc, flag(opts.KeepAnnotations), flag(opts.KeepOutlines), errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: scrub: " + C.GoString(errBuf))
	}
	return nil
}

func (d *mupdfDoc) hiddenContentRaw() ([]int, error) {
	if d.doc == nil {
		return nil, errors.New("gomupdf: document closed")
	}
	errBuf := (*C.char)(C.malloc(errBufLen))
	defer C.free(unsafe.Pointer(errBuf))
	var out [C.HC_COUNT]C.int
	if C.gomupdf_hidden_content(d.ctx, d.doc, &out[0], errBuf, errBufLen) != 0 {
		return nil, errors.New("gomupdf: hidden content: " + C.GoString(errBuf))
	}
	counts := make([]int, len(out))
	for i, v := range out {
		counts[i] = int(v)
	}
	return counts, nil
}
