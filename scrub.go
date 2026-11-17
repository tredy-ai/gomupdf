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
	// clear what it holds. Embedded media in kept annotations stay: a
	// FileAttachment's file, a Sound's sound, a Movie's movie file, a
	// Screen's rendition action (unless it carries /JS) with its media clip
	// and embedded file, the assets of a RichMedia annotation and the 3D
	// stream of a 3D annotation (whose scripts are out of scope, see Scrub).
	// The annotations of a page MuPDF does not serve and other annotations
	// on no page are kept as well; one that only something Scrub removes
	// references (an outline item's GoTo3DView action, without
	// KeepOutlines) is kept by Scrub but has nothing left referencing it, so
	// a garbage-collecting save drops it. The zero value removes all
	// annotations except Link annotations, all widgets, and /AcroForm.
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
	// detect or clear sensitive ones. Hidden template pages (/Type /Template,
	// listed in /Names/Templates) are emptied like any page MuPDF does not
	// serve, whatever the options, so a form that spawns pages from
	// templates spawns blank ones; the scripts that would spawn them are
	// removed anyway. The form keeps its signatures too:
	// with KeepFormFields or KeepAnnotations the catalog /Perms and /DSS
	// stay, and so does every signature field's /V signature dictionary,
	// whose /Contents holds the signer's certificates (a name and often an
	// e-mail address) and whose /Name, /Reason, /Location and /ContactInfo
	// are free text. KeepFormFields is new in this fork.
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
	// /A or /PA of any entry in a page's /Annots or of any annotation or field
	// on no page (see NonLinkAnnotations; annotations, widgets and Links,
	// including those a default Scrub removes), the /AA or /A of any
	// node of the /AcroForm field tree or of any field above a widget (up
	// /Parent, which reaches fields /Fields leaves out) or of any other node
	// of the form on no page (see NonLinkAnnotations), or the /A of any
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
	// shared by several pages counts once per page. They also count the
	// annotations and form fields on no page: indirect dictionaries in no
	// /Annots of a page MuPDF serves (those of an orphan page or of an
	// intermediate /Pages node included) and not in the form (the nodes
	// reached down /Kids from /AcroForm /Fields and from the fields above each
	// widget on a page), which only a structure element's /OBJR or a /Popup or
	// /IRT reaches; Widgets the widgets and fields, NonLinkAnnotations the
	// rest but Links. An annotation is a dictionary with /Type /Annot, or with no
	// /Type, a /Subtype that names an annotation type and a /Rect array; a
	// field is one with no /Type other than /Annot and an /FT name, or a /T
	// string and a /Kids array. One that is also page content (a resource
	// dictionary shaped like an annotation) is not counted: Scrub leaves it
	// as it is. One that a kept annotation's appearance uses (a graphics
	// state shaped like an annotation) is counted, and stays after a Scrub
	// that keeps that annotation.
	NonLinkAnnotations, Widgets int

	// Thumbnails and PageMetadata count pages and intermediate page-tree
	// nodes with /Thumb and /Metadata.
	Thumbnails, PageMetadata int
	// PieceInfo counts /PieceInfo dictionaries on the catalog, on pages and on
	// intermediate page-tree nodes, and the page-piece dictionaries (an
	// untyped dictionary whose every value is a dictionary with /LastModified
	// or /Private) on the objects ObjectMetadata lists. Applications keep
	// private data there; Illustrator keeps a whole private copy of the
	// artwork, text included.
	PieceInfo int

	// TrailerKeys counts the keys of the trailer other than /Root, /Info,
	// /ID, /Encrypt, /Size, /Prev and /XRefStm and, when the trailer is a
	// cross-reference stream's dictionary, that stream's own /Type, /W,
	// /Index, /Filter, /DecodeParms and /Length. A save never writes them,
	// but a garbage-collecting save keeps everything they reference, as
	// objects nothing in the saved file references (qpdf's /QTest, say, can
	// hold a page's content stream from before a redaction).
	TrailerKeys int

	// OrphanPages counts the page dictionaries that are not pages MuPDF
	// serves (the PageCount pages) but still hold content (any key Scrub
	// removes from them, /Parent included; not /Annots, whose entries count
	// as annotations on no page). A page dictionary is one with
	// /Type /Page or /Type /Template (a template /Names/Templates lists), or
	// with no /Type, a /MediaBox and a /Contents or /Parent; one with /Kids
	// is a page-tree node and never counts. A page outside the page tree
	// counts, and so does one in it that MuPDF does not serve (a /Kids entry
	// past a /Count that is too small). Such a page is never extracted,
	// rendered or redacted, while a structure element's /Pg, a destination
	// or a template can still reach it. One that a served page has above it,
	// up /Parent, counts only for the keys other than /Resources and
	// /Parent, which that page inherits.
	OrphanPages int

	// ObjectMetadata counts the objects other than the catalog, pages and
	// page-tree nodes whose /Metadata is an XMP stream (/Type /Metadata or
	// /Subtype /XML): image and form XObjects, fonts (/Type /Font), font
	// descriptors, structure elements (/Type /StructElem), output intents,
	// ICC profile streams (no /Type or /Subtype, an integer /N) and embedded
	// files (/Type /EmbeddedFile), the objects on which /Metadata, /PieceInfo
	// and /LastModified have their documented meaning. Only these objects are
	// read: elsewhere, in a resource dictionary say, the same key can be a
	// resource name.
	ObjectMetadata int
	// LastModified counts /LastModified dates (strings) on the catalog, on
	// pages and intermediate page-tree nodes and on the objects ObjectMetadata
	// lists.
	LastModified int
	// Perms and DSS report the catalog /Perms (DocMDP and UR3 signature
	// dictionaries) and /DSS (the document security store: certificates,
	// OCSP responses and CRLs, which hold names and e-mail addresses).
	Perms, DSS bool
	// WebCapture reports web-capture data: the catalog /SpiderInfo, or the
	// /Names/IDS or /Names/URLS name tree.
	WebCapture bool

	// Layers is the number of optional-content groups the catalog
	// /OCProperties lists in /OCGs, 0 without /OCProperties. With Layers 0
	// MuPDF hides nothing, whatever the file's content says.
	//
	// HiddenLayers is 0 when Layers is 0. Otherwise it is the number of
	// optional-content groups and membership dictionaries (/Type /OCG and
	// /Type /OCMD dictionaries, listed or not) that MuPDF's default
	// configuration hides for viewing: an /OFF group, a group whose /Usage
	// /View /ViewState is /OFF or whose /Intent the configuration leaves out,
	// and a membership dictionary whose /P policy evaluates to hidden. It
	// reads every indirect one (in an object stream too) and the direct ones
	// written as an object's own value (an annotation's or XObject's /OC, an
	// entry of an indirect /Properties dictionary) or as an entry of a
	// direct /Properties dictionary of an object or of its direct /Resources;
	// each indirect one counts once, each direct one once per place. It fails
	// closed: it is at least 1 when any of those is a membership dictionary,
	// a group with /Usage or with an /Intent other than /View, or a direct
	// one, even if it shows. HiddenLayers 0 with Layers non-zero therefore
	// means that every group and membership dictionary those places hold is
	// an indirect plain group the configuration shows. It does not read
	// content streams: a group or membership dictionary written inline as a
	// marked-content property list (/OC << ... >> BDC) is not seen, and MuPDF
	// honours it. Only Layers 0 rules out hidden content; a caller that must
	// not miss any calls Document.ShowAllLayers whenever Layers is non-zero.
	//
	// Content marked with a hidden group is neither extracted, searched,
	// rendered nor redacted, but stays in the file, and a viewer's user can
	// switch it on. MuPDF does not evaluate a /VE visibility expression and
	// shows its content (so it is extracted too); a viewer that evaluates it
	// may hide it. Scrub leaves layers alone: call Document.ShowAllLayers
	// before extraction to make all of it ordinary content, after which both
	// read 0.
	Layers, HiddenLayers int
}

