// Package gomupdf is a focused binding for reading, extracting, rendering, and
// writing PDFs. It exposes a compact API: open a (possibly encrypted) PDF from a
// file or from memory, authenticate, and pull reading-order text and positioned
// geometry per page, along with rendering and document-assembly helpers.
//
// The public types here (Document, Page, Font and the result types) are
// backend-neutral; every operation that touches a native engine is delegated to
// the interfaces in backend.go. The default engine is MuPDF (cgo), compiled in
// unless the binary is built with `-tags nomupdf`.
//
// Concurrency: a Document binds one backend and is guarded by a mutex; do not
// share a Document across goroutines without it. Always Close a Document.
package gomupdf

import (
	"errors"
	"iter"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/tredy-ai/gomupdf/geometry"
)

// ---------------------------------------------------------------------------
// Document and page API.
//
// A Document is opened from a file (Open) or from in-memory bytes (OpenStream),
// optionally unlocked with Authenticate, and queried via NeedsPass, PageCount,
// LoadPage, and Pages. Each Page exposes text and geometry extraction. Always
// Close a Document when done.
// ---------------------------------------------------------------------------

// Document is an open PDF. Methods are serialized internally; a Document binds
// one backend. Always Close it.
type Document struct {
	mu        sync.Mutex
	b         docBackend
	locked    bool   // encrypted and not yet authenticated
	encrypted bool   // whether the PDF was password-protected at open
	path      string // file the document was opened from by Open; "" otherwise
	badScrub  bool   // a Scrub failed on the document; see Scrub
	badRedact bool   // ApplyRedactions failed after changing a page; see ApplyRedactions
}

// Open opens a PDF document from a file path.
//
// Unlike upstream, Open does not read the file into memory: MuPDF keeps the
// file open and reads it on demand until Close. The file MUST NOT be modified
// while the Document is open (unlinking it is fine on Linux). Because there is
// no in-memory original, an incremental save needs a document opened with
// OpenStream.
//
// Also changed from upstream: every object of a PDF is loaded once at open
// (for a password-locked PDF, when Authenticate succeeds), here and in
// OpenStream and OpenAnyStream. MuPDF repairs a damaged cross-reference
// table lazily, the first time an object fails to load, by rebuilding it
// from the file; a repair after open could change the page count and the
// pages (a file can have no pages until it is repaired) and discards every
// change made before it, so a redaction or an edit reported as done would
// not be in the saved file. Loading everything makes any repair happen
// first. It costs one parse of each object (streams are not decoded),
// about 1µs and 0.5KB per object: 39ms and 17MB for a file with 40000
// objects. ApplyRedactions, AddRedaction, AddRedactionQuads and every save
// return an error if a repair still happens during the call.
//
// Also changed: a document that needs a password (NeedsPass) answers only
// IsEncrypted, NeedsPass, Authenticate and Close until Authenticate
// succeeds. Every method that reads its content (text, metadata, XMP, TOC,
// page labels, page geometry, fonts, images, drawings, links, annotations,
// widgets, rendering, hidden content, conversion) returns an error, as
// every change and save already did; LoadPage returns an error, PageCount
// returns 0, and Pages yields nothing. MuPDF decrypts each object once,
// when it is first loaded, and keeps it: one read before Authenticate
// (stock MuPDF decrypts it with the wrong key) leaves that object's
// strings garbled for good.
//
// Also new in this fork, here and in OpenStream and OpenAnyStream (or, for a
// password-locked PDF, in Authenticate, which then fails): a page tree that
// claims more pages than it can serve is refused with an error that names
// the rule and the numbers ("page tree declares N pages, ..."). A declared
// page count within the file's number of objects is accepted as before, at
// no cost. A larger one, possible only with direct page dictionaries in
// /Kids or a page or node listed more than once, is accepted only if it is
// at most 1,048,576 and MuPDF can build its page map from the tree with
// every page in it a dictionary; MuPDF then serves the pages the tree holds.
// A count above 8,388,607 (MuPDF's maximum object number) is refused
// whatever the file. MuPDF's page map takes 16 bytes per declared page (16
// MiB at 1,048,576; 22 GB for pdf.js poppler-67295-0.pdf's /Count
// 1,410,065,407), and a count it cannot serve (a missing /Kids entry, a
// cycle) would otherwise cost a page lookup in every page loop, per declared
// page.
func Open(filename string) (*Document, error) {
	if defaultDriver == nil {
		return nil, errNoBackend
	}
	if strings.IndexByte(filename, 0) >= 0 {
		return nil, errors.New("gomupdf: path contains a NUL byte")
	}
	b, needsPass, err := defaultDriver.openPath(filename)
	if err != nil {
		return nil, err
	}
	d := &Document{b: b, locked: needsPass, encrypted: needsPass, path: filename}
	runtime.SetFinalizer(d, (*Document).Close)
	return d, nil
}

