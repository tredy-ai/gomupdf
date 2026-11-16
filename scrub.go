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
	// /AcroForm. The zero value removes all annotations except Link
	// annotations, all widgets, and /AcroForm.
	KeepAnnotations bool
	// KeepOutlines keeps the catalog /Outlines. The zero value removes them and
	// resets /PageMode /UseOutlines to /UseNone.
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
	// AssociatedFiles is the number of /AF arrays on the catalog and on page
	// dictionaries.
	AssociatedFiles int

	// JavaScript reports /Names/JavaScript, or a JavaScript or Launch action
	// (directly or through its /Next chain) in the catalog /OpenAction, the
	// catalog /AA, a page /AA, or the /AA of any entry in a page's /Annots
	// (annotations, widgets and Links). Not checked: /AA of non-widget form
	// fields in the /AcroForm field tree, and /A actions of annotations,
	// widgets and outline items. The action walks of one call share a budget
	// of 65,536 action nodes and stop at 32 levels of /Next; an action they
	// cannot walk in full (cyclic, too deep, or past the budget) counts as
	// JavaScript. So a document with very many safe actions can report
	// JavaScript before Scrub, which removes every /AA it checks.
	JavaScript bool

	// NonLinkAnnotations and Widgets count the entries of every page's /Annots
	// array: Widgets those with /Subtype /Widget, NonLinkAnnotations all other
	// dictionaries except /Subtype /Link (Popups included). An /Annots array
	// shared by several pages counts once per page.
	NonLinkAnnotations, Widgets int

	// Thumbnails and PageMetadata count pages with /Thumb and /Metadata.
	Thumbnails, PageMetadata int
	// PieceInfo counts /PieceInfo dictionaries on the catalog and on pages.
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
//     /UseNone);
//   - on every page dictionary, /Thumb, /AA, /AF, /Metadata and /PieceInfo
//     (values on intermediate /Pages nodes are not touched);
//   - unless opts.KeepAnnotations, every annotation except Links, every
//     widget, and the catalog /AcroForm;
//   - the /AA of every annotation that stays.
//
// Removed objects that may be reachable from elsewhere are destroyed, not
// just unlinked, so a reference that survives (a structure-tree /OBJR or /AF,
// as tagged PDFs carry) resolves to null instead of keeping the content.
// Unless opts.KeepAnnotations, that is the non-Link annotations and widgets
// with their /Popup and their /FS and /AF filespecs, and the /AcroForm
// dictionary with its field tree; in every case it is /Info, the XMP stream,
// page /Metadata and /Thumb streams, the filespecs of /Names/EmbeddedFiles
// and of catalog and page /AF, every such filespec's /EF streams, the
// /Names/JavaScript actions with their script strings and streams, and the
// nodes of both name trees. Each is destroyed only if it is recognisably of
// its kind (an /Info dictionary has no /Type, an XMP stream has /Type
// /Metadata or /Subtype /XML, a filespec has /Type /Filespec, and so on).
//
// Scrub never destroys kept content. Before it changes anything it loads
// every object (so that an xref repair MuPDF needs happens first) and marks
// as kept the catalog and every page-tree node (down /Kids from /Pages, and
// up /Parent from every page); everything referenced, directly or not, from
// each node's /Contents, /Resources and /Group (so the content streams and
// the own and inherited resources of every page), from /Encrypt, from
// /OCProperties and, with opts.KeepAnnotations, from /AcroForm; and the
// annotations that stay (Links, or all of them with opts.KeepAnnotations)
// and, with opts.KeepOutlines, the outline items, with everything they
// reference other than through their own /Parent, /P, /Popup, /AA and /SE.
// An object reached as kept content is walked through every key, whatever
// other role it has (page-tree node, kept annotation, outline item), except
// on a page, where the walk skips the keys Scrub removes there and /Annots,
// and on the catalog, where it skips the keys Scrub removes or edits there,
// /Pages and /StructTreeRoot. A removed entry that points at kept content (a
// /Thumb that the page also draws, an /Info that is the page's /Resources)
// is only unlinked.
//
// Everything else is only unlinked too: annotation appearance streams and
// resources, /AA and /OpenAction actions, /PieceInfo, /Outlines, objects not
// recognisably of the removed kind, and the parts of a name or field tree
// beyond 32 levels or 65,536 units of walk work. Unlinked objects leave the
// file only if nothing else references them.
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
