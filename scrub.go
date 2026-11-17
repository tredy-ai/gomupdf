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
	// a JavaScript or Launch action. The zero value removes all annotations
	// except Link annotations, all widgets, and /AcroForm.
	KeepAnnotations bool
	// KeepFormFields keeps the form: every widget annotation and the catalog
	// /AcroForm with its whole field tree (fields on no page included), with
	// their scripts removed as under KeepAnnotations. Every other non-Link
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
	Outlines bool // catalog /Outlines

	// EmbeddedFiles is the number of entries in the /Names/EmbeddedFiles name
	// tree, following /Kids. The walk stops at 32 levels, at a /Kids cycle,
	// and after 65,536 units of work (one per node, per /Kids entry and per
	// /Names pair); a tree it cannot walk in full reports the entries it did
	// walk, and at least 1, never 0.
	EmbeddedFiles int
	// AssociatedFiles is the number of /AF arrays on the catalog, on page
	// dictionaries and on intermediate page-tree nodes (the /Pages nodes
	// above a page, up to 32 levels, each counted once).
	AssociatedFiles int

	// JavaScript reports /Names/JavaScript, or a JavaScript or Launch action
	// (directly or through its /Next chain) in the catalog /OpenAction, the
	// catalog /AA, the /AA of a page or intermediate page-tree node, the /AA,
	// /A or /PA of any entry in a page's /Annots (annotations, widgets and
	// Links, including those a default Scrub removes), the /AA or /A of any
	// node of the /AcroForm field tree or of any field above a widget (up
	// /Parent, which reaches fields /Fields leaves out), or the /A of any
	// outline item. It does not know which ScrubOptions a later Scrub uses, so
	// it reports these wherever they are.
	//
	// The field-tree and outline walks stop at 32 levels and at a /Kids or
	// /First cycle, and after 65,536 units of work (one per /Fields or /Kids
	// entry, one per outline item); a walk cut short, or a widget with more
	// than 32 fields above it, counts as JavaScript, and still does after a
	// Scrub that keeps that form or outline. The action walks of one call
	// share a budget of 65,536 action nodes (one per action dictionary or
	// /Next array) and stop at 32 levels of /Next; an action they cannot walk
	// in full (cyclic, too deep, or past the budget) counts as JavaScript. So
	// a page of 65,537 Links with URI actions reports JavaScript before Scrub;
	// Scrub removes the actions past its own budget of the same size, so the
	// reopened output does not on that account.
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
}

// hiddenContentFromCounts maps the backend's counts, in HiddenContent field
// order, to a HiddenContent.
func hiddenContentFromCounts(c []int) (HiddenContent, error) {
	if len(c) != 12 {
		return HiddenContent{}, fmt.Errorf("gomupdf: hidden content: got %d counts, want 12", len(c))
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
	}, nil
}

// Scrub removes document-level content that page redaction never touches:
//
//   - trailer /Info, and catalog /Metadata, /AA, /AF and /PieceInfo;
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
// field tree; in every case it is /Info, the XMP stream,
// page and page-tree node /Metadata and /Thumb streams, the filespecs of
// /Names/EmbeddedFiles and of catalog, page and node /AF, every such
// filespec's /EF streams, the /Names/JavaScript actions with their script
// strings and streams, the nodes of both name trees, and every JavaScript or
// Launch action Scrub removes or removes the holder of (from /OpenAction, any
// /AA, an /A or /PA, an outline item, a removed annotation or field) with its
// /JS script string or stream and its /F filespec. Each is destroyed only if
// it is recognisably of its kind (an /Info dictionary has no /Type, an XMP
// stream has /Type /Metadata or /Subtype /XML, a filespec has /Type
// /Filespec, an action has /S /JavaScript or /S /Launch, and so on).
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
// than through their own /Parent, /P, /Popup, /AA and /SE and an /A or /PA
// that Scrub removes. An object reached as kept content is walked through
// every key, whatever other role it has (page-tree node, kept annotation,
// field, outline item), except on a page or an intermediate page-tree node
// that Scrub edits, where the walk skips the keys Scrub removes there and
// /Annots, and on the catalog, where it skips the keys Scrub removes or edits
// there, /Pages and /StructTreeRoot. A removed entry that points at kept
// content (a /Thumb that the page also draws, an /Info that is the page's
// /Resources, a JavaScript action that is also a graphics state) is only
// unlinked. A field-tree node, outline item or intermediate page-tree node
// that is itself reached as page content (a malformed file) is left as it is,
// scripts included; HiddenContent still reports what it holds.
//
// Everything else is only unlinked too: annotation appearance streams and
// resources, actions other than JavaScript and Launch, /PieceInfo,
// /Outlines, objects not recognisably of the removed kind, and the parts of
// a name tree, field tree or outline beyond 32 levels or 65,536 units of
// walk work, and of a page tree more than 32 levels above a page. Unlinked
// objects leave the file only if nothing else references them. Scrub's
// action walks share one budget of 65,536 action nodes per call, as
// HiddenContent's do; past it, every /A, /PA and /OpenAction it checks
// counts as unsafe and is removed, so on a page of 65,537 Links with URI
// actions the last one loses its URI (and so on, in page order, past that).
//
// Out of scope: /AcroForm /XFA (kept when the form stays; XFA can hold
// scripts and form data), JavaScript reached other than through an action
// Scrub checks (a Rendition action's /JS, a 3D or RichMedia annotation's
// scripts, a javascript: URI), and actions of other kinds that reach outside
// the file (URI, GoToR, GoToE, SubmitForm, ImportData).
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

// HiddenContent reports what Scrub would remove, without changing the
// document. See HiddenContent's fields for exactly which locations are read;
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
