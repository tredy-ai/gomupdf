package gomupdf

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
)

// Save options: fine-grained control over PDF serialization, mirroring the
// knobs PyMuPDF exposes on Document.save (garbage collection level, stream
// compression, linearization, pretty-printing, ASCII output, encryption, and
// incremental save).

// SaveOptions configures how a Document is serialized. The zero value writes an
// uncompressed, un-garbage-collected PDF; DefaultSaveOptions returns sensible
// production defaults. Encryption fields are honored only when Encrypt is true.
type SaveOptions struct {
	// Garbage is the dead-object collection level, 0–4:
	//  0 none, 1 collect unreferenced, 2 + compact xref, 3 + merge duplicates,
	//  4 + dedupe identical streams. Out-of-range values are clamped.
	Garbage int

	Deflate       bool // compress uncompressed streams (FlateDecode)
	DeflateImages bool // also recompress image streams
	DeflateFonts  bool // also recompress embedded font streams

	Clean  bool // sanitize and rewrite content streams
	Linear bool // linearize ("fast web view")
	ASCII  bool // emit only ASCII (escape binary)
	Pretty bool // pretty-print PDF objects

	Encrypt       bool   // serialize with AES-256 encryption
	UserPassword  string // required to open (when Encrypt)
	OwnerPassword string // grants full permissions (when Encrypt)
	Permissions   int    // permission bitmask; -1 grants all

	// Incremental appends changes to the original bytes instead of rewriting
	// the whole file. Valid only for documents opened with OpenStream;
	// incompatible with Garbage, Linear, and Encrypt. A file MuPDF had to
	// repair cannot be saved incrementally; since OpenStream loads every
	// object (see Open), that now includes a file whose damage is only in
	// objects nothing else reads (changed from upstream).
	Incremental bool
}

// DefaultSaveOptions returns options suitable for most documents: garbage level
// 3 (merge duplicates) with stream deflation enabled.
func DefaultSaveOptions() SaveOptions {
	return SaveOptions{Garbage: 3, Deflate: true}
}

// SaveBytesWithOptions serializes the document to PDF bytes using opts.
func (d *Document) SaveBytesWithOptions(opts SaveOptions) ([]byte, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return nil, errors.New("gomupdf: document closed")
	}
	return d.b.save(opts, nil)
}

// SaveWithOptions writes the document to a PDF file using opts.
//
// Unlike upstream, a non-incremental, unencrypted save is streamed by MuPDF
// straight to a temporary file (".gomupdf-*") in path's directory, which is
// renamed over path on success and removed on failure, so the output is never
// held in memory and a failed save never leaves a partial file at path.
// Incremental saves and saves with opts.Encrypt stay buffered in memory, as
// before.
//
// The result's mode is that of the file it replaces, or 0644 with the process
// umask applied for a new file, matching the old os.WriteFile(path, data,
// 0o644): under umask 077 a new file is 0600.
// If the document was opened by Open from the same file as path, an error is
// returned and nothing is written: MuPDF is still reading that file lazily.
// This holds for the buffered (Encrypt, Incremental) paths too. A destination
// that is a symlink is written through, as os.WriteFile did; a dangling one is
// an error.
func (d *Document) SaveWithOptions(path string, opts SaveOptions) error {
	if opts.Incremental || opts.Encrypt {
		if err := d.checkNotSource(path); err != nil {
			return err
		}
		data, err := d.SaveBytesWithOptions(opts)
		if err != nil {
			return err
		}
		return os.WriteFile(path, data, 0o644)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return errors.New("gomupdf: document closed")
	}
	if err := d.checkNotSource(path); err != nil {
		return err
	}
	// Like os.WriteFile, write through a symlink: rename over the link's
	// target, not the link. A dangling symlink is an error (upstream would
	// have created its target; we refuse rather than guess the target path).
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		r, err := filepath.EvalSymlinks(path)
		if err != nil {
			return fmt.Errorf("gomupdf: save destination is a dangling symlink: %w", err)
		}
		path = r
	}
	// A new destination's temp file is created 0644 so the kernel applies the
	// umask (no process-global syscall.Umask). An existing destination's is
	// created 0600, so the document is never more readable than before, and
	// gets the destination's mode just before the rename.
	_, statErr := os.Stat(path)
	tmp, err := createSaveTemp(filepath.Dir(path), saveTempMode(statErr == nil))
	if err != nil {
		return err
	}
	name := tmp.Name()
	// MuPDF opens the temp file by name, so close our handle first.
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := d.b.savePath(name, opts); err != nil {
		os.Remove(name)
		return err
	}
	if dst, err := os.Stat(path); err == nil {
		if err := os.Chmod(name, dst.Mode().Perm()); err != nil {
			os.Remove(name)
			return err
		}
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// saveTempMode is the creation mode of SaveWithOptions' temp file: 0600 when
// it will replace an existing file, 0644 (before the umask) for a new one.
func saveTempMode(destExists bool) os.FileMode {
	if destExists {
		return 0o600
	}
	return 0o644
}

// createSaveTemp creates ".gomupdf-<random>" in dir with mode & ^umask,
// retrying a few times if the random name is taken.
func createSaveTemp(dir string, mode os.FileMode) (*os.File, error) {
	var err error
	for range 10 {
		var f *os.File
		name := filepath.Join(dir, fmt.Sprintf(".gomupdf-%016x", rand.Uint64()))
		f, err = os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, mode)
		if !os.IsExist(err) {
			return f, err
		}
	}
	return nil, err
}

// checkNotSource returns an error if path is the file d was opened from by
// Open (compared with os.SameFile, so links and hard links count): MuPDF is
// still reading that file lazily, so writing it would corrupt the document.
// d.path is set once at open and never changes, so no lock is needed.
func (d *Document) checkNotSource(path string) error {
	if d.path == "" {
		return nil
	}
	dst, err := os.Stat(path)
	if err != nil {
		return nil
	}
	if src, err := os.Stat(d.path); err == nil && os.SameFile(src, dst) {
		return errors.New("gomupdf: save destination is the file the document was opened from")
	}
	return nil
}

// EzSave writes the document to a file with DefaultSaveOptions — the
// convenience path equivalent to PyMuPDF's ez_save.
func (d *Document) EzSave(path string) error {
	return d.SaveWithOptions(path, DefaultSaveOptions())
}

// SaveIncremental appends pending changes to path using an incremental update.
// The document must have been opened with OpenStream. It is shorthand for
// SaveWithOptions with Incremental set.
//
// Incremental save of an in-memory document requires a reasonably recent MuPDF;
// some older builds cannot relate the output to the original input and return
// "Cannot derive input stream from output stream".
func (d *Document) SaveIncremental(path string) error {
	return d.SaveWithOptions(path, SaveOptions{Incremental: true})
}
