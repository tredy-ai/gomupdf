package gomupdf

import (
	"errors"
	"fmt"
)

// Document scrub: removing, and checking for, the document-level places where
// content survives page redaction. Both Scrub and HiddenContent are additions
// of this fork; upstream has only the piecemeal SetMetadata, DeleteXMP,
// DeleteAnnotation and SetTOC.

// ScrubOptions selects what Document.Scrub keeps. The zero value removes
// everything Scrub knows about.
type ScrubOptions struct {
	// KeepAnnotations keeps every annotation, widget and the catalog
	// /AcroForm. Their scripts still go: every annotation and every node of
	// the field tree loses /AA, and its /A (and /PA) when that is or chains to
	// a JavaScript or Launch action. /AcroForm /XFA is removed regardless:
	// it is a second, XML copy of the form with its own scripts and values,
	// which MuPDF ignores, so Page.Widgets and SetTextField never see or
	// clear what it holds. The zero value removes all annotations except Link
	// annotations, all widgets, and /AcroForm.
	KeepAnnotations bool
	// KeepFormFields keeps the form: every widget annotation and the catalog
	// /AcroForm with its whole field tree (fields on no page included), with
	// their scripts removed as under KeepAnnotations, and without /XFA, which
	// Scrub always removes because its XML copy of the field values and its
	// scripts are invisible to Page.Widgets and SetTextField. Every other non-Link
	// annotation, popups and replies included, is still removed as by
	// default. KeepAnnotations keeps the form too, whatever KeepFormFields
	// says. Form field values are not reliably part of page text extraction
	// (only an appearance stream that shows a value is), so a caller that
	// keeps form fields must inspect the values itself (Page.Widgets) to
	// detect or clear sensitive ones. KeepFormFields is new in this fork.
	KeepFormFields bool
	// KeepOutlines keeps the catalog /Outlines. Each item keeps its title and
	// any /Dest but loses an /A that is or chains to a JavaScript or Launch
	// action. The zero value removes the outlines and resets /PageMode
	// /UseOutlines to /UseNone.
	KeepOutlines bool
}