// OpenStream opens a PDF from in-memory bytes. It does
// not fail on encryption; the returned Document is left locked (NeedsPass()
// true) until Authenticate succeeds, and until then answers nothing but
// IsEncrypted, NeedsPass, Authenticate and Close (see Open). Like Open, it
// refuses a page tree that claims more pages than it can serve.
func OpenStream(pdf []byte) (*Document, error) {
	if len(pdf) == 0 {
		return nil, errors.New("gomupdf: empty input")
	}
	if defaultDriver == nil {
		return nil, errNoBackend
	}
	b, needsPass, err := defaultDriver.open(pdf, ".pdf")
	if err != nil {
		return nil, err
	}
	d := &Document{b: b, locked: needsPass, encrypted: needsPass}
	runtime.SetFinalizer(d, (*Document).Close)
	return d, nil
}

// OpenWithPassword opens a file by path, like Open, and authenticates in one step, returning an
// error if the password is wrong (QoL over Open + Authenticate).
func OpenWithPassword(filename, password string) (*Document, error) {
	d, err := Open(filename)
	if err != nil {
		return nil, err
	}
	if d.NeedsPass() && !d.Authenticate(password) {
		d.Close()
		return nil, errors.New("gomupdf: incorrect password")
	}
	return d, nil
}

// OpenStreamWithPassword is OpenWithPassword for in-memory bytes.
func OpenStreamWithPassword(pdf []byte, password string) (*Document, error) {
	d, err := OpenStream(pdf)
	if err != nil {
		return nil, err
	}
	if d.NeedsPass() && !d.Authenticate(password) {
		d.Close()
		return nil, errors.New("gomupdf: incorrect password")
	}
	return d, nil
}

// IsEncrypted reports whether the PDF was password-protected (stays true even
// after successful authentication, unlike NeedsPass).
func (d *Document) IsEncrypted() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.encrypted
}

// NeedsPass reports whether the document is encrypted and not yet unlocked.
// Unlike MuPDF's static flag, this flips to false once Authenticate succeeds.
func (d *Document) NeedsPass() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.locked
}

// Authenticate tries a password and returns true if the document is now
// readable (correct password, or it was never locked).
//
// Changed from upstream: on success it loads every object of the document
// (see Open); if that fails (only running out of memory can), it returns
// false and the document stays locked. It also returns false, and the
// document stays locked, for a page tree that claims more pages than it can
// serve (see Open). While a document is locked, every method that reads,
// changes or saves it returns an error (see Open).
func (d *Document) Authenticate(password string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return false
	}
	ok := d.b.authenticate(password)
	if ok {
		d.locked = false
	}
	return ok
}

// PageCount returns the number of pages. Returns 0 if the
// document is closed or unreadable.
//
// Changed from upstream: it also returns 0 while the document is locked
// (see Open): counting the pages loads the catalog, and a string in it
// decrypted with the wrong key would stay wrong after Authenticate.
func (d *Document) PageCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return 0
	}
	return d.b.pageCount()
}

// LoadPage returns a handle to page i (0-based).
// The page is lightweight (it defers to the document on each call); no separate
// Close is required. A locked document (see Open) returns an error.
func (d *Document) LoadPage(i int) (*Page, error) {
	if d.NeedsPass() {
		return nil, errLocked
	}
	if i < 0 || i >= d.PageCount() {
		return nil, errors.New("gomupdf: page out of range")
	}
	return &Page{doc: d, Number: i}, nil
}

// Pages iterates pages in order. Use as:
//
//	for i, page := range doc.Pages() { ... }
func (d *Document) Pages() iter.Seq2[int, *Page] {
	return func(yield func(int, *Page) bool) {
		n := d.PageCount()
		for i := 0; i < n; i++ {
			if !yield(i, &Page{doc: d, Number: i}) {
				return
			}
		}
	}
}

// rawText pulls page i's reading-order text (lines '\n'-joined).
func (d *Document) rawText(i int) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return "", errors.New("gomupdf: document closed")
	}
	return d.b.textRaw(i)
}

