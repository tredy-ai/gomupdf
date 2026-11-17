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
//
// The action budget scales with the file: each HiddenContent call and each
// Scrub pass may reach actions GOMUPDF_ACTION_FLOOR + GOMUPDF_ACTION_PER_OBJECT
// x (xref length) times (gomupdf_action_budget). Every reach of an action
// (dictionary or /Next array, direct or indirect, including an indirect one
// already decided) costs one unit, and an indirect one is decided once per
// marks (gomupdf_action_walk caches the verdict), so a legitimate file, which
// reaches each action a few times, never runs out, while one shared action
// reached from very many places (a hostile shape) does, and fails closed.
#define GOMUPDF_MAX_DEPTH 32
#define GOMUPDF_ACTION_FLOOR 4096
#define GOMUPDF_ACTION_PER_OBJECT 32
#define GOMUPDF_TREE_BUDGET (1 << 16)   // name-tree or field-tree units per walk

// gomupdf_add adds v >= 0 to *x, saturating at INT_MAX instead of wrapping.
static void gomupdf_add(int *x, long long v) {
    long long s = (long long)*x + v;
    *x = s > INT_MAX ? INT_MAX : (int)s;
}

// Indices into the out array of gomupdf_hidden_content; scrub.go reads the same
// order.
enum {
    HC_INFO, HC_XMP, HC_ACROFORM, HC_OUTLINES, HC_EMBEDDED, HC_AF, HC_JS,
    HC_NONLINK, HC_WIDGETS, HC_THUMBS, HC_PAGEMETA, HC_PIECEINFO, HC_XFA, HC_TRAILER, HC_ORPHANPAGES,
    HC_OBJMETA, HC_LASTMOD, HC_PERMS, HC_DSS, HC_WEBCAPTURE, HC_LAYERS, HC_HIDDENLAYERS, HC_COUNT
};

// Kinds of removed object. gomupdf_is_kind identifies each positively, so a
// removed slot that points at an object of another kind never destroys it
// (defence in depth: the GM_KEEP marks are what protect kept content).
enum {
    GK_INFO, GK_XMP, GK_TREE, GK_FILESPEC, GK_EF, GK_EMBEDDED, GK_ACTION, GK_SCRIPT,
    GK_ANNOT, GK_FIELD, GK_ACROFORM, GK_THUMB, GK_STRING, GK_LAUNCHP, GK_XFA, GK_ANYACTION,
    GK_CONTENTS, GK_XOBJECT, GK_PIECEINFO, GK_PRIVATE, GK_SIG, GK_DSS
};

// gomupdf_name_in reports whether name is in the NULL-terminated list.
static int gomupdf_name_in(const char *name, const char **list) {
    for (; *list; list++)
        if (!strcmp(name, *list)) return 1;
    return 0;
}

// Scrub keeps an int of marks per object number. Before it changes anything
// it marks kept content (GM_KEEP): the trailer /Root and /Encrypt, every
// page-tree node (from /Root /Pages down /Kids, and from every page MuPDF
// serves up /Parent; a page dictionary MuPDF does not serve is an orphan
// page instead, gomupdf_push_node), the full closures of each node's /Contents, /Resources
// and /Group, of /Encrypt and /OCProperties, and the closures of the kept
// annotations (Links, widgets with KeepFormFields, or all of them with
// KeepAnnotations), with KeepOutlines of /Outlines and with KeepFormFields or
// KeepAnnotations of /AcroForm and its field tree,
// except through the keys gomupdf_keep_tops skips on those dictionaries and
// their items, fields and kids themselves. Each role
// has its own queued bit, so an object with several is expanded in each, and
// one reached as content is always walked as content (see gomupdf_walk_skips).
// The closures of the page-tree /Contents, /Resources and /Group, /Encrypt
// and /OCProperties are walked first and also get GM_CONTENT: in them any
// dictionary key can be a name with a meaning (a resource, a crypt filter, an
// optional-content setting), so gomupdf_own never lets Scrub delete a key
// from one of them in place, and Scrub leaves a field-tree node, outline item,
// intermediate page-tree node or orphan page (a page dictionary not in the
// page tree) that is GM_CONTENT as it is. An orphan page reached outside the
// content walks is kept but not expanded (gomupdf_keep_walk).
// Then it records what it removes (GM_DEL) and finally destroys every GM_DEL
// object that is not GM_KEEP; a kept one is only unlinked. Objects Scrub
// creates (gomupdf_own's copies) are numbered past the marks, so they are
// never marked, recorded or destroyed.
//
// A JavaScript or Launch /A or /PA that Scrub removes from a kept annotation,
// field or outline item is decided before it is marked: gomupdf_keep_tops does
// not follow an /A or /PA that gomupdf_action_unsafe calls unsafe (charging
// the marking pass's own action budget), as it never follows /AA. So such an
// action is kept only if something else kept reaches it (page content, say),
// and Scrub then only unlinks it. The verdicts on indirect actions are cached
// in the marks (GM_ASAFE, GM_ABAD), so the edit pass, with a fresh budget,
// reuses the marking pass's verdict on every indirect action that pass
// decided; it decides again only direct actions and actions the marking pass
// never reached. They can differ only past a budget, where an action called
// safe was walked in full and is safe, and one called unsafe is removed or
// left unkept, never kept and left linked as unsafe. A safe catalog
// /OpenAction is kept like a kept annotation's /A.
#define GM_KEEP 1
#define GM_DEL 2
#define GM_NODE 4     // the catalog or a page-tree node, queued for node expansion
#define GM_KIDS 8     // an indirect array already expanded as /Kids
#define GM_WALK 16    // queued for the content walk
#define GM_TOP 32     // queued as a kept annotation or outline item
#define GM_PAGE 64    // a page MuPDF counts, which Scrub edits
#define GM_ANNOTS 128 // an indirect array already expanded as /Annots
#define GM_CONTENT 256 // queued for the content walk from a page-content root
#define GM_INODE 512   // an intermediate page-tree node above a page (gomupdf_page_nodes)
#define GM_DONE 1024   // a page-tree node gomupdf_page_nodes has counted or edited
#define GM_FUP 2048    // a field gomupdf_widget_fields has checked or edited
#define GM_ASAFE 4096  // an action gomupdf_action_walk decided safe
#define GM_ABAD 8192   // an action gomupdf_action_walk decided unsafe
#define GM_AREC 16384  // an action gomupdf_record_chain has recorded
#define GM_FWALK 32768 // a field gomupdf_fields has walked (a shared kid is walked once)
#define GM_PIYES 0x10000  // decided a page-piece dictionary (gomupdf_pieceinfo)
#define GM_PINO 0x20000   // decided not one
#define GM_PIWALK 0x40000 // walked by gomupdf_destroy_pieceinfo
#define GM_ONPAGE 0x80000 // an entry of a page-tree node's /Annots
#define GM_FORM 0x100000  // a node of the form (gomupdf_mark_form)
#define GM_FKIDS 0x200000 // an indirect array gomupdf_mark_form has expanded as /Kids
#define GM_OWALK 0x400000 // walked by gomupdf_orphan_page (gomupdf_orphan_closure)

enum { GS_WALK, GS_TOP, GS_DOWN, GS_UP, GS_LINK, GS_FORM, GS_ORPHAN, GS_COUNT };

// Pending objects of the marking walk; every entry holds a reference.
typedef struct { pdf_obj **v; size_t n, cap; } gomupdf_stack;

typedef struct {
    unsigned int *m; // marks, indexed by object number
    int len;          // entries in m
    int root;         // object number of the catalog, which Scrub edits; 0 if none
    int content;      // set while queuing from page-content roots (GM_CONTENT)
    int abudget;      // action budget of the current pass (gomupdf_action_walk)
    gomupdf_stack s[GS_COUNT];
    pdf_obj *cur;     // the entry being expanded, holding a reference
} gomupdf_marks;