// hiddenContentCounts is the number of counts the backend returns.
const hiddenContentCounts = 22

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
		ObjectMetadata:     c[15],
		LastModified:       c[16],
		Perms:              c[17] != 0,
		DSS:                c[18] != 0,
		WebCapture:         c[19] != 0,
		Layers:             c[20],
		HiddenLayers:       c[21],
	}, nil
}

// Scrub removes document-level content that page redaction never touches:
//
//   - trailer /Info and every non-standard trailer key (see HiddenContent's
//     TrailerKeys), and catalog /Metadata, /AA, /AF, /PieceInfo,
//     /LastModified and the web-capture /SpiderInfo;
//   - /Names/EmbeddedFiles, /Names/JavaScript and the web-capture
//     /Names/IDS and /Names/URLS;
//   - unless opts.KeepAnnotations or opts.KeepFormFields, the catalog /Perms
//     (DocMDP and UR3 signature dictionaries) and /DSS (certificates, OCSP
//     responses and CRLs); see KeepFormFields for what a kept form keeps;
//   - the catalog /OpenAction, if it or any action in its /Next chain is a
//     JavaScript or Launch action (a destination or other action is kept);
//   - unless opts.KeepOutlines, /Outlines (and /PageMode /UseOutlines becomes
//     /UseNone); with it, the /A of every outline item that is or chains to a
//     JavaScript or Launch action (the item, its title and any /Dest stay);
//   - on every page dictionary and every intermediate page-tree node above a
//     page (up to 32 levels), /Thumb, /AA, /AF, /Metadata, /PieceInfo and
//     /LastModified;
//   - on every object HiddenContent's ObjectMetadata lists (images, forms,
//     fonts, structure elements and so on), /Metadata when it is an XMP
//     stream, /PieceInfo when it is a page-piece dictionary and /LastModified
//     when it is a string, edited in place even on an image or form the page
//     draws, since these keys play no part in drawing; the same names in a
//     resource dictionary are resource names and are not touched;
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
//   - on every page dictionary that is not a page MuPDF serves (see
//     HiddenContent's OrphanPages; a structure element's /Pg, a destination
//     or /Names/Templates can still reach one), /Contents, /Resources, /B,
//     /Group, /StructParents, /Parent and the keys removed from pages above,
//     and the same on each /Type /Pages node outside the page tree it
//     inherits from (32 levels); the dictionary itself stays, with its /Type,
//     /MediaBox and /Annots, for those references, and its annotations are
//     annotations on no page (below). One that a page MuPDF serves has above
//     it, up /Parent, keeps /Resources and /Parent, which that page inherits;
//   - every annotation and form field on no page and not in the form (see
//     HiddenContent's NonLinkAnnotations; pdf.js bug1722036.pdf has widgets
//     only its structure tree reaches) that the options would remove from a
//     page: by default all but Links, with opts.KeepFormFields all but Links,
//     widgets and fields, with opts.KeepAnnotations none; a field in the
//     form but on no page goes or stays with the form;
//   - on every annotation that stays, on a page or not, /AA, and each of /A and /PA that is or
//     chains through /Next to a JavaScript or Launch action; when the form
//     stays (opts.KeepAnnotations or opts.KeepFormFields), the same on every
//     node of the /AcroForm field tree (fields with /Kids as well as widgets,
//     edited in place so each stays the object /Fields and /Annots list) and
//     on every field above a kept widget up /Parent, which reaches fields
//     /Fields leaves out (32 levels), and on every other node of the form on
//     no page (down /Kids from those fields: a page widget's sibling, say).
//     The annotation or field stays: a Link that loses its
//     /A is inert. A URI, GoTo, GoToR, Named or other action stays as it was.
//
// Removed objects that may be reachable from elsewhere are destroyed, not
// just unlinked, so a reference that survives (a structure-tree /OBJR or /AF,
// as tagged PDFs carry) resolves to null instead of keeping the content (to
// an empty dictionary for an object created since the document was opened,
// such as an annotation added through this package, which MuPDF 1.25 could
// otherwise no longer save).
// Unless opts.KeepAnnotations, that is the removed annotations, on a page or
// not (and widgets and fields, unless opts.KeepFormFields), with their
// /Popup and their /FS and /AF
// filespecs, and, unless the form stays, the /AcroForm dictionary with its
// field tree; in every case it is the content streams of a page MuPDF does
// not serve, the forms and images its own and inherited /Resources /XObject
// reach, its tiling patterns (/Pattern), the glyph procedures of its Type 3
// fonts (/Font) and its soft-mask groups (/ExtGState /SMask /G, and that of
// a shading pattern's /ExtGState), down
// through the /Resources of each of those at any depth, and the annotations
// on it that the options remove as on a page, /Info, every
// removed /Metadata XMP stream, every removed /PieceInfo page-piece
// dictionary with its data dictionaries, their /Private values and the
// untyped streams a /Private dictionary holds, unless the form stays the
// /Perms signature dictionaries and the /DSS with the streams of its /Certs,
// /OCSPs and /CRLs, /SpiderInfo, the web-capture content sets of /Names/IDS
// and /Names/URLS with their source information, page and page-tree node
// /Thumb streams, the filespecs of
// /Names/EmbeddedFiles and of every /AF Scrub removes, every such
// filespec's /EF streams, the /Names/JavaScript actions with their script
// strings and streams, the nodes of the removed name trees, the /XFA streams, and
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
// other than /XObject, a signature dictionary has /Type /Sig or
// /DocTimeStamp, and so on).
//
// Scrub never destroys kept content. Before it changes anything it loads
// every object (so that an xref repair MuPDF needs happens first) and marks
// as kept the catalog and every page-tree node (down /Kids from /Pages, where
// a page dictionary MuPDF does not serve is an orphan page, not a node, and
// up /Parent from every page MuPDF serves, where any dictionary is a node);
// everything referenced, directly or not, from each node's /Contents,
// /Resources and /Group (so the content streams and the own and inherited
// resources of every page; of a page dictionary that is not a page MuPDF
// serves, only /Resources), from /Encrypt and from
// /OCProperties; and the annotations that stay (Links, widgets with
// opts.KeepFormFields, or all of them with opts.KeepAnnotations), when the
// form stays the /AcroForm dictionary and its field tree (through /Fields,
// /Kids and /CO), and with
// opts.KeepOutlines the outline items, with everything they reference other
// than through their own /Parent, /P, /Popup, /AA, /SE and /XFA and an /A or
// /PA that Scrub removes; what a kept annotation's /AP reaches is kept
// whatever it is, for an annotation or field on no page that Scrub keeps too
// (an orphan page's resources may list the same appearance). An object reached as kept content is walked through
// every key, whatever other role it has (page-tree node, kept annotation,
// field, outline item), except on a page or an intermediate page-tree node
// that Scrub edits, where the walk skips the keys Scrub removes there and
// /Annots, on the catalog, where it skips the keys Scrub removes or edits
// there, /Pages and /StructTreeRoot, on the objects ObjectMetadata lists the
// /Metadata, /PieceInfo and /LastModified Scrub removes there, and on any
// object the file specifications in an /AF array (an image the page draws
// may carry one; Scrub removes them in place, which does not change the
// drawing, and a resource named /AF never holds a file specification). A
// kept form keeps /Perms and /DSS with all they reference. A page dictionary
// that is not a page MuPDF serves and is reached other than as page content (a
// kept Link's or outline item's destination) is kept but not walked, so what
// it holds is still destroyed. Outside page content and kept appearances, an
// annotation or form field is kept only in its own right (a kept annotation,
// a node of a kept form), not because something kept points at it: a kept
// Link's or /OpenAction's Hide action whose /T names an annotation Scrub
// removes keeps the action, and the annotation is destroyed, as is one a
// kept annotation's /IRT, /Dest or other key names. A removed entry that points at kept
// content (a /Thumb that the page also draws, an /Info that is the page's
// /Resources, a JavaScript action that is also a graphics state, an action
// that a kept /A or a kept /OpenAction also uses) is only unlinked. A
// field-tree node, outline item or intermediate page-tree node
// that is itself reached as page content (a malformed file) is left as it is,
// scripts included; HiddenContent still reports what it holds.
//
// Everything else is only unlinked too: annotation appearance streams and
// resources, what a non-standard trailer key references, actions other than
// JavaScript and Launch, the /VRI entries of /DSS, the pages and images a
// web-capture content set lists, /Outlines, objects not recognisably of the
// removed kind, and the parts of
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
// field-tree node, outline item or page-tree node that is also page content,
// and an annotation, field or page dictionary on no page that is also page
// content; one of those that is a direct object nested in another (Scrub
// finds them among the indirect objects); text in structure elements, name
// trees and other document-level dictionaries Scrub does not list here; and
// optional content (layers): Scrub keeps /OCProperties as page content, so
// content a layer hides stays hidden from extraction and in the file. Call
// Document.ShowAllLayers before extraction to make it ordinary content, and
// see HiddenContent's Layers and HiddenLayers.
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
		return errLocked
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
// content, so a zero result relies on Scrub having destroyed it. Between
// Page.ApplyRedactions and Scrub (or a save and reopen) it counts each
// applied redaction annotation, which MuPDF unlinks from the page but does
// not delete, as an annotation on no page (NonLinkAnnotations). When the
// name-tree or action walk hits a limit, the field it feeds fails closed
// (EmbeddedFiles at least 1, JavaScript true); see the fields for the limits.
// The pages it checks are those MuPDF serves: a page MuPDF cannot look up in a
// damaged or cyclic page tree (and cannot load either) is skipped, not an
// error, as is a /Kids entry naming a missing object (which MuPDF serves as
// an empty page), and a page dictionary it never serves counts as an orphan
// page. A page tree that claims more pages than it can serve is refused as
// Open refuses it (see Open), before any page is looked up, and
// HiddenContent and Scrub return that error.
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
		return HiddenContent{}, errLocked
	}
	c, err := d.b.hiddenContentRaw()
	if err != nil {
		return HiddenContent{}, err
	}
	return hiddenContentFromCounts(c)
}