// Close releases all native resources. Safe to call twice.
func (d *Document) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b != nil {
		d.b.close()
		d.b = nil
	}
	runtime.SetFinalizer(d, nil)
}

// Page is a handle to a single page.
type Page struct {
	doc    *Document
	Number int // 0-based page index
}

// GetText returns the page's reading-order plain text. It ignores /ActualText, with no toggle (see TextOptions.UseActualText).
func (p *Page) GetText() (string, error) {
	return p.doc.rawText(p.Number)
}

// Bound returns the page's bounding box in points.
func (p *Page) Bound() (geometry.Rect, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return geometry.Rect{}, errors.New("gomupdf: document closed")
	}
	raw, err := d.b.boundRaw(p.Number)
	if err != nil {
		return geometry.Rect{}, err
	}
	f := strings.Fields(raw)
	if len(f) != 4 {
		return geometry.Rect{}, errors.New("gomupdf: bad bound output")
	}
	x0, _ := strconv.ParseFloat(f[0], 64)
	y0, _ := strconv.ParseFloat(f[1], 64)
	x1, _ := strconv.ParseFloat(f[2], 64)
	y1, _ := strconv.ParseFloat(f[3], 64)
	return geometry.Rect{X0: x0, Y0: y0, X1: x1, Y1: y1}, nil
}

// --- write API (Go entry points; backend does the work) --------------------

// NewPDF creates a new, empty PDF document.
func NewPDF() (*Document, error) {
	if defaultDriver == nil {
		return nil, errNoBackend
	}
	b, err := defaultDriver.newPDF()
	if err != nil {
		return nil, err
	}
	d := &Document{b: b}
	runtime.SetFinalizer(d, (*Document).Close)
	return d, nil
}

// SaveBytes serializes the document to PDF bytes. garbage collects unused objects.
func (d *Document) SaveBytes(garbage bool) ([]byte, error) {
	opts := SaveOptions{}
	if garbage {
		opts.Garbage = 1
	}
	return d.SaveBytesWithOptions(opts)
}

// Save writes the document to a PDF file.
func (d *Document) Save(path string, garbage bool) error {
	if err := d.checkNotSource(path); err != nil {
		return err
	}
	data, err := d.SaveBytes(garbage)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// SaveEncryptedBytes serializes the document with AES-256 encryption. Encrypted
// saves are buffered: the whole output is built in memory. userPwd
// is required to open; ownerPwd grants full permissions (may be "").
func (d *Document) SaveEncryptedBytes(userPwd, ownerPwd string) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return nil, errors.New("gomupdf: document closed")
	}
	if err := d.redactionFailed("save encrypted"); err != nil {
		return nil, err
	}
	return d.b.saveEncrypted(userPwd, ownerPwd)
}

// NewPage appends a blank page of the given size (points).
func (d *Document) NewPage(width, height float64) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	return d.b.newPage(width, height)
}

// DeletePage removes page n (0-based).
func (d *Document) DeletePage(n int) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	return d.b.deletePage(n)
}

// InsertPDF appends every page of the source PDF (given as bytes) to this
// document. password unlocks an encrypted source ("" if none).
func (d *Document) InsertPDF(src []byte, password string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	if len(src) == 0 {
		return errors.New("gomupdf: empty source")
	}
	return d.b.insertPDF(src, password)
}

// InsertText draws a line of Helvetica text at (x, y) (baseline) on page i.
func (p *Page) InsertText(x, y, size float64, text string) error {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	return d.b.insertText(p.Number, x, y, size, text)
}

// InsertImage places an encoded image (jpeg/png/…) into rect r on page i.
func (p *Page) InsertImage(r geometry.Rect, img []byte) error {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	if len(img) == 0 {
		return errors.New("gomupdf: empty image")
	}
	return d.b.insertImage(p.Number, [4]float64{r.X0, r.Y0, r.X1, r.Y1}, img)
}

// AddRectAnnot adds a rectangle (square) annotation on page i.
func (p *Page) AddRectAnnot(r geometry.Rect) error {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	return d.b.addRectAnnot(p.Number, [4]float64{r.X0, r.Y0, r.X1, r.Y1})
}

// imagesRaw returns the image-metadata trace output (IMG ... lines).
func (p *Page) imagesRaw() (string, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return "", errors.New("gomupdf: document closed")
	}
	return d.b.imagesRaw(p.Number)
}

