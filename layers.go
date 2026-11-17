package gomupdf

import "errors"

// ShowAllLayers makes all optional content (layers) ordinary, visible
// content. Content that the document's default optional-content
// configuration hides (a group listed in /OFF, a group whose usage or intent
// hides it, a membership dictionary whose policy evaluates to hidden; see
// HiddenContent's Layers and HiddenLayers) is otherwise never extracted,
// searched, rendered or redacted, and a quad redaction can never target it,
// while it stays in the file and a viewer's user can switch it on. A
// redaction even copies it unchanged into the page's new content unless a
// redaction area happens to cover it.
//
// ShowAllLayers deletes the catalog /OCProperties, then destroys every
// indirect optional-content group and membership dictionary (/Type /OCG or
// /Type /OCMD, and no keys other than those such a dictionary has), so that
// their names (/Name) do not stay in a saved file; the /Properties resources,
// /OC entries and marked-content spans that referred to them stay and now
// refer to nothing, which makes the content visible. Switching layers on one
// by one would not do: usage- and policy-hidden content would stay hidden.
// After it, extraction, search, rendering and redaction in this Document and
// in a saved and reopened copy see the formerly hidden content.
//
// Call it before extraction and detection, or they miss what it reveals. The
// page then shows every layer at once (all language alternatives of a
// multilingual page drawn over each other, say), and annotations with /OC
// become visible too. Scrub does not touch layers; without ShowAllLayers,
// hidden layer content survives redaction and Scrub.
//
// ShowAllLayers is new in this fork. A document without optional content is
// left as it is. It returns an error for non-PDF documents.
func (d *Document) ShowAllLayers() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	if d.locked {
		return errLocked
	}
	return d.b.showAllLayers()
}