// HiddenContent counts the document-level content that Scrub removes. A zero
// value means none of it is present. Counts saturate at 2,147,483,647 (the C
// int maximum) instead of wrapping.
type HiddenContent struct {
	Info     bool // trailer /Info dictionary
	XMP      bool // catalog /Metadata stream
	AcroForm bool // catalog /AcroForm
	XFA      bool // /AcroForm /XFA, which Scrub always removes
	Outlines bool // catalog /Outlines

	// EmbeddedFiles is the number of entries in the /Names/EmbeddedFiles name
	// tree, following /Kids. The walk stops at 32 levels, at a /Kids cycle,
	// and after 65,536 units of work (one per node, per /Kids entry and per
	// /Names pair); a tree it cannot walk in full reports the entries it did
	// walk, and at least 1, never 0.
	EmbeddedFiles int
	// AssociatedFiles is the number of /AF arrays on the catalog, on page
	// dictionaries and on intermediate page-tree nodes (the /Pages nodes
	// above a page, up to 32 levels, each counted once), plus the number of
	// other objects (XObjects, structure elements, annotations and so on)
	// whose /AF is an array holding a file specification.
	AssociatedFiles int

	// JavaScript reports /Names/JavaScript, or a script action (directly or
	// through its /Next chain) in the catalog /OpenAction, the
	// catalog /AA, the /AA of a page or intermediate page-tree node, the /AA,
	// /A or /PA of any entry in a page's /Annots (annotations, widgets and
	// Links, including those a default Scrub removes), the /AA or /A of any
	// node of the /AcroForm field tree or of any field above a widget (up
	// /Parent, which reaches fields /Fields leaves out), or the /A of any
	// outline item. It does not know which ScrubOptions a later Scrub uses, so
	// it reports these wherever they are. A script action is one with /S
	// /JavaScript or /S /Launch, one with a /JS entry whatever its /S (a
	// Rendition action's, say), or a URI action whose /URI is a javascript:
	// URI: it starts with "javascript:" in any case once a UTF-8 byte-order
	// mark and leading whitespace and control characters are skipped and tab,
	// CR and LF are ignored, either as raw bytes or as the text MuPDF decodes
	// (UTF-16 with a byte-order mark). In this documentation "JavaScript or
	// Launch action" means any of them.
	//
	// The field-tree and outline walks stop at 32 levels and at a /Kids or
	// /First cycle, and after 65,536 units of work (one per /Fields or /Kids
	// entry, one per outline item); a walk cut short, or a widget with more
	// than 32 fields above it, counts as JavaScript, and still does after a
	// Scrub that keeps that form or outline. The action walks of one call
	// share a budget of 4,096 + 32 x (xref length) reaches: every time a walk
	// reaches an action dictionary or /Next array costs one unit, even an
	// indirect one already decided, whose verdict is reused rather than walked
	// again. They stop at 32 levels of /Next; an action they cannot walk in
	// full (cyclic, too deep, or past the budget) counts as JavaScript. A
	// well-formed file reaches each action a few times and stays far within
	// the budget whatever its size; one action shared by very many places (one
	// Link listed 4,257 times on a page of a 5-object file) runs out, reports
	// JavaScript before Scrub, and loses that action in Scrub.
	JavaScript bool

	// NonLinkAnnotations and Widgets count the entries of every page's /Annots
	// array: Widgets those with /Subtype /Widget, NonLinkAnnotations all other
	// dictionaries except /Subtype /Link (Popups included). An /Annots array
	// shared by several pages counts once per page.
	NonLinkAnnotations, Widgets int

	// Thumbnails and PageMetadata count pages and intermediate page-tree
	// nodes with /Thumb and /Metadata.
	Thumbnails, PageMetadata int
	// PieceInfo counts /PieceInfo dictionaries on the catalog, on pages and on
	// intermediate page-tree nodes.
	PieceInfo int

	// TrailerKeys counts the keys of the trailer other than /Root, /Info,
	// /ID, /Encrypt, /Size, /Prev and /XRefStm and, when the trailer is a
	// cross-reference stream's dictionary, that stream's own /Type, /W,
	// /Index, /Filter, /DecodeParms and /Length. A save never writes them,
	// but a garbage-collecting save keeps everything they reference, as
	// objects nothing in the saved file references (qpdf's /QTest, say, can
	// hold a page's content stream from before a redaction).
	TrailerKeys int

	// OrphanPages counts the page dictionaries that are not in the page tree
	// but still hold content (any key Scrub removes from them). A page
	// dictionary is one with /Type /Page or /Type /Template (a template
	// /Names/Templates lists), or with no /Type, a /MediaBox and a /Contents
	// or /Parent; one with /Kids is a page-tree node and never counts. The
	// page tree is every node reached from the catalog /Pages down /Kids and
	// up /Parent from each page PageCount counts. Such a page is never
	// extracted, rendered or redacted, while a structure element's /Pg, a
	// destination or a template can still reach it.
	OrphanPages int
}

// hiddenContentCounts is the number of counts the backend returns.
const hiddenContentCounts = 15

// hiddenContentFromCounts maps the backend's counts, in HiddenContent field
// order, to a HiddenContent.
func hiddenContentFromCounts(c []int) (HiddenContent, error) {
	if len(c) != hiddenContentCounts {
		return HiddenContent{}, fmt.Errorf("gomupdf: hidden content: got %d counts, want %d", len(c), hiddenContentCounts)
	}
	return HiddenContent{
		Info:               c[0] != 0,
		XMP:                c[1] != 0,
		AcroForm:           c[2] != 0,
		Outlines:           c[3] != 0,
		EmbeddedFiles:      c[4],
		AssociatedFiles:    c[5],
		JavaScript:         c[6] != 0,
		NonLinkAnnotations: c[7],
		Widgets:            c[8],
		Thumbnails:         c[9],
		PageMetadata:       c[10],
		PieceInfo:          c[11],
		XFA:                c[12] != 0,
		TrailerKeys:        c[13],
		OrphanPages:        c[14],
	}, nil
}