// imageBytesRaw returns the encoded bytes + extension of the index-th image.
func (p *Page) imageBytesRaw(index int) ([]byte, string, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return nil, "", errors.New("gomupdf: document closed")
	}
	return d.b.imageBytes(p.Number, index)
}

// drawingsRaw returns the trace-device output (P/m/l/c/h lines).
func (p *Page) drawingsRaw() (string, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return "", errors.New("gomupdf: document closed")
	}
	return d.b.drawingsRaw(p.Number)
}

// tocRaw returns the outline output ("level\tpage\ttitle" per line).
func (d *Document) tocRaw() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return "", errors.New("gomupdf: document closed")
	}
	return d.b.tocRaw()
}

// linksRaw returns the link output ("x0 y0 x1 y1\turi" per line).
func (p *Page) linksRaw() (string, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return "", errors.New("gomupdf: document closed")
	}
	return d.b.linksRaw(p.Number)
}

// searchRaw returns the search output (one hit per line: 8 quad floats).
func (p *Page) searchRaw(needle string) (string, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return "", errors.New("gomupdf: document closed")
	}
	return d.b.searchRaw(p.Number, needle)
}

// wordsRaw returns the word-extraction output (one word per line:
// "x0 y0 x1 y1 block line\t<text>"). Parsed by Page.Words.
func (p *Page) wordsRaw() (string, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return "", errors.New("gomupdf: document closed")
	}
	return d.b.wordsRaw(p.Number)
}

// StructuredJSON returns structured text as JSON (blocks → lines → chars, each
// with bbox). Ignores /ActualText (no toggle; see TextOptions.UseActualText). This is the raw geometry feed for layout reconstruction; see
// Words/Blocks for typed access.
func (p *Page) StructuredJSON() (string, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return "", errors.New("gomupdf: document closed")
	}
	return d.b.structuredJSON(p.Number)
}

const softHyphen = "­" // U+00AD

// Lines returns the page's non-empty lines with the soft hyphen (U+00AD)
// stripped and trailing whitespace trimmed. It ignores /ActualText, with no toggle (see TextOptions.UseActualText).
func (p *Page) Lines() ([]string, error) {
	raw, err := p.GetText()
	if err != nil {
		return nil, err
	}
	return splitLines(raw), nil
}

func splitLines(raw string) []string {
	raw = strings.ReplaceAll(raw, softHyphen, "")
	var out []string
	for _, ln := range strings.Split(raw, "\n") {
		if strings.TrimSpace(ln) == "" {
			continue
		}
		out = append(out, strings.TrimRight(ln, " \t\r"))
	}
	return out
}

// AllLines returns every page's Lines concatenated into a single flat line
// stream across the whole document.
func (d *Document) AllLines() ([]string, error) {
	if d.NeedsPass() {
		return nil, errLocked
	}
	var out []string
	for _, page := range d.Pages() {
		lines, err := page.Lines()
		if err != nil {
			return nil, err
		}
		out = append(out, lines...)
	}
	return out, nil
}

// Text returns the full document text, pages separated by a form feed ("\f").
func (d *Document) Text() (string, error) {
	if d.NeedsPass() {
		return "", errLocked
	}
	var b strings.Builder
	for i, page := range d.Pages() {
		if i > 0 {
			b.WriteByte('\f')
		}
		t, err := page.GetText()
		if err != nil {
			return "", err
		}
		b.WriteString(t)
	}
	return b.String(), nil
}

// TextByPage returns each page's text as a slice.
func (d *Document) TextByPage() ([]string, error) {
	if d.NeedsPass() {
		return nil, errLocked
	}
	var out []string
	for _, page := range d.Pages() {
		t, err := page.GetText()
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// metaKeys are the standard metadata lookups exposed by Metadata.
var metaKeys = map[string]string{
	"format":       "format",
	"encryption":   "encryption",
	"title":        "info:Title",
	"author":       "info:Author",
	"subject":      "info:Subject",
	"keywords":     "info:Keywords",
	"creator":      "info:Creator",
	"producer":     "info:Producer",
	"creationDate": "info:CreationDate",
	"modDate":      "info:ModDate",
}

// Metadata returns document metadata (title, author, format, …). Absent keys are
// omitted.
func (d *Document) Metadata() (map[string]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return nil, errors.New("gomupdf: document closed")
	}
	out := make(map[string]string)
	for name, key := range metaKeys {
		v, ok, err := d.b.lookupMeta(key)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if v = strings.TrimSpace(v); v != "" {
			out[name] = v
		}
	}
	return out, nil
}