// gomupdf_mark returns the marks of an indirect object, or NULL for a direct
// object, an object number outside the xref, or a NULL mk (HiddenContent).
static unsigned int *gomupdf_mark(fz_context *ctx, gomupdf_marks *mk, pdf_obj *ref) {
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

static void gomupdf_destroy(fz_context *ctx, gomupdf_marks *mk, pdf_obj *obj, int kind);
static void gomupdf_destroy_action(fz_context *ctx, gomupdf_marks *mk, pdf_obj *act);
static int gomupdf_is_kind(fz_context *ctx, pdf_obj *o, int kind);
static void gomupdf_sweep(fz_context *ctx, pdf_document *pdf, gomupdf_marks *mk, int *out, int keep_annots,
                          int keep_fields);
static pdf_obj *gomupdf_trailer_extra(fz_context *ctx, pdf_document *pdf, int *count);
static void gomupdf_record_annot(fz_context *ctx, gomupdf_marks *del, pdf_obj *e);
static int gomupdf_annot_shaped(fz_context *ctx, pdf_obj *ref);

// gomupdf_af_files returns how many entries of the array v are file
// specification dictionaries: an /AF with any is an associated-files array.
// Outside the catalog and pages Scrub removes only those entries (and the
// key, when every entry is one), and the marking skips only those entries,
// so a resource that happens to be named /AF (a colour space array, which
// never holds a filespec) is never touched.
static int gomupdf_af_files(fz_context *ctx, pdf_obj *v) {
    int n = pdf_array_len(ctx, v), files = 0;
    for (int i = 0; i < n; i++) files += gomupdf_is_kind(ctx, pdf_array_get(ctx, v, i), GK_FILESPEC);
    return files;
}
static void gomupdf_page_nodes(fz_context *ctx, gomupdf_marks *mk, pdf_obj *page, unsigned int seen,
                               int *out, gomupdf_marks *ax);

// gomupdf_action_budget is the action budget of one HiddenContent call or
// Scrub pass: GOMUPDF_ACTION_FLOOR + GOMUPDF_ACTION_PER_OBJECT x (xref
// length), saturating at INT_MAX.
static int gomupdf_action_budget(fz_context *ctx, pdf_document *pdf) {
    long long b = GOMUPDF_ACTION_FLOOR + (long long)GOMUPDF_ACTION_PER_OBJECT * pdf_xref_len(ctx, pdf);
    return b > INT_MAX ? INT_MAX : (int)b;
}

// gomupdf_js_scheme reports whether the n bytes at s start with "javascript:"
// in any case, after a UTF-8 byte-order mark and bytes <= 0x20 or 0x7f, and
// ignoring tab, CR and LF anywhere, as browsers parse a URL scheme.
static int gomupdf_js_scheme(const char *s, size_t n) {
    static const char want[] = "javascript:";
    size_t i = 0, k = 0;
    if (n >= 3 && !memcmp(s, "\xEF\xBB\xBF", 3)) i = 3;
    while (i < n && ((unsigned char)s[i] <= 0x20 || s[i] == 0x7f)) i++;
    for (; i < n && k < sizeof want - 1; i++) {
        unsigned char c = (unsigned char)s[i];
        if (c == '\t' || c == '\r' || c == '\n') continue;
        if ((c >= 'A' && c <= 'Z' ? c + 32 : c) != (unsigned char)want[k]) return 0;
        k++;
    }
    return k == sizeof want - 1;
}

// gomupdf_is_script_action reports whether the action dictionary act runs a
// script or a program: /S /JavaScript or /S /Launch, a /JS entry whatever its
// /S (a Rendition action's, say), or a URI action whose /URI is a
// javascript: URI (gomupdf_js_scheme), as raw bytes or as the text MuPDF
// decodes from it (UTF-16 or UTF-8 with a byte-order mark).
static int gomupdf_is_script_action(fz_context *ctx, pdf_obj *act) {
    pdf_obj *s = pdf_dict_get(ctx, act, PDF_NAME(S));
    if (pdf_name_eq(ctx, s, PDF_NAME(JavaScript)) || pdf_name_eq(ctx, s, PDF_NAME(Launch))
        || pdf_dict_get(ctx, act, PDF_NAME(JS)))
        return 1;
    if (!pdf_name_eq(ctx, s, PDF_NAME(URI))) return 0;
    pdf_obj *uri = pdf_dict_get(ctx, act, PDF_NAME(URI));
    size_t n = 0;
    const char *raw = pdf_to_string(ctx, uri, &n);
    if (gomupdf_js_scheme(raw, n)) return 1;
    const char *text = pdf_to_text_string(ctx, uri); // borrowed, cached in the object
    return gomupdf_js_scheme(text, strlen(text));
}

// gomupdf_action_walk reports whether act (an action dictionary, or an array
// of them as /Next allows) is, or chains through /Next to, an action that
// runs a script or a program (gomupdf_is_script_action; called a JavaScript
// or Launch action elsewhere in this file). Every reach costs one unit of
// ax->abudget. An indirect action's verdict is cached in ax, so it is walked
// once; a chain deeper than the depth cap (a cycle, say) or cut by the budget
// counts, and is cached, as unsafe, so a safe chain is cached as safe only if
// it was walked in full. A NULL act (no such key) costs nothing.
static int gomupdf_action_walk(fz_context *ctx, pdf_obj *act, int depth, gomupdf_marks *ax) {
    if (!act) return 0;
    if (--ax->abudget < 0) return 1;
    unsigned int *m = gomupdf_mark(ctx, ax, act);
    if (m && (*m & (GM_ASAFE | GM_ABAD))) return (*m & GM_ABAD) != 0;
    int bad = depth > GOMUPDF_MAX_DEPTH;
    if (!bad && pdf_is_array(ctx, act)) {
        int n = pdf_array_len(ctx, act);
        for (int i = 0; i < n && !bad; i++)
            bad = gomupdf_action_walk(ctx, pdf_array_get(ctx, act, i), depth + 1, ax);
    } else if (!bad && pdf_is_dict(ctx, act)) {
        bad = gomupdf_is_script_action(ctx, act)
            || gomupdf_action_walk(ctx, pdf_dict_get(ctx, act, PDF_NAME(Next)), depth + 1, ax);
    }
    if (m) *m |= bad ? GM_ABAD : GM_ASAFE;
    return bad;
}

// gomupdf_record_chain records for destruction the action tree act, which
// Scrub removes because it holds a script action: every action dictionary in
// it (GK_ANYACTION, so a script nested directly in a GoTo goes with it) and
// each script action's payload (gomupdf_destroy_action). Each indirect
// object is recorded once (GM_AREC), every reach costs one unit of
// ax->abudget, and it stops at the depth cap; what it does not reach is only
// unlinked.
static void gomupdf_record_chain(fz_context *ctx, pdf_obj *act, int depth, gomupdf_marks *del,
                                 gomupdf_marks *ax) {
    unsigned int *m = gomupdf_mark(ctx, del, act);
    if (!act || depth > GOMUPDF_MAX_DEPTH || (m && (*m & GM_AREC)) || --ax->abudget < 0) return;
    if (m) *m |= GM_AREC;
    int n = pdf_array_len(ctx, act);
    for (int i = 0; i < n; i++) gomupdf_record_chain(ctx, pdf_array_get(ctx, act, i), depth + 1, del, ax);
    if (!pdf_is_dict(ctx, act)) return;
    gomupdf_destroy(ctx, del, act, GK_ANYACTION);
    gomupdf_destroy_action(ctx, del, act);
    gomupdf_record_chain(ctx, pdf_dict_get(ctx, act, PDF_NAME(Next)), depth + 1, del, ax);
}

// gomupdf_action_unsafe decides act (gomupdf_action_walk) and, when it is
// unsafe and del is given, records its whole tree (gomupdf_record_chain).
static int gomupdf_action_unsafe(fz_context *ctx, pdf_obj *act, gomupdf_marks *ax, gomupdf_marks *del) {
    int bad = gomupdf_action_walk(ctx, act, 0, ax);
    if (bad && del) gomupdf_record_chain(ctx, act, 0, del, ax);
    return bad;
}

// gomupdf_open_action_unsafe: an array /OpenAction is a destination and is
// harmless; a dictionary is an action and is walked.
static int gomupdf_open_action_unsafe(fz_context *ctx, pdf_obj *root, gomupdf_marks *ax, gomupdf_marks *del) {
    pdf_obj *oa = pdf_dict_gets(ctx, root, "OpenAction");
    return pdf_is_dict(ctx, oa) && gomupdf_action_unsafe(ctx, oa, ax, del);
}

// gomupdf_aa_unsafe reports whether any action in an /AA (additional-actions)
// dictionary is unsafe, charging ax's action budget; with del it walks them
// all and records the unsafe ones.
static int gomupdf_aa_unsafe(fz_context *ctx, pdf_obj *aa, gomupdf_marks *ax, gomupdf_marks *del) {
    int bad = 0, n = pdf_dict_len(ctx, aa);
    for (int i = 0; i < n && !(bad && !del); i++)
        bad |= gomupdf_action_unsafe(ctx, pdf_dict_get_val(ctx, aa, i), ax, del);
    return bad;
}

// gomupdf_actions reports whether the annotation or field o has a JavaScript
// or Launch action in its /AA, /A or /PA. With mk it walks them all and
// records each such action for destruction and, with strip, deletes /AA and
// each such /A and /PA from o, which the caller has made safe to edit in
// place. A safe /A or /PA (a URI, a GoTo) stays.
static int gomupdf_actions(fz_context *ctx, gomupdf_marks *mk, pdf_obj *o, int strip, gomupdf_marks *ax) {
    static const char *keys[] = {"A", "PA"}; // PA is not in MuPDF's name table
    int bad = gomupdf_aa_unsafe(ctx, pdf_dict_get(ctx, o, PDF_NAME(AA)), ax, mk);
    if (strip) pdf_dict_del(ctx, o, PDF_NAME(AA));
    for (int k = 0; k < 2 && !(bad && !mk); k++)
        if (gomupdf_action_unsafe(ctx, pdf_dict_gets(ctx, o, keys[k]), ax, mk)) {
            bad = 1;
            if (strip) pdf_dict_dels(ctx, o, keys[k]);
        }
    return bad;
}

// gomupdf_widget_fields applies gomupdf_actions (with del and strip) to the
// fields above widget w, up /Parent: a field that /AcroForm /Fields leaves out
// still gives its widgets its /AA. It climbs at most GOMUPDF_MAX_DEPTH levels
// and stops at a field already visited (GM_FUP in ax); with strip it leaves
// a field that is page content as it is. Returns whether any field has a
// JavaScript or Launch action, or the climb stopped at the depth cap.
static int gomupdf_widget_fields(fz_context *ctx, gomupdf_marks *ax, gomupdf_marks *del, pdf_obj *w,
                                 int strip) {
    int bad = 0, d = 0;
    pdf_obj *f = pdf_dict_get(ctx, w, PDF_NAME(Parent));
    for (; d < GOMUPDF_MAX_DEPTH && pdf_is_dict(ctx, f) && !(bad && !del); d++) {
        unsigned int *m = gomupdf_mark(ctx, ax, f);
        if (m && (*m & GM_FUP)) return bad;
        if (m) *m |= GM_FUP;
        if (!(strip && m && (*m & GM_CONTENT))) bad |= gomupdf_actions(ctx, del, f, strip, ax);
        f = pdf_dict_get(ctx, f, PDF_NAME(Parent));
    }
    return bad || (d == GOMUPDF_MAX_DEPTH && pdf_is_dict(ctx, f));
}

// gomupdf_in_content reports whether Scrub must leave obj as it is: obj is
// indirect and reached as page content (GM_CONTENT), or direct inside an
// object that is (in). A NULL mk (HiddenContent) never is.
static int gomupdf_in_content(fz_context *ctx, gomupdf_marks *mk, pdf_obj *obj, int in) {
    if (!pdf_is_indirect(ctx, obj)) return in;
    unsigned int *m = gomupdf_mark(ctx, mk, obj);
    return m && (*m & GM_CONTENT);
}

// gomupdf_keep_on marks obj kept and queues it on the WALK stack (keep all it
// references) or the TOP stack (a kept annotation or outline item). Each
// indirect object is queued once per stack; an object number outside the xref
// resolves to null and holds nothing. Outside the page-content walk, an
// annotation or field (gomupdf_annot_shaped) is kept only in its own right,
// as a kept annotation or form node (TOP), never because something kept
// refers to it (a Hide action's /T, say), so one the options remove is still
// destroyed and the reference resolves to null.
static void gomupdf_keep_on(fz_context *ctx, gomupdf_marks *mk, int which, pdf_obj *obj) {
    if (pdf_is_indirect(ctx, obj)) {
        unsigned int *m = gomupdf_mark(ctx, mk, obj);
        unsigned int queued = which == GS_TOP ? GM_TOP : GM_WALK;
        if (!m || (*m & queued)) return;
        if (which == GS_WALK && !mk->content && !(*m & GM_TOP) && gomupdf_annot_shaped(ctx, obj)) return;
        *m |= GM_KEEP | queued | (which == GS_WALK && mk->content ? GM_CONTENT : 0);
    } else if (!pdf_is_array(ctx, obj) && !pdf_is_dict(ctx, obj)) {
        return;
    }
    gomupdf_push(ctx, mk, which, obj);
}
#define gomupdf_keep(ctx, mk, obj) gomupdf_keep_on(ctx, mk, GS_WALK, obj)

// gomupdf_keep_value keeps the value v of key k (on stack which), except, when
// k is /AF and v an associated-files array (gomupdf_af_files), its file
// specifications, which Scrub removes everywhere.
static void gomupdf_keep_value(fz_context *ctx, gomupdf_marks *mk, int which, pdf_obj *k, pdf_obj *v) {
    if (!pdf_name_eq(ctx, k, PDF_NAME(AF)) || !gomupdf_af_files(ctx, v)) {
        gomupdf_keep_on(ctx, mk, which, v);
        return;
    }
    int n = pdf_array_len(ctx, v);
    for (int i = 0; i < n; i++) {
        pdf_obj *e = pdf_array_get(ctx, v, i);
        if (!gomupdf_is_kind(ctx, e, GK_FILESPEC)) gomupdf_keep_on(ctx, mk, which, e);
    }
}

// gomupdf_keep_tops expands the kept annotations, outline items and (with
// KeepFormFields or KeepAnnotations) the /AcroForm dictionary and its field
// tree: everything they reference is kept except through their own /Parent,
// /P, /Popup, /AA, /SE, /XFA and an associated-files /AF, which lead up a
// tree, to another annotation, to actions, XFA streams or files Scrub
// removes or into the structure tree, and except an /A or /PA that is or
// chains to a JavaScript or Launch action, which Scrub removes (one that is
// also reached as content is walked in full as well). Outline siblings and
// children, /Fields, /Kids and /CO entries and an /IRT annotation are
// expanded the same way; an array among them has each entry expanded.
// The object is held while expanding it, as the action check resolves others.
static void gomupdf_keep_tops(fz_context *ctx, gomupdf_marks *mk) {
    static const char *skip[] = {"Parent", "P", "Popup", "AA", "SE", "XFA", NULL};
    static const char *items[] = {"First", "Last", "Next", "Prev", "Kids", "Fields", "CO", "IRT", NULL};
    gomupdf_stack *s = &mk->s[GS_TOP];
    while (s->n) {
        mk->cur = s->v[--s->n];
        pdf_obj *o = pdf_keep_obj(ctx, pdf_resolve_indirect(ctx, mk->cur));
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = o;
        int n = pdf_array_len(ctx, o);
        for (int i = 0; i < n; i++) gomupdf_keep_on(ctx, mk, GS_TOP, pdf_array_get(ctx, o, i));
        n = pdf_dict_len(ctx, o);
        for (int i = 0; i < n; i++) {
            pdf_obj *key = pdf_dict_get_key(ctx, o, i), *v = pdf_dict_get_val(ctx, o, i);
            const char *k = pdf_to_name(ctx, key);
            if (gomupdf_name_in(k, skip)) continue;
            if ((!strcmp(k, "A") || !strcmp(k, "PA")) && gomupdf_action_unsafe(ctx, v, mk, NULL))
                continue;
            gomupdf_keep_value(ctx, mk, gomupdf_name_in(k, items) ? GS_TOP : GS_WALK, key, v);
        }
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = NULL;
    }
}

// gomupdf_walk_skips reports whether the content walk passes over key k of a
// page or intermediate page-tree node Scrub edits (is_page) or the catalog
// (is_root). Only these objects, which Scrub edits, skip anything, and only
// what they lose to Scrub or what has its own role: on a page or node, the
// keys Scrub removes and /Annots (the annotations that stay are kept as
// such); on the catalog, the keys Scrub removes or edits, /Pages and
// /StructTreeRoot (which reaches removed annotations through /OBJR). Every
// other object, whatever other role it has, is walked through every key: in a
// resource subdictionary any name, /P or /Thumb included, can be a resource.
static int gomupdf_walk_skips(fz_context *ctx, int is_page, int is_root, pdf_obj *k) {
    static const char *page[] = {"Thumb", "AA", "AF", "Metadata", "PieceInfo", "LastModified", "Annots", NULL};
    static const char *root[] = {"Metadata", "AA", "AF", "PieceInfo", "LastModified", "AcroForm", "Outlines",
                                 "OpenAction", "Names", "Pages", "StructTreeRoot", "Perms", "DSS", "SpiderInfo",
                                 NULL};
    const char *name = pdf_to_name(ctx, k);
    return (is_page && gomupdf_name_in(name, page)) || (is_root && gomupdf_name_in(name, root));
}

// gomupdf_type_is reports whether o's /Type is the name type.
static int gomupdf_type_is(fz_context *ctx, pdf_obj *o, const char *type) {
    pdf_obj *t = pdf_dict_get(ctx, o, PDF_NAME(Type));
    return pdf_is_name(ctx, t) && !strcmp(pdf_to_name(ctx, t), type);
}

// gomupdf_page_shaped reports whether ref (an indirect object, so that a
// stream is told from a dictionary) is a page dictionary: one with /Type
// /Page or /Type /Template, or with no /Type, a /MediaBox and a /Contents or
// /Parent. A dictionary with /Kids is a page-tree node, and no other kind of
// object has /MediaBox, so this matches neither a page-tree node nor a
// non-page; whether it is a page of the document is up to the page-tree marks
// (GM_NODE).
static int gomupdf_page_shaped(fz_context *ctx, pdf_obj *ref) {
    if (!pdf_is_dict(ctx, ref) || pdf_is_stream(ctx, ref) || pdf_dict_get(ctx, ref, PDF_NAME(Kids))) return 0;
    if (pdf_dict_get(ctx, ref, PDF_NAME(Type)))
        return gomupdf_type_is(ctx, ref, "Page") || gomupdf_type_is(ctx, ref, "Template");
    return pdf_dict_get(ctx, ref, PDF_NAME(MediaBox))
        && (pdf_dict_get(ctx, ref, PDF_NAME(Contents)) || pdf_dict_get(ctx, ref, PDF_NAME(Parent)));
}

// gomupdf_private_holder reports whether ref (an indirect object) is one on
// which /Metadata, /PieceInfo and /LastModified have their documented
// meaning, besides the catalog and the page tree, which Scrub handles with
// their other keys: an image or form XObject, a font, font descriptor,
// structure element or output intent, an ICC profile stream (no /Type or
// /Subtype, an integer /N) or an embedded file. Resource dictionaries, where
// these names can be resource names, are none of these.
static int gomupdf_private_holder(fz_context *ctx, pdf_obj *ref) {
    pdf_obj *t = pdf_dict_get(ctx, ref, PDF_NAME(Type)), *st = pdf_dict_get(ctx, ref, PDF_NAME(Subtype));
    if (pdf_is_stream(ctx, ref)) {
        if (pdf_name_eq(ctx, st, PDF_NAME(Form)) || pdf_name_eq(ctx, st, PDF_NAME(Image)))
            return !t || pdf_name_eq(ctx, t, PDF_NAME(XObject));
        return pdf_name_eq(ctx, t, PDF_NAME(EmbeddedFile))
            || (!t && !st && pdf_is_int(ctx, pdf_dict_get(ctx, ref, PDF_NAME(N))));
    }
    return pdf_is_dict(ctx, ref)
        && (pdf_name_eq(ctx, t, PDF_NAME(Font)) || pdf_name_eq(ctx, t, PDF_NAME(FontDescriptor))
            || pdf_name_eq(ctx, t, PDF_NAME(OutputIntent)) || gomupdf_type_is(ctx, ref, "StructElem"));
}

// gomupdf_pieceinfo reports whether v is a page-piece dictionary
// (GK_PIECEINFO), caching the verdict on an indirect one in mk, so that one
// shared by many objects is checked once.
static int gomupdf_pieceinfo(fz_context *ctx, gomupdf_marks *mk, pdf_obj *v) {
    unsigned int *m = gomupdf_mark(ctx, mk, v);
    if (m && (*m & (GM_PIYES | GM_PINO))) return (*m & GM_PIYES) != 0;
    int yes = gomupdf_is_kind(ctx, v, GK_PIECEINFO);
    if (m) *m |= yes ? GM_PIYES : GM_PINO;
    return yes;
}

// gomupdf_private_key reports whether Scrub removes key name, holding v, from
// a private-data holder (gomupdf_private_holder): /Metadata when v is an XMP
// stream, /PieceInfo when v is a page-piece dictionary (gomupdf_pieceinfo),
// /LastModified when v is a string. The marking walk skips exactly these
// keys of a holder, so what they hold is not kept through it.
static int gomupdf_private_key(fz_context *ctx, gomupdf_marks *mk, const char *name, pdf_obj *v) {
    if (!strcmp(name, "Metadata")) return gomupdf_is_kind(ctx, v, GK_XMP);
    if (!strcmp(name, "LastModified")) return pdf_is_string(ctx, v);
    return !strcmp(name, "PieceInfo") && gomupdf_pieceinfo(ctx, mk, v);
}

// gomupdf_keep_walk keeps the transitive closure of the queued objects,
// iteratively: the visited marks bound the work by the size of the file. It
// follows every key, except those gomupdf_walk_skips names on pages, on
// intermediate page-tree nodes not reached as page content (which Scrub
// edits; one reached as content was queued with GM_CONTENT, before any other
// role, and is left as it is) and on the catalog, and the file
// specifications of an associated-files /AF on any object
// (gomupdf_keep_value), which Scrub removes everywhere. A page dictionary
// that is not a page MuPDF serves (an orphan page, which Scrub empties), reached
// other than as page content (a kept Link's destination, say), is kept but
// not expanded, so what it holds is not kept through it. On an indirect
// private-data holder (gomupdf_private_holder) it skips the keys Scrub
// removes there (gomupdf_private_key), which play no part in drawing. The
// object is held while expanding it, as those checks resolve others.
static void gomupdf_keep_walk(fz_context *ctx, gomupdf_marks *mk) {
    gomupdf_stack *s = &mk->s[GS_WALK];
    while (s->n) {
        mk->cur = s->v[--s->n];
        unsigned int *m = gomupdf_mark(ctx, mk, mk->cur);
        int is_page = m && ((*m & GM_PAGE) || (*m & (GM_INODE | GM_CONTENT)) == GM_INODE);
        int is_root = m && pdf_to_num(ctx, mk->cur) == mk->root;
        int orphan = m && !(*m & (GM_NODE | GM_CONTENT)) && gomupdf_page_shaped(ctx, mk->cur);
        int holder = m && gomupdf_private_holder(ctx, mk->cur);
        pdf_obj *o = pdf_keep_obj(ctx, pdf_resolve_indirect(ctx, mk->cur));
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = o;
        if (!orphan && pdf_is_array(ctx, o)) {
            int n = pdf_array_len(ctx, o);
            for (int i = 0; i < n; i++) gomupdf_keep(ctx, mk, pdf_array_get(ctx, o, i));
        } else if (!orphan && pdf_is_dict(ctx, o)) {
            int n = pdf_dict_len(ctx, o);
            for (int i = 0; i < n; i++) {
                pdf_obj *k = pdf_dict_get_key(ctx, o, i), *v = pdf_dict_get_val(ctx, o, i);
                if (gomupdf_walk_skips(ctx, is_page, is_root, k)
                    || (holder && gomupdf_private_key(ctx, mk, pdf_to_name(ctx, k), v)))
                    continue;
                gomupdf_keep_value(ctx, mk, GS_WALK, k, v);
            }
        }
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = NULL;
    }
}

// gomupdf_push_node marks node as a kept page-tree node and queues it on the
// DOWN or UP stack, once. A page dictionary that is not one of the pages
// MuPDF serves (GM_PAGE, set first: a page past a /Count that is too small,
// say) is not a page-tree node but an orphan page, and is not queued; the
// served pages are authoritative, so a page listed twice or under a damaged
// /Count is still a page as long as MuPDF serves it.
static void gomupdf_push_node(fz_context *ctx, gomupdf_marks *mk, int which, pdf_obj *node) {
    if (pdf_is_indirect(ctx, node)) {
        unsigned int *m = gomupdf_mark(ctx, mk, node);
        if (!m || (*m & GM_NODE)) return;
        if (!(*m & GM_PAGE) && gomupdf_page_shaped(ctx, node)) return;
        *m |= GM_NODE | GM_KEEP;
    } else if (!pdf_is_dict(ctx, node)) {
        return;
    }
    gomupdf_push(ctx, mk, which, node);
}

// gomupdf_push_entries queues the entries of a /Kids (DOWN) or /Annots (LINK,
// marking each GM_ONPAGE) array. An indirect array shared by many nodes is
// expanded only once in each role.
static void gomupdf_push_entries(fz_context *ctx, gomupdf_marks *mk, int which, pdf_obj *arr) {
    unsigned int *m = gomupdf_mark(ctx, mk, arr);
    unsigned int seen = which == GS_LINK ? GM_ANNOTS : GM_KIDS;
    if (m) {
        if (*m & seen) return;
        *m |= seen;
    }
    int n = pdf_array_len(ctx, arr);
    for (int i = 0; i < n; i++) {
        pdf_obj *e = pdf_array_get(ctx, arr, i);
        if (which == GS_LINK) {
            unsigned int *em = gomupdf_mark(ctx, mk, e);
            if (em) *em |= GM_ONPAGE;
            gomupdf_push(ctx, mk, GS_LINK, e);
        } else {
            gomupdf_push_node(ctx, mk, which, e);
        }
    }
}

// gomupdf_keep_nodes expands the queued page-tree nodes: it keeps what
// rendering takes from a node (/Contents, /Resources, which the pages below a
// /Pages node inherit, and /Group), queues the /Annots entries of a page
// (GM_PAGE, or a direct page dictionary, which cannot be marked) as candidate
// kept annotations, and queues /Parent (up) and, on the DOWN stack, /Kids.
// The /Annots of any other node are on no page MuPDF shows, and are left to
// gomupdf_sweep as annotations on no page. The node is held while expanding
// it, as pdf_array_len below resolves other objects.
static void gomupdf_keep_nodes(fz_context *ctx, gomupdf_marks *mk, int which) {
    gomupdf_stack *s = &mk->s[which];
    while (s->n) {
        mk->cur = s->v[--s->n];
        unsigned int *m = gomupdf_mark(ctx, mk, mk->cur);
        int page = m ? (*m & GM_PAGE) != 0 : gomupdf_page_shaped(ctx, mk->cur);
        pdf_obj *o = pdf_keep_obj(ctx, pdf_resolve_indirect(ctx, mk->cur));
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = o;
        if (pdf_is_dict(ctx, o)) {
            gomupdf_keep(ctx, mk, pdf_dict_get(ctx, o, PDF_NAME(Contents)));
            gomupdf_keep(ctx, mk, pdf_dict_get(ctx, o, PDF_NAME(Resources)));
            gomupdf_keep(ctx, mk, pdf_dict_get(ctx, o, PDF_NAME(Group)));
            if (page) gomupdf_push_entries(ctx, mk, GS_LINK, pdf_dict_get(ctx, o, PDF_NAME(Annots)));
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

// gomupdf_annot_kept reports whether the /Annots entry a stays: a Link always,
// a widget with keep_fields, anything with keep_annots.
static int gomupdf_annot_kept(fz_context *ctx, pdf_obj *a, int keep_annots, int keep_fields) {
    return keep_annots || gomupdf_is_link(ctx, a)
        || (keep_fields && pdf_name_eq(ctx, pdf_dict_get(ctx, a, PDF_NAME(Subtype)), PDF_NAME(Widget)));
}

// gomupdf_form_push queues obj for gomupdf_mark_form, once, and reports
// whether it was queued.
static int gomupdf_form_push(fz_context *ctx, gomupdf_marks *mk, pdf_obj *obj) {
    unsigned int *m = gomupdf_mark(ctx, mk, obj);
    if (m) {
        if (*m & GM_FORM) return 0;
        *m |= GM_FORM;
    } else if (!pdf_is_dict(ctx, obj)) {
        return 0;
    }
    gomupdf_push(ctx, mk, GS_FORM, obj);
    return 1;
}

// gomupdf_mark_form marks the form (GM_FORM): every node reached down /Kids
// from the /AcroForm /Fields entries and from the fields above each widget
// on the LINK stack (the /Annots entries of the page tree), up /Parent at
// most GOMUPDF_MAX_DEPTH levels, which reaches fields /Fields leaves out and
// their other kids. Each indirect node and indirect /Kids array is expanded
// once, so the work is bounded by the size of the file, and the walk is
// iterative. The node is held while expanding it, as reading /Kids resolves
// others.
static void gomupdf_mark_form(fz_context *ctx, pdf_document *pdf, gomupdf_marks *mk) {
    pdf_obj *fields = pdf_dict_getp(ctx, pdf_dict_get(ctx, pdf_trailer(ctx, pdf), PDF_NAME(Root)), "AcroForm/Fields");
    for (int i = 0; i < pdf_array_len(ctx, fields); i++) gomupdf_form_push(ctx, mk, pdf_array_get(ctx, fields, i));
    gomupdf_stack *links = &mk->s[GS_LINK];
    for (size_t i = 0; i < links->n; i++) {
        pdf_obj *w = links->v[i];
        if (!pdf_name_eq(ctx, pdf_dict_get(ctx, w, PDF_NAME(Subtype)), PDF_NAME(Widget))) continue;
        pdf_obj *f = pdf_dict_get(ctx, w, PDF_NAME(Parent));
        for (int d = 0; d < GOMUPDF_MAX_DEPTH && gomupdf_form_push(ctx, mk, f); d++)
            f = pdf_dict_get(ctx, f, PDF_NAME(Parent));
    }
    gomupdf_stack *s = &mk->s[GS_FORM];
    while (s->n) {
        mk->cur = s->v[--s->n];
        pdf_obj *kids = pdf_dict_get(ctx, mk->cur, PDF_NAME(Kids));
        unsigned int *m = gomupdf_mark(ctx, mk, kids);
        if (!m || !(*m & GM_FKIDS)) {
            if (m) *m |= GM_FKIDS;
            for (int i = 0; i < pdf_array_len(ctx, kids); i++) gomupdf_form_push(ctx, mk, pdf_array_get(ctx, kids, i));
        }
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = NULL;
    }
}

// gomupdf_mark_tree marks the page tree (GM_NODE): the catalog, every node
// from /Root /Pages down /Kids except page dictionaries MuPDF does not serve,
// every page MuPDF serves (also GM_PAGE) and the nodes up /Parent from each
// (GM_INODE between a page and the catalog). It keeps what rendering takes
// from each node (gomupdf_keep_nodes, which also queues the pages' /Annots
// entries on the LINK stack, GM_ONPAGE), then marks the form
// (gomupdf_mark_form). Scrub's marking pass calls it with
// mk->content set and walks those closures; HiddenContent only reads the
// marks.
static void gomupdf_mark_tree(fz_context *ctx, pdf_document *pdf, gomupdf_marks *mk) {
    pdf_obj *root = pdf_dict_get(ctx, pdf_trailer(ctx, pdf), PDF_NAME(Root));
    unsigned int *m = gomupdf_mark(ctx, mk, root);
    if (m) *m |= GM_NODE | GM_KEEP;
    mk->root = m ? pdf_to_num(ctx, root) : 0;
    // The served pages first: gomupdf_push_node tells the other page
    // dictionaries (orphan pages) by them.
    int npages = pdf_count_pages(ctx, pdf);
    for (int p = 0; p < npages; p++)
        if ((m = gomupdf_mark(ctx, mk, pdf_lookup_page_obj(ctx, pdf, p)))) *m |= GM_PAGE;
    gomupdf_push_node(ctx, mk, GS_DOWN, pdf_dict_get(ctx, root, PDF_NAME(Pages)));
    gomupdf_keep_nodes(ctx, mk, GS_DOWN);
    for (int p = 0; p < npages; p++) gomupdf_push_node(ctx, mk, GS_UP, pdf_lookup_page_obj(ctx, pdf, p));
    // Every page is GM_PAGE before the nodes above them are marked.
    for (int p = 0; p < npages; p++)
        gomupdf_page_nodes(ctx, mk, pdf_lookup_page_obj(ctx, pdf, p), GM_INODE, NULL, NULL);
    gomupdf_keep_nodes(ctx, mk, GS_UP);
    gomupdf_mark_form(ctx, pdf, mk);
}

// gomupdf_mark_content marks the page tree and the form (gomupdf_mark_tree)
// and, as page content (GM_CONTENT), the closures of the page-tree
// /Contents, /Resources and /Group, of /Encrypt and of /OCProperties. Scrub's
// marking pass starts with it, and HiddenContent runs it so that it skips
// what Scrub leaves as page content.
static void gomupdf_mark_content(fz_context *ctx, pdf_document *pdf, gomupdf_marks *mk) {
    pdf_obj *trailer = pdf_trailer(ctx, pdf);
    mk->content = 1;
    gomupdf_mark_tree(ctx, pdf, mk);
    gomupdf_keep(ctx, mk, pdf_dict_get(ctx, trailer, PDF_NAME(Encrypt)));
    gomupdf_keep(ctx, mk, pdf_dict_getp(ctx, trailer, "Root/OCProperties"));
    gomupdf_keep_walk(ctx, mk);
    mk->content = 0;
}

// gomupdf_keep_pass marks the kept content of the document (see GM_KEEP).
// keep_fields (KeepFormFields, or KeepAnnotations) keeps the widgets and the
// /AcroForm field tree.
static void gomupdf_keep_pass(fz_context *ctx, pdf_document *pdf, gomupdf_marks *mk,
                              int keep_annots, int keep_fields, int keep_outlines) {
    pdf_obj *root = pdf_dict_get(ctx, pdf_trailer(ctx, pdf), PDF_NAME(Root));
    mk->abudget = gomupdf_action_budget(ctx, pdf);
    gomupdf_mark_content(ctx, pdf, mk);

    // A safe /OpenAction stays, so its actions are kept like a kept /A: a
    // removed script chain that shares one of them only unlinks it.
    if (!gomupdf_open_action_unsafe(ctx, root, mk, NULL))
        gomupdf_keep(ctx, mk, pdf_dict_gets(ctx, root, "OpenAction"));
    // A kept form keeps the signature dictionaries and the security store its
    // signature fields may rely on.
    if (keep_fields) {
        gomupdf_keep(ctx, mk, pdf_dict_get(ctx, root, PDF_NAME(Perms)));
        gomupdf_keep(ctx, mk, pdf_dict_gets(ctx, root, "DSS"));
    }

    // Kept annotations: Links, widgets with keep_fields, or every /Annots
    // entry with keep_annots.
    gomupdf_stack *links = &mk->s[GS_LINK];
    while (links->n) {
        mk->cur = links->v[--links->n];
        if (gomupdf_annot_kept(ctx, mk->cur, keep_annots, keep_fields)) gomupdf_keep_on(ctx, mk, GS_TOP, mk->cur);
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = NULL;
    }
    if (keep_outlines) gomupdf_keep_on(ctx, mk, GS_TOP, pdf_dict_get(ctx, root, PDF_NAME(Outlines)));
    if (keep_fields) gomupdf_keep_on(ctx, mk, GS_TOP, pdf_dict_get(ctx, root, PDF_NAME(AcroForm)));
    gomupdf_keep_tops(ctx, mk);
    gomupdf_keep_walk(ctx, mk);
}

// gomupdf_load_all loads every object in the xref. MuPDF repairs (rebuilds)
// a broken xref at most once per document, and only while loading an object,
// so a repair any object needs happens here, before Scrub marks or changes
// anything, and none can happen later. An object that fails to load is left
// to fail again where it is used. Loading caches the object in the xref, so
// there is no reference to drop.
//
// An object in an object stream that fails to load marks that stream: its
// other objects are not tried here, since each try parses the stream again
// (a crafted stream with 100000 entries took 23s). They would fail the same
// way, without a repair (MuPDF repairs for a missing member only after the
// stream parsed, and that try already happened), and fail or load later
// where used.
//
// A repair rebuilds the xref and forgets the objects already loaded, so a
// pass during which one happened is run again (there is no second repair).
// Afterwards, an entry that holds no object (gomupdf_unloaded) is one that
// cannot be loaded, or holds nothing: the sweeps skip it rather than parse
// it again.
static void gomupdf_load_pass(fz_context *ctx, pdf_document *pdf);
static void gomupdf_load_all(fz_context *ctx, pdf_document *pdf) {
    for (int pass = 0; pass < 2; pass++) {
        int repaired = pdf->repair_attempted;
        gomupdf_load_pass(ctx, pdf);
        if (pdf->repair_attempted == repaired) return;
    }
}

// gomupdf_unloaded reports whether xref entry i holds no object: a free or
// missing entry, an object that is null, or one that failed to load in
// gomupdf_load_all (a member of an object stream that does not parse, say).
static int gomupdf_unloaded(fz_context *ctx, pdf_document *pdf, int i) {
    return !pdf_get_xref_entry_no_null(ctx, pdf, i)->obj;
}

static void gomupdf_load_pass(fz_context *ctx, pdf_document *pdf) {
    unsigned char *bad = NULL; // bad[n]: object stream n failed
    int nbad = 0;
    fz_var(bad);
    fz_var(nbad);
    fz_try(ctx) {
        for (int i = 1; i < pdf_xref_len(ctx, pdf); i++) {
            int stm = 0;
            fz_var(stm);
            fz_try(ctx) {
                pdf_xref_entry *x = pdf_get_xref_entry_no_null(ctx, pdf, i);
                // In an object stream, ofs is the stream's object number.
                stm = x->type == 'o' && x->ofs > 0 && x->ofs < INT_MAX ? (int)x->ofs : 0;
                if (!(stm > 0 && stm < nbad && bad[stm]))
                    pdf_cache_object(ctx, pdf, i);
            }
            fz_catch(ctx) {
                fz_rethrow_if(ctx, FZ_ERROR_SYSTEM);
                fz_ignore_error(ctx);
                if (stm > 0) {
                    if (!bad) {
                        nbad = pdf_xref_len(ctx, pdf);
                        bad = fz_calloc(ctx, nbad, 1);
                    }
                    if (stm < nbad) bad[stm] = 1;
                }
            }
        }
    }
    fz_always(ctx) fz_free(ctx, bad);
    fz_catch(ctx) fz_rethrow(ctx);
}

// gomupdf_stabilise runs gomupdf_load_all on a PDF (anything else has no
// xref to repair). Returns 0, or -1 with err set (only running out of memory
// fails).
static int gomupdf_stabilise(fz_context *ctx, fz_document *doc, char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) return 0;
    fz_try(ctx) gomupdf_load_all(ctx, pdf);
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        return -1;
    }
    return 0;
}

static int gomupdf_repair_attempted(fz_context *ctx, fz_document *doc) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    return pdf && pdf->repair_attempted;
}

// gomupdf_keep_all allocates the marks and runs gomupdf_keep_pass. A broken
// object can make MuPDF repair (rebuild) the xref while the pass loads it, so
// a pass during which that happened is redone on the repaired xref (a
// backstop: gomupdf_load_all makes any repair happen before). Returns
// pdf->repair_attempted as the marks saw it.
static int gomupdf_keep_all(fz_context *ctx, pdf_document *pdf, gomupdf_marks *mk,
                            int keep_annots, int keep_fields, int keep_outlines) {
    for (int attempt = 0;; attempt++) {
        int repaired = pdf->repair_attempted;
        size_t len = (size_t)pdf_xref_len(ctx, pdf);
        fz_free(ctx, mk->m);
        mk->m = NULL;
        mk->len = 0;
        mk->m = fz_calloc(ctx, len ? len : 1, sizeof *mk->m);
        mk->len = (int)len;
        gomupdf_keep_pass(ctx, pdf, mk, keep_annots, keep_fields, keep_outlines);
        if (pdf->repair_attempted == repaired) return repaired;
        if (attempt) fz_throw(ctx, FZ_ERROR_FORMAT, "xref repaired while marking kept content");
    }
}

static int gomupdf_is_kind(fz_context *ctx, pdf_obj *o, int kind) {
#define GK_HAS(k) (pdf_dict_get(ctx, o, PDF_NAME(k)) != NULL)
#define GK_IS(v, k) pdf_name_eq(ctx, v, PDF_NAME(k))
    pdf_obj *t = pdf_dict_get(ctx, o, PDF_NAME(Type));
    pdf_obj *st = pdf_dict_get(ctx, o, PDF_NAME(Subtype));
    int stream = pdf_is_stream(ctx, o), dict = !stream && pdf_is_dict(ctx, o);
    if (GK_IS(st, Link)) return 0;
    switch (kind) {
    case GK_INFO: return dict && (!t || GK_IS(t, Info)); // FOP writes /Type /Info
    case GK_EF: case GK_LAUNCHP: return dict && !t;
    case GK_ACROFORM: return dict && !t && (GK_HAS(Fields) || GK_HAS(XFA));
    case GK_TREE: return dict && !t && (GK_HAS(Names) || GK_HAS(Kids));
    case GK_FILESPEC:
        return dict && (GK_IS(t, Filespec) || (!t && (GK_HAS(EF) || GK_HAS(F) || GK_HAS(UF))));
    case GK_ACTION: return dict && (!t || GK_IS(t, Action)) && gomupdf_is_script_action(ctx, o);
    case GK_ANYACTION: return dict && (!t || GK_IS(t, Action)) && pdf_is_name(ctx, pdf_dict_get(ctx, o, PDF_NAME(S)));
    case GK_ANNOT: return dict && (!t || GK_IS(t, Annot)) && st;
    case GK_FIELD:
        return dict && (!t || GK_IS(t, Annot))
            && (GK_HAS(FT) || GK_HAS(T) || GK_HAS(Kids) || GK_IS(st, Widget));
    case GK_XMP: return stream && (GK_IS(t, Metadata) || GK_IS(st, XML));
    case GK_EMBEDDED:
        return stream && (GK_IS(t, EmbeddedFile)
            || (!t && !GK_IS(st, Form) && !GK_IS(st, Image) && !GK_IS(st, PS)));
    case GK_SCRIPT: return pdf_is_string(ctx, o) || (stream && !t && !st);
    case GK_STRING: return pdf_is_string(ctx, o);
    case GK_XFA: case GK_CONTENTS: return stream && !t && !st;
    case GK_XOBJECT: return stream && (!t || GK_IS(t, XObject)) && (GK_IS(st, Form) || GK_IS(st, Image));
    case GK_PRIVATE: return !t && (dict || (stream && !st));
    case GK_SIG: return dict && (GK_IS(t, Sig) || gomupdf_type_is(ctx, o, "DocTimeStamp"));
    case GK_DSS: return dict && (!t || gomupdf_type_is(ctx, o, "DSS"));
    case GK_PIECEINFO: {
        // An untyped dictionary whose every value is a data dictionary
        // (/LastModified or /Private).
        int n = pdf_dict_len(ctx, o);
        if (!dict || t || !n) return 0;
        for (int i = 0; i < n; i++) {
            pdf_obj *d = pdf_dict_get_val(ctx, o, i);
            if (!pdf_is_dict(ctx, d) || pdf_is_stream(ctx, d)
                || !(pdf_dict_gets(ctx, d, "LastModified") || pdf_dict_get(ctx, d, PDF_NAME(Private))))
                return 0;
        }
        return 1;
    }
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
    unsigned int *m = gomupdf_mark(ctx, mk, obj);
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

// gomupdf_walked reports whether obj is indirect and was already walked
// (bit: GM_PIWALK or GM_OWALK, one per walk), marking it walked.
static int gomupdf_walked(fz_context *ctx, gomupdf_marks *mk, pdf_obj *obj, unsigned int bit) {
    unsigned int *m = gomupdf_mark(ctx, mk, obj);
    if (!m) return 0;
    if (*m & bit) return 1;
    *m |= bit;
    return 0;
}

// gomupdf_destroy_pieceinfo records a removed /PieceInfo for destruction, if
// it is a page-piece dictionary (gomupdf_pieceinfo): the dictionary, each
// data dictionary in it, each one's /Private value (an untyped dictionary or
// stream) and, when that is a dictionary, the untyped streams it holds
// (Illustrator keeps its private copy of the artwork in such streams). Each
// indirect object is walked once (GM_PIWALK), so private data shared by many
// objects costs one walk.
static void gomupdf_destroy_pieceinfo(fz_context *ctx, gomupdf_marks *mk, pdf_obj *pi) {
    if (!mk || !gomupdf_pieceinfo(ctx, mk, pi) || gomupdf_walked(ctx, mk, pi, GM_PIWALK)) return;
    gomupdf_destroy(ctx, mk, pi, GK_PIECEINFO);
    for (int i = 0; i < pdf_dict_len(ctx, pi); i++) {
        pdf_obj *d = pdf_dict_get_val(ctx, pi, i);
        if (gomupdf_walked(ctx, mk, d, GM_PIWALK)) continue;
        gomupdf_destroy(ctx, mk, d, GK_EF);
        pdf_obj *p = pdf_dict_get(ctx, d, PDF_NAME(Private));
        if (gomupdf_walked(ctx, mk, p, GM_PIWALK) || !gomupdf_is_kind(ctx, p, GK_PRIVATE)) continue;
        gomupdf_destroy(ctx, mk, p, GK_PRIVATE);
        if (pdf_is_stream(ctx, p)) continue;
        for (int j = 0; j < pdf_dict_len(ctx, p); j++)
            gomupdf_destroy(ctx, mk, pdf_dict_get_val(ctx, p, j), GK_CONTENTS);
    }
}

// gomupdf_private_keys counts into out (HiddenContent) or removes (Scrub, mk)
// the keys gomupdf_private_key names on the private-data holder o, recording
// the XMP streams and page-piece data for destruction. It edits o in place
// even when o is page content (an image the page draws): these keys play no
// part in drawing, and the value checks keep a resource of the same name out
// of reach.
static void gomupdf_private_keys(fz_context *ctx, gomupdf_marks *mk, pdf_obj *o, int *out) {
    static const char *keys[] = {"Metadata", "PieceInfo", "LastModified"};
    static const int counts[] = {HC_OBJMETA, HC_PIECEINFO, HC_LASTMOD};
    for (int k = 0; k < 3; k++) {
        pdf_obj *v = pdf_dict_gets(ctx, o, keys[k]);
        if (!v || !gomupdf_private_key(ctx, mk, keys[k], v)) continue;
        if (out) {
            gomupdf_add(&out[counts[k]], 1);
            continue;
        }
        if (k == 0) gomupdf_destroy(ctx, mk, v, GK_XMP);
        if (k == 1) gomupdf_destroy_pieceinfo(ctx, mk, v);
        pdf_dict_dels(ctx, o, keys[k]);
    }
}

// gomupdf_destroy_signatures records the catalog's /Perms and /DSS for
// destruction: the /Perms dictionary and its signature dictionaries (/Type
// /Sig or /DocTimeStamp), and the /DSS dictionary with the untyped streams of
// its /Certs, /OCSPs and /CRLs arrays. The /VRI entries are only unlinked;
// they normally list the same streams.
static void gomupdf_destroy_signatures(fz_context *ctx, gomupdf_marks *mk, pdf_obj *root) {
    static const char *lists[] = {"Certs", "OCSPs", "CRLs"};
    pdf_obj *perms = pdf_dict_get(ctx, root, PDF_NAME(Perms)), *dss = pdf_dict_gets(ctx, root, "DSS");
    gomupdf_destroy(ctx, mk, perms, GK_EF);
    for (int i = 0; i < pdf_dict_len(ctx, perms); i++) gomupdf_destroy(ctx, mk, pdf_dict_get_val(ctx, perms, i), GK_SIG);
    if (!gomupdf_is_kind(ctx, dss, GK_DSS)) return;
    gomupdf_destroy(ctx, mk, dss, GK_DSS);
    for (int k = 0; k < 3; k++) {
        pdf_obj *a = pdf_dict_gets(ctx, dss, lists[k]);
        for (int i = 0; i < pdf_array_len(ctx, a); i++) gomupdf_destroy(ctx, mk, pdf_array_get(ctx, a, i), GK_CONTENTS);
    }
}

// gomupdf_destroy_spiderset records a web-capture content set (a
// /Names/IDS or /Names/URLS value, /Type /SpiderContentSet) for destruction
// with its untyped /SI source-information dictionaries (one, or an array of
// them), which hold the captured URLs. The pages and images its /O lists are
// only unlinked.
static void gomupdf_destroy_spiderset(fz_context *ctx, gomupdf_marks *mk, pdf_obj *set) {
    unsigned int *m = gomupdf_mark(ctx, mk, set);
    if (!m || (*m & GM_DEL)) return; // a set many names share is walked once
    *m |= GM_DEL; // the caller checked /Type /SpiderContentSet
    pdf_obj *si = pdf_dict_gets(ctx, set, "SI");
    gomupdf_destroy(ctx, mk, si, GK_EF);
    for (int i = 0; i < pdf_array_len(ctx, si); i++) gomupdf_destroy(ctx, mk, pdf_array_get(ctx, si, i), GK_EF);
}

// gomupdf_destroy_file records a removed file reference: a filespec, or a
// file-name string.
static void gomupdf_destroy_file(fz_context *ctx, gomupdf_marks *mk, pdf_obj *f) {
    gomupdf_destroy(ctx, mk, f, GK_STRING);
    gomupdf_destroy_filespec(ctx, mk, f);
}

// gomupdf_destroy_action records a removed script action (see
// gomupdf_is_script_action; a /Names/JavaScript value or one
// gomupdf_action_unsafe reaches): the action, its /JS script string or
// stream, its /URI string, its /F file and, for Launch, its /Win, /Mac and
// /Unix dictionaries with their /F files.
static void gomupdf_destroy_action(fz_context *ctx, gomupdf_marks *mk, pdf_obj *act) {
    static const char *platforms[] = {"Win", "Mac", "Unix"}; // Win is not in MuPDF's name table
    if (!mk || !gomupdf_is_kind(ctx, act, GK_ACTION)) return;
    gomupdf_destroy(ctx, mk, act, GK_ACTION);
    gomupdf_destroy(ctx, mk, pdf_dict_get(ctx, act, PDF_NAME(JS)), GK_SCRIPT);
    gomupdf_destroy(ctx, mk, pdf_dict_get(ctx, act, PDF_NAME(URI)), GK_STRING);
    gomupdf_destroy_file(ctx, mk, pdf_dict_get(ctx, act, PDF_NAME(F)));
    for (int i = 0; i < 3; i++) {
        pdf_obj *p = pdf_dict_gets(ctx, act, platforms[i]);
        gomupdf_destroy(ctx, mk, p, GK_LAUNCHP);
        gomupdf_destroy_file(ctx, mk, pdf_dict_get(ctx, p, PDF_NAME(F)));
    }
}

// gomupdf_destroy_xfa records the /AcroForm /XFA streams: one stream, or the
// streams of an array of name/stream pairs.
static void gomupdf_destroy_xfa(fz_context *ctx, gomupdf_marks *mk, pdf_obj *xfa) {
    gomupdf_destroy(ctx, mk, xfa, GK_XFA);
    int n = pdf_array_len(ctx, xfa);
    for (int i = 1; i < n; i += 2) gomupdf_destroy(ctx, mk, pdf_array_get(ctx, xfa, i), GK_XFA);
}

// gomupdf_destroy_all deletes every recorded object that is not kept: every
// reference to it that survives elsewhere (a StructElem /OBJR or /AF, say)
// then resolves to null, and a garbage-collecting save rewrites it as null.
// An object created since the file was opened (an annotation added through
// the API, a redaction annotation ApplyRedactions left behind) becomes, once
// deleted, an entry MuPDF says never existed, and MuPDF 1.25's save loads
// every entry first and fails on it; such an object becomes an empty
// dictionary instead (MuPDF cannot store a null object: pdf_update_object
// with null deletes), which holds nothing.
static void gomupdf_destroy_all(fz_context *ctx, pdf_document *pdf, gomupdf_marks *mk) {
    for (int i = 1; i < mk->len; i++) {
        if ((mk->m[i] & (GM_DEL | GM_KEEP)) != GM_DEL) continue;
        pdf_delete_object(ctx, pdf, i);
        if (pdf_get_xref_entry_no_null(ctx, pdf, i)->type != 0) continue;
        pdf_obj *empty = pdf_new_dict(ctx, pdf, 0);
        fz_try(ctx) pdf_update_object(ctx, pdf, i, empty);
        fz_always(ctx) pdf_drop_obj(ctx, empty);
        fz_catch(ctx) fz_rethrow(ctx);
    }
}

// gomupdf_name_tree counts the key/value pairs of a name tree, following
// /Kids, and records every node and value in del when not NULL: filespecs,
// or with js (the marks holding the action budget, for /Names/JavaScript)
// each value's action tree (gomupdf_action_unsafe). Each node, /Kids entry
// and /Names pair costs one unit of *budget. A node deeper than the depth cap
// or on a /Kids cycle, and everything past the budget, sets *incomplete and
// is skipped.
static long long gomupdf_name_tree(fz_context *ctx, pdf_obj *node, int depth, pdf_cycle_list *up,
                                   int *budget, int *incomplete, gomupdf_marks *del, gomupdf_marks *js) {
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
        if (js && del) gomupdf_action_unsafe(ctx, v, js, del);
        else if (gomupdf_type_is(ctx, v, "SpiderContentSet")) gomupdf_destroy_spiderset(ctx, del, v);
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

// gomupdf_destroy_name_tree records a whole name tree for destruction (js:
// /Names/JavaScript, whose actions charge del's action budget).
static void gomupdf_destroy_name_tree(fz_context *ctx, gomupdf_marks *del, pdf_obj *tree, int js) {
    int budget = GOMUPDF_TREE_BUDGET, incomplete = 0;
    gomupdf_name_tree(ctx, tree, 0, NULL, &budget, &incomplete, del, js ? del : NULL);
}

// gomupdf_fields walks the /AcroForm field tree below the array fields,
// following /Kids at most GOMUPDF_MAX_DEPTH levels deep and charging one unit
// of *budget per array entry; a node too deep or on a /Kids cycle is skipped,
// and everything past the budget, which makes the walk incomplete. An
// indirect node already walked (GM_FWALK in ax: a kid several parents list)
// is skipped, so a tree that shares its kids is walked once per node. Each
// node's actions charge ax (gomupdf_actions). Without mk (HiddenContent)
// it returns 1 as soon as a node has a JavaScript or Launch action or the walk
// is incomplete. With mk and !strip (Scrub removing /AcroForm) it records
// every node and those actions for destruction; with strip (Scrub keeping the
// fields) it removes /AA and those actions from every node that is not page
// content (in is the content flag of the array's container, see
// gomupdf_in_content), editing it in place.
static int gomupdf_fields(fz_context *ctx, gomupdf_marks *mk, int strip, pdf_obj *fields, int in,
                          int depth, pdf_cycle_list *up, int *budget, gomupdf_marks *ax) {
    int found = 0, n = pdf_array_len(ctx, fields);
    in = gomupdf_in_content(ctx, mk, fields, in);
    for (int i = 0; i < n && !(found && !mk); i++) {
        if (--*budget < 0) return 1;
        pdf_obj *f = pdf_array_get(ctx, fields, i);
        pdf_cycle_list here;
        if (!pdf_is_dict(ctx, f)) continue;
        if (depth > GOMUPDF_MAX_DEPTH || pdf_cycle(ctx, &here, up, f)) {
            found = 1;
            continue;
        }
        unsigned int *fm = gomupdf_mark(ctx, ax, f);
        if (fm && (*fm & GM_FWALK)) continue; // a kid shared by several parents
        if (fm) *fm |= GM_FWALK;
        int c = gomupdf_in_content(ctx, mk, f, in);
        if (!strip) gomupdf_destroy(ctx, mk, f, GK_FIELD);
        if (!(strip && c)) found |= gomupdf_actions(ctx, mk, f, strip, ax);
        found |= gomupdf_fields(ctx, mk, strip, pdf_dict_get(ctx, f, PDF_NAME(Kids)), c, depth + 1, &here,
                                budget, ax);
    }
    return found;
}

// gomupdf_outline walks the outline items below node (its /First item and
// that item's /Next siblings, then their children), at most GOMUPDF_MAX_DEPTH
// levels deep and charging one unit of *budget per item; a /First cycle, an
// item too deep or past the budget makes the walk incomplete (a /Next cycle
// runs until the budget does). Without mk (HiddenContent) it returns 1 as soon
// as an item's /A is or chains to a JavaScript or Launch action or the walk is
// incomplete. With mk (Scrub keeping outlines) it deletes such an /A from
// every item that is not page content (in: see gomupdf_fields), in place, and
// records its actions for destruction. /Dest is a destination and stays.
static int gomupdf_outline(fz_context *ctx, gomupdf_marks *mk, pdf_obj *node, int in, int depth,
                           pdf_cycle_list *up, int *budget, gomupdf_marks *ax) {
    pdf_cycle_list here;
    if (depth > GOMUPDF_MAX_DEPTH || pdf_cycle(ctx, &here, up, node)) return 1;
    int found = 0;
    for (pdf_obj *it = pdf_dict_get(ctx, node, PDF_NAME(First)); pdf_is_dict(ctx, it) && !(found && !mk);
         it = pdf_dict_get(ctx, it, PDF_NAME(Next))) {
        if (--*budget < 0) return 1;
        int c = gomupdf_in_content(ctx, mk, it, in);
        if (!c && gomupdf_action_unsafe(ctx, pdf_dict_get(ctx, it, PDF_NAME(A)), ax, mk)) {
            found = 1;
            if (mk) pdf_dict_del(ctx, it, PDF_NAME(A));
        }
        found |= gomupdf_outline(ctx, mk, it, c, depth + 1, &here, budget, ax);
    }
    return found;
}

// gomupdf_page_keys counts into out (HiddenContent) or removes (Scrub, mk) the
// keys Scrub removes from a page or intermediate page-tree node: /Thumb, /AA,
// /AF, /Metadata, /PieceInfo and /LastModified, recording what they hold for
// destruction.
static void gomupdf_page_keys(fz_context *ctx, gomupdf_marks *mk, pdf_obj *o, int *out, gomupdf_marks *ax) {
    pdf_obj *af = pdf_dict_get(ctx, o, PDF_NAME(AF)), *aa = pdf_dict_get(ctx, o, PDF_NAME(AA));
    if (out) {
        gomupdf_add(&out[HC_THUMBS], pdf_dict_get(ctx, o, PDF_NAME(Thumb)) != NULL);
        gomupdf_add(&out[HC_PAGEMETA], pdf_dict_get(ctx, o, PDF_NAME(Metadata)) != NULL);
        gomupdf_add(&out[HC_AF], af != NULL);
        gomupdf_add(&out[HC_PIECEINFO], pdf_dict_get(ctx, o, PDF_NAME(PieceInfo)) != NULL);
        gomupdf_add(&out[HC_LASTMOD], pdf_dict_gets(ctx, o, "LastModified") != NULL);
        if (!out[HC_JS] && gomupdf_aa_unsafe(ctx, aa, ax, NULL)) out[HC_JS] = 1;
        return;
    }
    gomupdf_destroy(ctx, mk, pdf_dict_get(ctx, o, PDF_NAME(Thumb)), GK_THUMB);
    gomupdf_destroy(ctx, mk, pdf_dict_get(ctx, o, PDF_NAME(Metadata)), GK_XMP);
    gomupdf_destroy_pieceinfo(ctx, mk, pdf_dict_get(ctx, o, PDF_NAME(PieceInfo)));
    for (int i = 0; i < pdf_array_len(ctx, af); i++)
        gomupdf_destroy_filespec(ctx, mk, pdf_array_get(ctx, af, i));
    gomupdf_aa_unsafe(ctx, aa, ax, mk);
    pdf_dict_del(ctx, o, PDF_NAME(Thumb));
    pdf_dict_del(ctx, o, PDF_NAME(AA));
    pdf_dict_del(ctx, o, PDF_NAME(AF));
    pdf_dict_del(ctx, o, PDF_NAME(Metadata));
    pdf_dict_del(ctx, o, PDF_NAME(PieceInfo));
    pdf_dict_dels(ctx, o, "LastModified");
}

// gomupdf_page_nodes walks up /Parent from page through the intermediate
// page-tree nodes above it, at most GOMUPDF_MAX_DEPTH of them, and stops at a
// direct object, a node already marked seen, a page (GM_PAGE) or the catalog,
// so the nodes above all pages cost one visit each. The marking pass (seen
// GM_INODE) only marks them; with out (HiddenContent) it counts their page
// keys; otherwise (Scrub, seen GM_DONE) it removes them from every node that
// is not page content.
static void gomupdf_page_nodes(fz_context *ctx, gomupdf_marks *mk, pdf_obj *page, unsigned int seen,
                               int *out, gomupdf_marks *ax) {
    pdf_obj *node = pdf_dict_get(ctx, page, PDF_NAME(Parent));
    for (int d = 0; d < GOMUPDF_MAX_DEPTH; d++) {
        unsigned int *m = gomupdf_mark(ctx, mk, node);
        if (!m || (*m & (seen | GM_PAGE)) || pdf_to_num(ctx, node) == mk->root || !pdf_is_dict(ctx, node))
            return;
        *m |= seen;
        if (out) gomupdf_page_keys(ctx, NULL, node, out, ax);
        else if (seen == GM_DONE && !(*m & GM_CONTENT)) gomupdf_page_keys(ctx, mk, node, NULL, ax);
        node = pdf_dict_get(ctx, node, PDF_NAME(Parent));
    }
}

static int gomupdf_hidden_content(fz_context *ctx, fz_document *doc, int *out,
                                  char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    for (int i = 0; i < HC_COUNT; i++) out[i] = 0;
    // Marks for the walks up /Parent and the action and field walks: no
    // kept-content marking.
    gomupdf_marks mk;
    memset(&mk, 0, sizeof mk);
    fz_var(mk);
    int failed = 0;
    fz_try(ctx) {
        // Load every object first, as Scrub does: the /AF sweep reads them
        // all, and a repair MuPDF needs must happen before anything is read
        // (so the counts describe the repaired document, page count
        // included) and never while an array taken from the old xref is in
        // use.
        gomupdf_load_all(ctx, pdf);
        pdf_obj *trailer = pdf_trailer(ctx, pdf);
        pdf_obj *root = pdf_dict_get(ctx, trailer, PDF_NAME(Root));
        pdf_obj *names = pdf_dict_get(ctx, root, PDF_NAME(Names));
        int budget = GOMUPDF_TREE_BUDGET, incomplete = 0;
        // One action budget and verdict cache for the whole call: a large /AA
        // shared by many annotations, or by many keys, is walked once and
        // each further reach is charged against the budget.
        mk.abudget = gomupdf_action_budget(ctx, pdf);
        size_t len = (size_t)pdf_xref_len(ctx, pdf);
        mk.m = fz_calloc(ctx, len ? len : 1, sizeof *mk.m);
        mk.len = (int)len;
        // The same page-tree, form and page-content marks as Scrub's.
        gomupdf_mark_content(ctx, pdf, &mk);

        out[HC_INFO] = pdf_dict_get(ctx, trailer, PDF_NAME(Info)) != NULL;
        gomupdf_trailer_extra(ctx, pdf, &out[HC_TRAILER]);
        out[HC_XMP] = pdf_dict_get(ctx, root, PDF_NAME(Metadata)) != NULL;
        out[HC_ACROFORM] = pdf_dict_get(ctx, root, PDF_NAME(AcroForm)) != NULL;
        out[HC_XFA] = pdf_dict_getp(ctx, root, "AcroForm/XFA") != NULL;
        out[HC_OUTLINES] = pdf_dict_get(ctx, root, PDF_NAME(Outlines)) != NULL;
        gomupdf_add(&out[HC_EMBEDDED], gomupdf_name_tree(ctx, pdf_dict_get(ctx, names, PDF_NAME(EmbeddedFiles)),
                                                         0, NULL, &budget, &incomplete, NULL, NULL));
        // Fail closed: a tree that could not be walked in full is never "none".
        if (incomplete && out[HC_EMBEDDED] == 0) out[HC_EMBEDDED] = 1;
        out[HC_AF] = pdf_dict_get(ctx, root, PDF_NAME(AF)) != NULL;
        out[HC_PIECEINFO] = pdf_dict_get(ctx, root, PDF_NAME(PieceInfo)) != NULL;
        out[HC_LASTMOD] = pdf_dict_gets(ctx, root, "LastModified") != NULL;
        out[HC_PERMS] = pdf_dict_get(ctx, root, PDF_NAME(Perms)) != NULL;
        out[HC_DSS] = pdf_dict_gets(ctx, root, "DSS") != NULL;
        out[HC_WEBCAPTURE] = pdf_dict_gets(ctx, root, "SpiderInfo") || pdf_dict_gets(ctx, names, "IDS")
            || pdf_dict_gets(ctx, names, "URLS");
        pdf_obj *ocp = pdf_dict_get(ctx, root, PDF_NAME(OCProperties));
        out[HC_LAYERS] = ocp ? pdf_array_len(ctx, pdf_dict_get(ctx, ocp, PDF_NAME(OCGs))) : 0;
        out[HC_JS] = pdf_dict_get(ctx, names, PDF_NAME(JavaScript)) != NULL
            || gomupdf_open_action_unsafe(ctx, root, &mk, NULL)
            || gomupdf_aa_unsafe(ctx, pdf_dict_get(ctx, root, PDF_NAME(AA)), &mk, NULL);
        if (!out[HC_JS]) {
            budget = GOMUPDF_TREE_BUDGET;
            pdf_obj *fields = pdf_dict_getp(ctx, root, "AcroForm/Fields");
            out[HC_JS] = gomupdf_fields(ctx, NULL, 0, fields, 0, 0, NULL, &budget, &mk);
        }
        if (!out[HC_JS]) {
            budget = GOMUPDF_TREE_BUDGET;
            pdf_obj *outlines = pdf_dict_get(ctx, root, PDF_NAME(Outlines));
            out[HC_JS] = gomupdf_outline(ctx, NULL, outlines, 0, 0, NULL, &budget, &mk);
        }

        int npages = pdf_count_pages(ctx, pdf);
        for (int p = 0; p < npages; p++) {
            pdf_obj *page = pdf_lookup_page_obj(ctx, pdf, p);
            gomupdf_page_keys(ctx, NULL, page, out, &mk);
            gomupdf_page_nodes(ctx, &mk, page, GM_DONE, out, &mk);

            pdf_obj *annots = pdf_dict_get(ctx, page, PDF_NAME(Annots));
            int n = pdf_array_len(ctx, annots);
            for (int i = 0; i < n; i++) {
                pdf_obj *a = pdf_array_get(ctx, annots, i);
                if (!pdf_is_dict(ctx, a)) continue;
                if (!out[HC_JS] && gomupdf_actions(ctx, NULL, a, 0, &mk)) out[HC_JS] = 1;
                if (gomupdf_is_link(ctx, a)) continue;
                if (pdf_name_eq(ctx, pdf_dict_get(ctx, a, PDF_NAME(Subtype)), PDF_NAME(Widget))) {
                    gomupdf_add(&out[HC_WIDGETS], 1);
                    if (!out[HC_JS] && gomupdf_widget_fields(ctx, &mk, NULL, a, 0)) out[HC_JS] = 1;
                } else
                    gomupdf_add(&out[HC_NONLINK], 1);
            }
        }
        gomupdf_sweep(ctx, pdf, &mk, out, 0, 0);
    }
    fz_always(ctx) gomupdf_marks_free(ctx, &mk);
    fz_catch(ctx) {
        snprintf(err, errlen, "%s", fz_caught_message(ctx));
        failed = 1;
    }
    return failed ? -1 : 0;
}

// gomupdf_kept reports whether obj is indirect and kept (GM_KEEP).
static int gomupdf_kept(fz_context *ctx, gomupdf_marks *mk, pdf_obj *obj) {
    unsigned int *m = gomupdf_mark(ctx, mk, obj);
    return m && (*m & GM_KEEP);
}

// gomupdf_orphan_closure records for destruction the forms and images an
// orphan page's resource dictionary res reaches through /XObject, and those
// that the /Resources of each such form reach in turn, iteratively (the
// ORPHAN stack). Each indirect object is walked once (GM_OWALK: resource
// dictionaries, /XObject dictionaries, forms), and nothing kept (GM_KEEP) is
// walked: all a kept object reaches is kept too. What is recorded and kept
// is only unlinked.
static void gomupdf_orphan_closure(fz_context *ctx, gomupdf_marks *mk, pdf_obj *res) {
    gomupdf_stack *s = &mk->s[GS_ORPHAN];
    if (pdf_is_dict(ctx, res)) gomupdf_push(ctx, mk, GS_ORPHAN, res);
    while (s->n) {
        mk->cur = s->v[--s->n];
        pdf_obj *xo = pdf_dict_get(ctx, mk->cur, PDF_NAME(XObject));
        if (!gomupdf_kept(ctx, mk, mk->cur) && !gomupdf_walked(ctx, mk, mk->cur, GM_OWALK)
            && !gomupdf_kept(ctx, mk, xo) && !gomupdf_walked(ctx, mk, xo, GM_OWALK)) {
            int n = pdf_dict_len(ctx, xo);
            for (int i = 0; i < n; i++) {
                pdf_obj *v = pdf_dict_get_val(ctx, xo, i);
                gomupdf_destroy(ctx, mk, v, GK_XOBJECT);
                if (gomupdf_kept(ctx, mk, v) || !gomupdf_is_kind(ctx, v, GK_XOBJECT)
                    || !pdf_name_eq(ctx, pdf_dict_get(ctx, v, PDF_NAME(Subtype)), PDF_NAME(Form))
                    || gomupdf_walked(ctx, mk, v, GM_OWALK))
                    continue;
                pdf_obj *r = pdf_dict_get(ctx, v, PDF_NAME(Resources));
                if (pdf_is_dict(ctx, r)) gomupdf_push(ctx, mk, GS_ORPHAN, r);
            }
        }
        pdf_drop_obj(ctx, mk->cur);
        mk->cur = NULL;
    }
}

// gomupdf_orphan_page counts into out (HiddenContent) or empties (Scrub, mk)
// the page dictionary page, which is not a page MuPDF serves: it removes
// /Contents, /Resources, /Annots, /B, /Group, /StructParents, /Parent and the
// keys gomupdf_page_keys removes, recording for destruction the content
// streams (/Contents or the entries of a /Contents array, walked once however
// many orphan pages share it), the forms and images its /Resources reach
// (gomupdf_orphan_closure) and what gomupdf_page_keys records. A /Pages node
// it inherits from that is outside the page tree (/Type /Pages, not GM_NODE
// and not page content), and each such node above that one, up to
// GOMUPDF_MAX_DEPTH levels, is orphan content too and emptied the same way,
// once. Its annotations are then on no page, and gomupdf_sweep handles them
// as such. The dictionary stays, with its other keys (/Type, /MediaBox), for
// the destinations and structure elements that reference it. HiddenContent
// counts it only if it holds one of those keys, so an emptied one reads
// clean.
static void gomupdf_orphan_page(fz_context *ctx, gomupdf_marks *mk, pdf_obj *page, int *out) {
    static const char *keys[] = {"Contents", "Resources", "Annots", "B", "Group", "StructParents", "Parent",
                                 "Thumb", "AA", "AF", "Metadata", "PieceInfo", "LastModified", NULL};
    if (out) {
        for (const char **k = keys; *k; k++)
            if (pdf_dict_gets(ctx, page, *k)) {
                gomupdf_add(&out[HC_ORPHANPAGES], 1);
                break;
            }
        return;
    }
    pdf_obj *node = pdf_keep_obj(ctx, page), *up = NULL;
    fz_var(node);
    fz_var(up);
    fz_try(ctx) {
        for (int d = 0; node && d <= GOMUPDF_MAX_DEPTH; d++) {
            up = pdf_keep_obj(ctx, pdf_dict_get(ctx, node, PDF_NAME(Parent))); // deleting /Parent drops it
            gomupdf_page_keys(ctx, mk, node, NULL, mk);
            pdf_obj *c = pdf_dict_get(ctx, node, PDF_NAME(Contents));
            gomupdf_destroy(ctx, mk, c, GK_CONTENTS);
            int n = gomupdf_walked(ctx, mk, c, GM_OWALK) ? 0 : pdf_array_len(ctx, c);
            for (int i = 0; i < n; i++) gomupdf_destroy(ctx, mk, pdf_array_get(ctx, c, i), GK_CONTENTS);
            gomupdf_orphan_closure(ctx, mk, pdf_dict_get(ctx, node, PDF_NAME(Resources)));
            for (const char **k = keys; *k; k++) pdf_dict_dels(ctx, node, *k);
            pdf_drop_obj(ctx, node);
            node = NULL;
            unsigned int *m = gomupdf_mark(ctx, mk, up);
            if (m && !(*m & (GM_NODE | GM_CONTENT)) && gomupdf_type_is(ctx, up, "Pages")
                && !gomupdf_walked(ctx, mk, up, GM_OWALK)) {
                node = up;
                up = NULL;
            }
            pdf_drop_obj(ctx, up);
            up = NULL;
        }
    }
    fz_always(ctx) {
        pdf_drop_obj(ctx, node);
        pdf_drop_obj(ctx, up);
    }
    fz_catch(ctx) fz_rethrow(ctx);
}

// gomupdf_annot_shaped returns 1 if ref (an indirect object) is an
// annotation dictionary, 2 if it is a form field dictionary that is not one,
// and 0 otherwise. An annotation has /Type /Annot, or no /Type, a /Subtype
// that names an annotation type and a /Rect array; a field has no /Type
// other than /Annot and an /FT name, or a /T string and a /Kids array.
// Streams (XObjects), typed objects (fonts, pages, structure elements) and
// dictionaries without these keys never match.
static int gomupdf_annot_shaped(fz_context *ctx, pdf_obj *ref) {
    static const char *subtypes[] = {"Text", "Link", "FreeText", "Line", "Square", "Circle", "Polygon",
        "PolyLine", "Highlight", "Underline", "Squiggly", "StrikeOut", "Caret", "Stamp", "Ink", "Popup",
        "FileAttachment", "Sound", "Movie", "Screen", "Widget", "PrinterMark", "TrapNet", "Watermark", "3D",
        "Redact", "Projection", "RichMedia", NULL};
    if (!pdf_is_dict(ctx, ref) || pdf_is_stream(ctx, ref)) return 0;
    pdf_obj *t = pdf_dict_get(ctx, ref, PDF_NAME(Type)), *st = pdf_dict_get(ctx, ref, PDF_NAME(Subtype));
    if (t && !pdf_name_eq(ctx, t, PDF_NAME(Annot))) return 0;
    if (t || (pdf_is_name(ctx, st) && gomupdf_name_in(pdf_to_name(ctx, st), subtypes)
              && pdf_is_array(ctx, pdf_dict_get(ctx, ref, PDF_NAME(Rect)))))
        return 1;
    return pdf_is_name(ctx, pdf_dict_get(ctx, ref, PDF_NAME(FT)))
        || (pdf_is_string(ctx, pdf_dict_get(ctx, ref, PDF_NAME(T))) && pdf_is_array(ctx, pdf_dict_get(ctx, ref, PDF_NAME(Kids))))
        ? 2 : 0;
}

// gomupdf_orphan_annot handles the annotation or field a (shape from
// gomupdf_annot_shaped), which is on no page's /Annots and, unless Scrub
// removes the whole form, not in the form. HiddenContent (out) counts a
// widget or field in Widgets, any other annotation but a Link in
// NonLinkAnnotations, and reports its JavaScript and Launch actions (and a
// widget's fields' up /Parent). Scrub (mk) removes what the options remove
// from a page (gomupdf_annot_kept; a field counts as a widget), recording it
// for destruction as a removed annotation (gomupdf_record_annot), and strips
// the scripts from what stays, in place, as on a page.
static void gomupdf_orphan_annot(fz_context *ctx, gomupdf_marks *mk, pdf_obj *a, int shape, int *out, int keep_annots,
                                 int keep_fields) {
    int widget = shape == 2 || pdf_name_eq(ctx, pdf_dict_get(ctx, a, PDF_NAME(Subtype)), PDF_NAME(Widget));
    if (out) {
        if (!out[HC_JS] && gomupdf_actions(ctx, NULL, a, 0, mk)) out[HC_JS] = 1;
        if (!out[HC_JS] && widget && gomupdf_widget_fields(ctx, mk, NULL, a, 0)) out[HC_JS] = 1;
        if (widget) gomupdf_add(&out[HC_WIDGETS], 1);
        else if (!gomupdf_is_link(ctx, a)) gomupdf_add(&out[HC_NONLINK], 1);
        return;
    }
    if (!(gomupdf_annot_kept(ctx, a, keep_annots, keep_fields) || (keep_fields && widget))) {
        gomupdf_destroy(ctx, mk, a, GK_FIELD);
        gomupdf_record_annot(ctx, mk, a);
        return;
    }
    gomupdf_actions(ctx, mk, a, 1, mk);
    if (keep_fields && widget) gomupdf_widget_fields(ctx, mk, mk, a, 1);
}

// gomupdf_layer adds to out[HC_HIDDENLAYERS] the optional-content group or
// membership dictionary oc (/Type /OCG or /OCMD; anything else is ignored)
// if MuPDF's current configuration hides it for viewing, asking MuPDF about
// g: the reference to oc when it is indirect, oc itself when it is direct.
// It sets *floor when oc is one HiddenLayers reports whatever its state (see
// HiddenContent.HiddenLayers): a membership dictionary, a group with /Usage
// or an /Intent other than /View, or a direct one. MuPDF hides nothing when
// the configuration lists no group, so the caller asks only when it does.
static void gomupdf_layer(fz_context *ctx, pdf_document *pdf, pdf_obj *oc, pdf_obj *g, int *out, int *floor) {
    pdf_obj *t = pdf_dict_get(ctx, oc, PDF_NAME(Type)), *intent = pdf_dict_get(ctx, oc, PDF_NAME(Intent));
    int ocmd = pdf_name_eq(ctx, t, PDF_NAME(OCMD));
    if (pdf_is_stream(ctx, oc) || !pdf_is_dict(ctx, oc) || !(ocmd || pdf_name_eq(ctx, t, PDF_NAME(OCG)))) return;
    if (ocmd || !pdf_is_indirect(ctx, oc) || pdf_dict_get(ctx, oc, PDF_NAME(Usage))
        || (intent && !pdf_name_eq(ctx, intent, PDF_NAME(View))))
        *floor = 1;
    if (pdf_is_ocg_hidden(ctx, pdf, NULL, "View", g)) gomupdf_add(&out[HC_HIDDENLAYERS], 1);
}

// gomupdf_layer_direct applies gomupdf_layer to each direct value of the
// dictionary d (an /OC value, a /Properties entry).
static void gomupdf_layer_direct(fz_context *ctx, pdf_document *pdf, pdf_obj *d, int *out, int *floor) {
    int n = pdf_dict_len(ctx, d);
    for (int i = 0; i < n; i++) {
        pdf_obj *v = pdf_dict_get_val(ctx, d, i);
        if (!pdf_is_indirect(ctx, v)) gomupdf_layer(ctx, pdf, v, v, out, floor);
    }
}

// gomupdf_layers applies gomupdf_layer to object i (ref) and to the direct
// groups and membership dictionaries it holds where optional content can name
// one: its own values (an annotation's or XObject's /OC, the entries of a
// /Properties dictionary that is ref), the entries of a direct /Properties
// and of the /Properties of a direct /Resources. An indirect /Properties or
// /Resources is an object of its own, visited in its own turn. MuPDF matches
// groups against the configuration by number and generation, and the
// configuration names a group "N 0 R" (a generation is 0 for an object in an
// object stream, whose xref entry holds its index in the stream where a
// generation would be), so the reference it is asked about carries that.
static void gomupdf_layers(fz_context *ctx, pdf_document *pdf, int i, pdf_obj *ref, int *out, int *floor) {
    pdf_xref_entry *x = pdf_get_xref_entry_no_null(ctx, pdf, i);
    pdf_obj *g = pdf_new_indirect(ctx, pdf, i, x->type == 'o' ? 0 : x->gen);
    fz_try(ctx) {
        gomupdf_layer(ctx, pdf, ref, g, out, floor);
        gomupdf_layer_direct(ctx, pdf, ref, out, floor);
        pdf_obj *props = pdf_dict_get(ctx, ref, PDF_NAME(Properties)), *res = pdf_dict_get(ctx, ref, PDF_NAME(Resources));
        if (!pdf_is_indirect(ctx, props)) gomupdf_layer_direct(ctx, pdf, props, out, floor);
        props = pdf_dict_get(ctx, res, PDF_NAME(Properties));
        if (!pdf_is_indirect(ctx, res) && !pdf_is_indirect(ctx, props)) gomupdf_layer_direct(ctx, pdf, props, out, floor);
    }
    fz_always(ctx) pdf_drop_obj(ctx, g);
    fz_catch(ctx) fz_rethrow(ctx);
}

// gomupdf_sweep visits every object in the xref. It finds each /AF that is an
// associated-files array (gomupdf_af_files), skipping the catalog, pages and
// intermediate page-tree nodes (GM_PAGE, GM_INODE or GM_DONE in mk, whose /AF
// is handled with them). With out (HiddenContent) it counts them; otherwise
// (Scrub) it records their filespecs for destruction and, when every entry is
// one, deletes /AF in place, even on page content such as an image XObject:
// /AF plays no part in drawing, and a resource named /AF never holds a
// filespec. A mixed array keeps its other entries, and its destroyed
// filespecs become null. Among the objects the marks cover (not the copies
// Scrub makes), it counts or empties (gomupdf_orphan_page, with the options)
// each page dictionary that is not in the page tree (not GM_NODE: an orphan
// page, gomupdf_push_node) and, in
// Scrub, not page content. On every other private-data holder
// (gomupdf_private_holder) outside the catalog and page tree it counts or
// removes the private keys (gomupdf_private_keys). HiddenContent, when the
// configuration lists a group, also counts the hidden optional content
// (gomupdf_layers) and fails closed (at least 1) on what it cannot vouch for.
// A direct dictionary nested in an object is not visited, except where
// gomupdf_layers looks. The caller has loaded every object first
// (gomupdf_load_all), so no repair can happen during the sweep, and an entry
// that holds no object then (gomupdf_unloaded) is skipped: resolving it in
// every step would parse it again each time (100,000 members of a broken
// object stream took minutes).
static void gomupdf_sweep(fz_context *ctx, pdf_document *pdf, gomupdf_marks *mk, int *out, int keep_annots,
                          int keep_fields) {
    int n = pdf_xref_len(ctx, pdf), floor = 0;
    fz_var(floor);
    for (int i = 1; i < n; i++) {
        if (gomupdf_unloaded(ctx, pdf, i)) continue; // nothing to read; resolving it would parse it again
        unsigned int m = i < mk->len ? mk->m[i] : 0;
        pdf_obj *ref = pdf_new_indirect(ctx, pdf, i, 0), *af = NULL;
        fz_var(af);
        fz_try(ctx) {
            af = pdf_keep_obj(ctx, pdf_dict_get(ctx, ref, PDF_NAME(AF))); // a broken object is null
            int files = i == mk->root || (m & (GM_PAGE | GM_INODE | GM_DONE)) ? 0 : gomupdf_af_files(ctx, af);
            if (files && out) {
                gomupdf_add(&out[HC_AF], 1);
            } else if (files) {
                for (int j = 0; j < pdf_array_len(ctx, af); j++)
                    gomupdf_destroy_filespec(ctx, mk, pdf_array_get(ctx, af, j));
                if (files == pdf_array_len(ctx, af)) pdf_dict_del(ctx, ref, PDF_NAME(AF));
            }
            int shape = 0;
            if (i < mk->len && !(m & (GM_NODE | GM_CONTENT)) && gomupdf_page_shaped(ctx, ref))
                gomupdf_orphan_page(ctx, mk, ref, out);
            else if (i < mk->len && !(m & (GM_ONPAGE | GM_CONTENT)) && !((out || keep_fields) && (m & GM_FORM))
                     && (shape = gomupdf_annot_shaped(ctx, ref)))
                gomupdf_orphan_annot(ctx, mk, ref, shape, out, keep_annots, keep_fields);
            else if (i < mk->len && (m & GM_FORM) && !(m & (GM_ONPAGE | GM_CONTENT)) && (out || keep_fields)
                     && gomupdf_annot_shaped(ctx, ref)) {
                // A node of the form on no page that neither /Fields nor a
                // page widget's /Parent chain reaches (a sibling of a page
                // widget, say): its scripts are reported, or go when the
                // form stays, as on every other form node.
                if (!out) gomupdf_actions(ctx, mk, ref, 1, mk);
                else if (!out[HC_JS] && gomupdf_actions(ctx, NULL, ref, 0, mk)) out[HC_JS] = 1;
            } else if (i != mk->root && !(m & (GM_PAGE | GM_INODE | GM_DONE)) && gomupdf_private_holder(ctx, ref))
                gomupdf_private_keys(ctx, mk, ref, out);
            if (out && out[HC_LAYERS]) gomupdf_layers(ctx, pdf, i, ref, out, &floor);
        }
        fz_always(ctx) {
            pdf_drop_obj(ctx, af);
            pdf_drop_obj(ctx, ref);
        }
        fz_catch(ctx) fz_rethrow(ctx);
    }
    if (floor && !out[HC_HIDDENLAYERS]) out[HC_HIDDENLAYERS] = 1;
}

// gomupdf_trailer_del deletes key from the trailer. The first edit to an
// object makes MuPDF start an incremental xref section with a copy of the
// trailer, and when the trailer is a cross-reference stream's dictionary,
// editing the trailer is that first edit: the copy keeps the key, and a
// trailer fetched before it is stale. So it deletes from the current trailer
// until that holds no key, and fails if it cannot.
static void gomupdf_trailer_del(fz_context *ctx, pdf_document *pdf, pdf_obj *key) {
    for (int i = 0; i < 3 && pdf_dict_get(ctx, pdf_trailer(ctx, pdf), key); i++)
        pdf_dict_del(ctx, pdf_trailer(ctx, pdf), key);
    if (pdf_dict_get(ctx, pdf_trailer(ctx, pdf), key))
        fz_throw(ctx, FZ_ERROR_FORMAT, "cannot remove /%s from the trailer", pdf_to_name(ctx, key));
}

// gomupdf_trailer_extra returns the first key of the current trailer that is
// not a standard one (see HiddenContent.TrailerKeys), or NULL, and with count
// sets *count to the number of such keys. A save writes only /Size, /Root,
// /Info, /ID and /Encrypt, but garbage collection marks from the whole
// trailer, so what another key references is written as an object nothing
// references. A cross-reference stream's own keys are in the list because
// MuPDF's trailer for such a file is that stream's dictionary.
static pdf_obj *gomupdf_trailer_extra(fz_context *ctx, pdf_document *pdf, int *count) {
    static const char *std[] = {"Root", "Info", "ID", "Encrypt", "Size", "Prev", "XRefStm",
                                "Type", "W", "Index", "Filter", "DecodeParms", "Length", NULL};
    pdf_obj *t = pdf_trailer(ctx, pdf), *first = NULL;
    int n = pdf_dict_len(ctx, t);
    if (count) *count = 0;
    for (int i = 0; i < n; i++) {
        pdf_obj *k = pdf_dict_get_key(ctx, t, i);
        if (gomupdf_name_in(pdf_to_name(ctx, k), std)) continue;
        if (!first) first = k;
        if (!count) break;
        ++*count;
    }
    return first;
}

// gomupdf_trailer_strip deletes every non-standard key from the current
// trailer (gomupdf_trailer_del), one at a time, and fails if any is left.
// What they referenced is only unlinked.
static void gomupdf_trailer_strip(fz_context *ctx, pdf_document *pdf) {
    int n = pdf_dict_len(ctx, pdf_trailer(ctx, pdf));
    for (int round = 0;; round++) {
        pdf_obj *k = gomupdf_trailer_extra(ctx, pdf, NULL);
        if (!k) return;
        if (round > n) fz_throw(ctx, FZ_ERROR_FORMAT, "cannot remove /%s from the trailer", pdf_to_name(ctx, k));
        k = pdf_keep_obj(ctx, k); // the deletion drops the trailer's reference
        fz_try(ctx) gomupdf_trailer_del(ctx, pdf, k);
        fz_always(ctx) pdf_drop_obj(ctx, k);
        fz_catch(ctx) fz_rethrow(ctx);
    }
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
    unsigned int *m = gomupdf_mark(ctx, mk, v);
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
// each of its entries, and returns the array. An entry that stays (see
// gomupdf_annot_kept) is copied to a new indirect object; one Scrub removes
// may be copied directly.
static pdf_obj *gomupdf_own_annots(fz_context *ctx, gomupdf_marks *mk, pdf_obj *page, int keep_annots,
                                   int keep_fields) {
    pdf_obj *arr = gomupdf_own(ctx, mk, page, PDF_NAME(Annots), 0, 0);
    int n = pdf_array_len(ctx, arr);
    for (int i = 0; i < n; i++)
        gomupdf_own(ctx, mk, arr, NULL, i, gomupdf_annot_kept(ctx, pdf_array_get(ctx, arr, i), keep_annots, keep_fields));
    return arr;
}

// gomupdf_record_annot records the annotation e, which Scrub removes, for
// destruction: e itself, its JavaScript and Launch actions, its /FS and /AF
// filespecs, its /Popup and, for a widget, the fields above it up /Parent
// (GOMUPDF_MAX_DEPTH levels) with their JavaScript and Launch actions.
static void gomupdf_record_annot(fz_context *ctx, gomupdf_marks *del, pdf_obj *e) {
    gomupdf_destroy(ctx, del, e, GK_ANNOT);
    gomupdf_actions(ctx, del, e, 0, del);
    gomupdf_destroy_filespec(ctx, del, pdf_dict_get(ctx, e, PDF_NAME(FS)));
    pdf_obj *af = pdf_dict_get(ctx, e, PDF_NAME(AF));
    for (int j = 0; j < pdf_array_len(ctx, af); j++)
        gomupdf_destroy_filespec(ctx, del, pdf_array_get(ctx, af, j));
    pdf_obj *popup = pdf_dict_get(ctx, e, PDF_NAME(Popup));
    if (pdf_name_eq(ctx, pdf_dict_get(ctx, popup, PDF_NAME(Subtype)), PDF_NAME(Popup)))
        gomupdf_destroy(ctx, del, popup, GK_ANNOT);
    if (!pdf_name_eq(ctx, pdf_dict_get(ctx, e, PDF_NAME(Subtype)), PDF_NAME(Widget))) return;
    pdf_obj *f = pdf_dict_get(ctx, e, PDF_NAME(Parent));
    for (int d = 0; d < GOMUPDF_MAX_DEPTH && pdf_is_dict(ctx, f); d++) {
        gomupdf_destroy(ctx, del, f, GK_FIELD);
        gomupdf_actions(ctx, del, f, 0, del);
        f = pdf_dict_get(ctx, f, PDF_NAME(Parent));
    }
}

// gomupdf_scrub_annots deletes every non-Link annotation and (unless
// keep_fields) every widget on one page through MuPDF, then sweeps the raw
// /Annots array for anything MuPDF does not list (Popup and unknown subtypes,
// broken entries), keeping only Links and, with keep_fields, widgets. Every
// removed entry, its /Popup, /FS and /AF filespecs, its JavaScript and Launch
// actions and, for a widget, its /Parent field chain are recorded in del
// first. Before any of that, the page takes private copies of the /Annots
// array and entries kept content also uses, and resyncs a page MuPDF already
// has open with them.
static void gomupdf_scrub_annots(fz_context *ctx, pdf_document *pdf, int pageno, gomupdf_marks *del,
                                 int keep_fields) {
    pdf_page *page = NULL;
    fz_var(page);
    fz_try(ctx) {
        gomupdf_own_annots(ctx, del, pdf_lookup_page_obj(ctx, pdf, pageno), 0, keep_fields);
        page = pdf_load_page(ctx, pdf, pageno);
        pdf_sync_annots(ctx, page);
        pdf_obj *arr = pdf_dict_get(ctx, page->obj, PDF_NAME(Annots));
        for (int i = 0; i < pdf_array_len(ctx, arr); i++) {
            pdf_obj *e = pdf_array_get(ctx, arr, i);
            if (!pdf_is_dict(ctx, e) || gomupdf_annot_kept(ctx, e, 0, keep_fields)) continue;
            gomupdf_record_annot(ctx, del, e);
            // pdf_delete_annot also unlinks the /Popup target from /Annots,
            // even a Link; the sweep below removes real popups instead.
            pdf_dict_del(ctx, e, PDF_NAME(Popup));
        }
        pdf_annot *a, *next;
        // Take next before deleting: pdf_delete_annot drops the page's reference.
        for (a = pdf_first_annot(ctx, page); a; a = next) {
            next = pdf_next_annot(ctx, a);
            if (pdf_annot_type(ctx, a) != PDF_ANNOT_LINK) pdf_delete_annot(ctx, page, a);
        }
        for (a = keep_fields ? NULL : pdf_first_widget(ctx, page); a; a = next) {
            next = pdf_next_widget(ctx, a);
            pdf_delete_annot(ctx, page, a);
        }
        pdf_obj *annots = pdf_dict_get(ctx, page->obj, PDF_NAME(Annots));
        for (int i = pdf_array_len(ctx, annots) - 1; i >= 0; i--) {
            pdf_obj *e = pdf_array_get(ctx, annots, i);
            if (!pdf_is_dict(ctx, e) || !gomupdf_annot_kept(ctx, e, 0, keep_fields)) pdf_array_delete(ctx, annots, i);
        }
    }
    fz_always(ctx) { fz_drop_page(ctx, (fz_page *)page); }
    fz_catch(ctx) { fz_rethrow(ctx); }
}

static int gomupdf_scrub(fz_context *ctx, fz_document *doc, int keep_annots, int keep_fields,
                         int keep_outlines, char *err, int errlen) {
    pdf_document *pdf = pdf_specifics(ctx, doc);
    if (!pdf) { snprintf(err, errlen, "not a PDF document"); return -1; }
    keep_fields |= keep_annots;
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
        repaired = gomupdf_keep_all(ctx, pdf, &mk, keep_annots, keep_fields, keep_outlines);
        marked = 1;

        pdf_obj *trailer = pdf_trailer(ctx, pdf);
        pdf_obj *root = pdf_dict_get(ctx, trailer, PDF_NAME(Root));
        pdf_obj *names = pdf_dict_get(ctx, root, PDF_NAME(Names));
        pdf_obj *af = pdf_dict_get(ctx, root, PDF_NAME(AF));
        // The edit pass gets a fresh action budget and keeps the marking
        // pass's verdicts (GM_ASAFE, GM_ABAD).
        mk.abudget = gomupdf_action_budget(ctx, pdf);

        // Record what is removed below while it is still linked.
        gomupdf_destroy(ctx, &mk, pdf_dict_get(ctx, trailer, PDF_NAME(Info)), GK_INFO);
        gomupdf_destroy(ctx, &mk, pdf_dict_get(ctx, root, PDF_NAME(Metadata)), GK_XMP);
        for (int i = 0; i < pdf_array_len(ctx, af); i++)
            gomupdf_destroy_filespec(ctx, &mk, pdf_array_get(ctx, af, i));
        gomupdf_destroy_name_tree(ctx, &mk, pdf_dict_get(ctx, names, PDF_NAME(EmbeddedFiles)), 0);
        gomupdf_destroy_name_tree(ctx, &mk, pdf_dict_get(ctx, names, PDF_NAME(JavaScript)), 1);
        if (!keep_fields) {
            pdf_obj *acroform = pdf_dict_get(ctx, root, PDF_NAME(AcroForm));
            int budget = GOMUPDF_TREE_BUDGET;
            gomupdf_destroy(ctx, &mk, acroform, GK_ACROFORM);
            gomupdf_fields(ctx, &mk, 0, pdf_dict_get(ctx, acroform, PDF_NAME(Fields)), 0, 0, NULL, &budget, &mk);
        }
        gomupdf_destroy_xfa(ctx, &mk, pdf_dict_getp(ctx, root, "AcroForm/XFA"));
        gomupdf_aa_unsafe(ctx, pdf_dict_get(ctx, root, PDF_NAME(AA)), &mk, &mk);
        gomupdf_destroy_pieceinfo(ctx, &mk, pdf_dict_get(ctx, root, PDF_NAME(PieceInfo)));
        gomupdf_destroy(ctx, &mk, pdf_dict_gets(ctx, root, "SpiderInfo"), GK_EF);
        gomupdf_destroy_name_tree(ctx, &mk, pdf_dict_gets(ctx, names, "IDS"), 0);
        gomupdf_destroy_name_tree(ctx, &mk, pdf_dict_gets(ctx, names, "URLS"), 0);
        if (!keep_fields) gomupdf_destroy_signatures(ctx, &mk, root);

        gomupdf_trailer_del(ctx, pdf, PDF_NAME(Info));
        gomupdf_trailer_strip(ctx, pdf);
        pdf_dict_del(ctx, root, PDF_NAME(Metadata));
        pdf_dict_del(ctx, root, PDF_NAME(AA));
        pdf_dict_del(ctx, root, PDF_NAME(AF));
        pdf_dict_del(ctx, root, PDF_NAME(PieceInfo));
        pdf_dict_dels(ctx, root, "LastModified");
        pdf_dict_dels(ctx, root, "SpiderInfo");
        // A kept form keeps the signature data its signature fields may use.
        if (!keep_fields) {
            pdf_dict_del(ctx, root, PDF_NAME(Perms));
            pdf_dict_dels(ctx, root, "DSS");
        }
        // Unlink /AcroForm before the page loop: pdf_delete_annot on a widget
        // walks /Fields recursively with no depth cap and no cycle check on
        // direct /Kids arrays, so a hostile field tree would overflow the stack.
        // With keep_fields it stays, and Scrub deletes no widget. /XFA (a
        // second, XML copy of the form, with its own values and scripts, that
        // MuPDF ignores) and the catalog's XFA-only /NeedsRendering go always.
        if (!keep_fields) pdf_dict_del(ctx, root, PDF_NAME(AcroForm));
        else if (pdf_dict_getp(ctx, root, "AcroForm/XFA"))
            pdf_dict_del(ctx, gomupdf_own(ctx, &mk, root, PDF_NAME(AcroForm), 0, 0), PDF_NAME(XFA));
        pdf_dict_dels(ctx, root, "NeedsRendering");
        if (names) {
            names = gomupdf_own(ctx, &mk, root, PDF_NAME(Names), 0, 0);
            pdf_dict_del(ctx, names, PDF_NAME(EmbeddedFiles));
            pdf_dict_del(ctx, names, PDF_NAME(JavaScript));
            pdf_dict_dels(ctx, names, "IDS");
            pdf_dict_dels(ctx, names, "URLS");
        }
        if (gomupdf_open_action_unsafe(ctx, root, &mk, &mk)) pdf_dict_dels(ctx, root, "OpenAction");
        if (!keep_outlines) {
            pdf_dict_del(ctx, root, PDF_NAME(Outlines));
            if (pdf_name_eq(ctx, pdf_dict_get(ctx, root, PDF_NAME(PageMode)), PDF_NAME(UseOutlines)))
                pdf_dict_put_name(ctx, root, PDF_NAME(PageMode), "UseNone");
        }

        int npages = pdf_count_pages(ctx, pdf);
        for (int p = 0; p < npages; p++) {
            if (!keep_annots) gomupdf_scrub_annots(ctx, pdf, p, &mk, keep_fields);
            pdf_obj *page = pdf_lookup_page_obj(ctx, pdf, p);
            gomupdf_page_keys(ctx, &mk, page, NULL, &mk);
            gomupdf_page_nodes(ctx, &mk, page, GM_DONE, NULL, &mk);
            // Annotations that stay (see gomupdf_annot_kept) lose their /AA
            // and their JavaScript and Launch /A and /PA so no script
            // survives, matching what HiddenContent checks.
            pdf_obj *annots = gomupdf_own_annots(ctx, &mk, page, keep_annots, keep_fields);
            int n = pdf_array_len(ctx, annots);
            for (int i = 0; i < n; i++) {
                pdf_obj *a = pdf_array_get(ctx, annots, i);
                if (!pdf_is_dict(ctx, a)) continue;
                gomupdf_actions(ctx, &mk, a, 1, &mk);
                if (keep_fields && pdf_name_eq(ctx, pdf_dict_get(ctx, a, PDF_NAME(Subtype)), PDF_NAME(Widget)))
                    gomupdf_widget_fields(ctx, &mk, &mk, a, 1);
            }
        }
        // The field tree, past the widgets the page loop reached, and the
        // outline items lose their scripts too.
        if (keep_fields) {
            pdf_obj *acroform = pdf_dict_get(ctx, root, PDF_NAME(AcroForm));
            int budget = GOMUPDF_TREE_BUDGET;
            gomupdf_fields(ctx, &mk, 1, pdf_dict_get(ctx, acroform, PDF_NAME(Fields)),
                           gomupdf_in_content(ctx, &mk, acroform, 0), 0, NULL, &budget, &mk);
        }
        if (keep_outlines) {
            pdf_obj *outlines = pdf_dict_get(ctx, root, PDF_NAME(Outlines));
            int budget = GOMUPDF_TREE_BUDGET;
            gomupdf_outline(ctx, &mk, outlines, gomupdf_in_content(ctx, &mk, outlines, 0), 0, NULL, &budget,
                            &mk);
        }
        // /AF wherever else it occurs (XObjects, structure elements,
        // annotations, outline items, Scrub's own copies), and pages that are
        // not in the page tree.
        gomupdf_sweep(ctx, pdf, &mk, NULL, keep_annots, keep_fields);
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

// stabilise loads every object of a PDF once, so that any xref repair MuPDF
// needs happens now. MuPDF repairs a damaged xref at most once, lazily, the
// first time an object fails to load, and the repair rebuilds the xref from
// the file: every change made before it (new objects, edited dictionaries)
// is dropped from the document, and the page tree, page count and objects
// can differ from what was read before. Done once per document; later calls
// are free. The cost is one pass over the objects, parsing each (streams are
// not decoded), comparable to a garbage-collecting save. A locked document
// is refused: objects loaded before Authenticate keep the wrong key's text.
func (d *mupdfDoc) stabilise() error {
	if d.stable {
		return nil
	}
	if d.locked {
		return errLocked
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	if C.gomupdf_stabilise(d.ctx, d.doc, errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: load objects: " + errText(errBuf))
	}
	d.stable = true
	return nil
}

// repaired reports whether MuPDF has repaired (or tried to repair) the xref.
func (d *mupdfDoc) repaired() bool {
	return C.gomupdf_repair_attempted(d.ctx, d.doc) != 0
}

func (d *mupdfDoc) scrub(opts ScrubOptions) error {
	if err := d.writable(); err != nil {
		return err
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	flag := func(b bool) C.int {
		if b {
			return 1
		}
		return 0
	}
	if C.gomupdf_scrub(d.ctx, d.doc, flag(opts.KeepAnnotations), flag(opts.KeepFormFields), flag(opts.KeepOutlines),
		errBuf, errBufLen) != 0 {
		return errors.New("gomupdf: scrub: " + errText(errBuf))
	}
	return nil
}

func (d *mupdfDoc) hiddenContentRaw() ([]int, error) {
	if err := d.readable(); err != nil {
		return nil, err
	}
	errBuf := newErrBuf()
	defer C.free(unsafe.Pointer(errBuf))
	var out [C.HC_COUNT]C.int
	if C.gomupdf_hidden_content(d.ctx, d.doc, &out[0], errBuf, errBufLen) != 0 {
		return nil, errors.New("gomupdf: hidden content: " + errText(errBuf))
	}
	counts := make([]int, len(out))
	for i, v := range out {
		counts[i] = int(v)
	}
	return counts, nil
}