// Scrub removes document-level content that page redaction never touches:
//
//   - trailer /Info and every non-standard trailer key (see HiddenContent's
//     TrailerKeys), and catalog /Metadata, /AA, /AF and /PieceInfo;
//   - /Names/EmbeddedFiles and /Names/JavaScript;
//   - the catalog /OpenAction, if it or any action in its /Next chain is a
//     JavaScript or Launch action (a destination or other action is kept);
//   - unless opts.KeepOutlines, /Outlines (and /PageMode /UseOutlines becomes
//     /UseNone); with it, the /A of every outline item that is or chains to a
//     JavaScript or Launch action (the item, its title and any /Dest stay);
//   - on every page dictionary and every intermediate page-tree node above a
//     page (up to 32 levels), /Thumb, /AA, /AF, /Metadata and /PieceInfo;
//   - unless opts.KeepAnnotations, every annotation except Links and widgets,
//     popups and replies included; unless opts.KeepAnnotations or
//     opts.KeepFormFields, every widget and the catalog /AcroForm;
//   - whatever the options, /AcroForm /XFA (a stream, or an array of
//     name/stream pairs) and the catalog /NeedsRendering, while the rest of
//     a kept /AcroForm stays;
//   - the file specifications of /AF wherever else an object (an image or
//     form XObject, a structure element, an annotation that stays, an
//     outline item) has an /AF array holding one, and the /AF key too when
//     every entry is one; a direct dictionary nested inside another object
//     is not searched;
//   - on every page dictionary that is not in the page tree (see
//     HiddenContent's OrphanPages; a structure element's /Pg, a destination
//     or /Names/Templates can still reach one), /Contents, /Resources,
//     /Annots, /B, /Group, /StructParents and the keys removed from pages
//     above; the dictionary itself stays, page-shaped, for those references;
//   - on every annotation that stays, /AA, and each of /A and /PA that is or
//     chains through /Next to a JavaScript or Launch action; when the form
//     stays (opts.KeepAnnotations or opts.KeepFormFields), the same on every
//     node of the /AcroForm field tree (fields with /Kids as well as widgets,
//     edited in place so each stays the object /Fields and /Annots list) and
//     on every field above a kept widget up /Parent, which reaches fields
//     /Fields leaves out (32 levels; a field with /Kids that neither reaches
//     is not touched). The annotation or field stays: a Link that loses its
//     /A is inert. A URI, GoTo, GoToR, Named or other action stays as it was.
//
// Removed objects that may be reachable from elsewhere are destroyed, not
// just unlinked, so a reference that survives (a structure-tree /OBJR or /AF,
// as tagged PDFs carry) resolves to null instead of keeping the content.
// Unless opts.KeepAnnotations, that is the removed annotations (and widgets,
// unless opts.KeepFormFields) with their /Popup and their /FS and /AF
// filespecs, and, unless the form stays, the /AcroForm dictionary with its
// field tree; in every case it is the content streams of a page that is not
// in the page tree, the forms and images in its own /Resources /XObject, and
// the annotations on it that the options remove as on a page, /Info, the XMP
// stream,
// page and page-tree node /Metadata and /Thumb streams, the filespecs of
// /Names/EmbeddedFiles and of every /AF Scrub removes, every such
// filespec's /EF streams, the /Names/JavaScript actions with their script
// strings and streams, the nodes of both name trees, the /XFA streams, and
// every JavaScript or Launch action Scrub removes or removes the holder of
// (from /OpenAction, any /AA, an /A or /PA, an outline item, a removed
// annotation or field, the /Next chain of a /Names/JavaScript action) with
// its /JS script string or stream, its /URI string, its /F file and a Launch
// action's /Win, /Mac and /Unix dictionaries with their /F files, and with
// every other action dictionary of the chain it removes (a GoTo whose /Next
// is the script, say). Each is destroyed only if it is recognisably of its
// kind (an /Info dictionary has no /Type or /Type /Info, an XMP stream has /Type /Metadata
// or /Subtype /XML, a filespec has /Type /Filespec, a script action is one
// as HiddenContent's JavaScript field defines it, another action has /S and
// no /Type other than /Action, an XFA stream or content stream has no /Type
// or /Subtype, an image or form has /Subtype /Image or /Form and no /Type
// other than /XObject, and so on).
//
// Scrub never destroys kept content. Before it changes anything it loads
// every object (so that an xref repair MuPDF needs happens first) and marks
// as kept the catalog and every page-tree node (down /Kids from /Pages, and
// up /Parent from every page); everything referenced, directly or not, from
// each node's /Contents, /Resources and /Group (so the content streams and
// the own and inherited resources of every page), from /Encrypt and from
// /OCProperties; and the annotations that stay (Links, widgets with
// opts.KeepFormFields, or all of them with opts.KeepAnnotations), when the
// form stays the /AcroForm dictionary and its field tree (through /Fields,
// /Kids and /CO), and with
// opts.KeepOutlines the outline items, with everything they reference other
// than through their own /Parent, /P, /Popup, /AA, /SE and /XFA and an /A or
// /PA that Scrub removes. An object reached as kept content is walked through
// every key, whatever other role it has (page-tree node, kept annotation,
// field, outline item), except on a page or an intermediate page-tree node
// that Scrub edits, where the walk skips the keys Scrub removes there and
// /Annots, on the catalog, where it skips the keys Scrub removes or edits
// there, /Pages and /StructTreeRoot, and on any object the file
// specifications in an /AF array (an image the page draws may carry one;
// Scrub removes them in place, which does not change the drawing, and a
// resource named /AF never holds a file specification). A page dictionary
// that is not in the page tree and is reached other than as page content (a
// kept Link's or outline item's destination) is kept but not walked, so what
// it holds is still destroyed. A removed entry that points at kept
// content (a /Thumb that the page also draws, an /Info that is the page's
// /Resources, a JavaScript action that is also a graphics state, an action
// that a kept /A or a kept /OpenAction also uses) is only unlinked. A
// field-tree node, outline item or intermediate page-tree node
// that is itself reached as page content (a malformed file) is left as it is,
// scripts included; HiddenContent still reports what it holds.
//
// Everything else is only unlinked too: annotation appearance streams and
// resources, what a non-standard trailer key references, actions other than
// JavaScript and Launch, /PieceInfo,
// /Outlines, objects not recognisably of the removed kind, and the parts of
// a name tree, field tree or outline beyond 32 levels or 65,536 units of
// walk work (a field listed by several parents is walked once), and of a
// page tree more than 32 levels above a page. Unlinked
// objects leave the file only if nothing else references them. Scrub's
// marking pass and its edit pass each have an action budget of 4,096 + 32 x
// (xref length) reaches, as a HiddenContent call does, and the edit pass
// reuses the marking pass's verdict on each indirect action; past the
// budget, every /A, /PA and /OpenAction Scrub checks counts as unsafe and is
// removed. A well-formed file of any size stays within it; see
// HiddenContent's JavaScript field for the shape that does not.
//
// Out of scope: the scripts of 3D and RichMedia annotations when annotations
// are kept; actions of other kinds that reach outside the file (URI, GoToR,
// GoToE, SubmitForm, ImportData); content beyond the limits above; a
// field-tree node, outline item or page-tree node that is also page content;
// an annotation or widget that is on no page's /Annots and not in the
// /AcroForm field tree (one only the structure tree references); and text
// in structure elements, optional-content groups, name trees and other
// document-level dictionaries Scrub does not list here.
//
// Scrub is meaningful only followed by a non-incremental save with
// SaveOptions.Garbage >= 1, which drops unreferenced objects and nulls
// references to destroyed ones; an incremental save keeps the original bytes
// in the file. Use HiddenContent on the reopened output to verify the
// locations it reads.
//
// An error from Scrub means the document MUST NOT be treated as scrubbed: it
// may have stopped partway, with some content removed and some left, and
// HiddenContent may read zero for locations it never reached. What it had
// already removed is still destroyed, unless the destroying itself fails or
// MuPDF repaired the xref after Scrub marked kept content (which loading
// every object first rules out); then removed objects may be only unlinked.
// A document on which Scrub failed MUST be discarded, not retried or saved as
// scrubbed: every later Scrub call on it returns an error.
//
// Scrub is new in this fork. It returns an error for non-PDF documents.
func (d *Document) Scrub(opts ScrubOptions) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	if d.locked {
		return errors.New("gomupdf: scrub: document is locked")
	}
	if d.badScrub {
		return errors.New("gomupdf: scrub: an earlier Scrub failed on this document; discard it")
	}
	if err := d.b.scrub(opts); err != nil {
		d.badScrub = true
		return err
	}
	return nil
}

// HiddenContent reports what Scrub would remove, without editing the
// document. It first loads every object, as Scrub does, so a file whose
// cross-reference table MuPDF has to repair is repaired before anything is
// read, as a save would repair it; PageCount can change (a broken file that
// read as 0 pages can have 1 afterwards), and the counts describe the
// repaired document. See HiddenContent's fields for exactly which locations are read;
// it does not search other references (a structure tree, say) for removed
// content, so a zero result relies on Scrub having destroyed it. When the
// name-tree or action walk hits a limit, the field it feeds fails closed
// (EmbeddedFiles at least 1, JavaScript true); see the fields for the limits.
//
// HiddenContent is new in this fork. It returns an error for non-PDF
// documents.
func (d *Document) HiddenContent() (HiddenContent, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return HiddenContent{}, errors.New("gomupdf: document closed")
	}
	if d.locked {
		return HiddenContent{}, errors.New("gomupdf: hidden content: document is locked")
	}
	c, err := d.b.hiddenContentRaw()
	if err != nil {
		return HiddenContent{}, err
	}
	return hiddenContentFromCounts(c)
}
