package gomupdf

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func pageText(t *testing.T, d *Document) string {
	t.Helper()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	s, err := p.GetText()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	m, _ := filepath.Glob(filepath.Join(dir, ".gomupdf-*"))
	if len(m) != 0 {
		t.Errorf("leftover temp files: %v", m)
	}
}

func TestSaveWithOptionsStreams(t *testing.T) {
	src := openFixture(t, "small-table.pdf")
	defer src.Close()
	want := pageText(t, src)
	dir := t.TempDir()

	for g := 0; g <= 4; g++ {
		path := filepath.Join(dir, "out.pdf")
		if err := src.SaveWithOptions(path, SaveOptions{Garbage: g, Deflate: true}); err != nil {
			t.Fatalf("garbage %d: %v", g, err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if n := bytes.Count(data, []byte("startxref")); n != 1 {
			t.Errorf("garbage %d: %d startxref, want 1", g, n)
		}
		d, err := Open(path)
		if err != nil {
			t.Fatalf("garbage %d reopen: %v", g, err)
		}
		if got := pageText(t, d); got != want {
			t.Errorf("garbage %d: text %q, want %q", g, got, want)
		}
		d.Close()
	}
	noTempFiles(t, dir)

	info, err := os.Stat(filepath.Join(dir, "out.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("new file mode = %v, want 0644", info.Mode().Perm())
	}
	// An existing destination keeps its mode.
	if err := os.Chmod(filepath.Join(dir, "out.pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := src.EzSave(filepath.Join(dir, "out.pdf")); err != nil {
		t.Fatal(err)
	}
	if info, _ = os.Stat(filepath.Join(dir, "out.pdf")); info.Mode().Perm() != 0o600 {
		t.Errorf("replaced file mode = %v, want 0600", info.Mode().Perm())
	}
}

func TestSaveWithOptionsRejectsSourcePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "src.pdf")
	b, err := os.ReadFile(res + "small-table.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err := d.SaveWithOptions(path, DefaultSaveOptions()); err == nil {
		t.Fatal("saving onto the source path should error")
	}
	if after, _ := os.ReadFile(path); !bytes.Equal(after, b) {
		t.Error("source file was modified")
	}
	noTempFiles(t, filepath.Dir(path))
	// A different destination still works.
	if err := d.SaveWithOptions(filepath.Join(filepath.Dir(path), "copy.pdf"), DefaultSaveOptions()); err != nil {
		t.Fatal(err)
	}
}

func TestSaveWithOptionsFailureLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	d := openFixture(t, "small-table.pdf")
	defer d.Close()

	// Unwritable (missing) directory: no temp file can be created.
	if err := d.SaveWithOptions(filepath.Join(dir, "nodir", "x.pdf"), DefaultSaveOptions()); err == nil {
		t.Error("expected an error for a missing directory")
	}

	// MuPDF fails after the temp file exists (not a PDF): the existing
	// destination is untouched and the temp file is removed.
	txt, err := OpenAnyStream([]byte("hello"), "txt")
	if err != nil {
		t.Fatal(err)
	}
	defer txt.Close()
	path := filepath.Join(dir, "keep.pdf")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := txt.SaveWithOptions(path, DefaultSaveOptions()); err == nil {
		t.Error("expected an error saving a non-PDF document")
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Errorf("destination changed to %q", got)
	}
	missing := filepath.Join(dir, "new.pdf")
	if err := txt.SaveWithOptions(missing, DefaultSaveOptions()); err == nil {
		t.Error("expected an error")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("partial file left at path")
	}
	noTempFiles(t, dir)
}

func TestSaveWithOptionsClosedAndBuffered(t *testing.T) {
	d, _ := NewPDF()
	d.Close()
	if err := d.SaveWithOptions(filepath.Join(t.TempDir(), "x.pdf"), DefaultSaveOptions()); err == nil {
		t.Error("closed document should error")
	}
	// Encrypt stays on the buffered path and still produces a file.
	e := buildDoc(t)
	defer e.Close()
	path := filepath.Join(t.TempDir(), "enc.pdf")
	if err := e.SaveWithOptions(path, SaveOptions{Encrypt: true, UserPassword: "u", OwnerPassword: "o", Permissions: -1}); err != nil {
		t.Fatal(err)
	}
	r, err := OpenWithPassword(path, "u")
	if err != nil {
		t.Fatal(err)
	}
	r.Close()
}

func TestSaveBufferedPathsRejectSourcePath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "src.pdf")
	b, err := os.ReadFile(res + "small-table.pdf")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for name, save := range map[string]func() error{
		"Encrypt": func() error {
			return d.SaveWithOptions(path, SaveOptions{Encrypt: true, UserPassword: "u", OwnerPassword: "o", Permissions: -1})
		},
		"Incremental": func() error { return d.SaveWithOptions(path, SaveOptions{Incremental: true}) },
		"Save":        func() error { return d.Save(path, true) },
	} {
		if err := save(); err == nil {
			t.Errorf("%s: saving onto the source path should error", name)
		}
		if after, _ := os.ReadFile(path); !bytes.Equal(after, b) {
			t.Errorf("%s: source file was modified", name)
		}
	}
}

func TestSaveWithOptionsThroughSymlink(t *testing.T) {
	src := openFixture(t, "small-table.pdf")
	defer src.Close()
	dir := t.TempDir()
	target := filepath.Join(dir, "target.pdf")
	link := filepath.Join(dir, "link.pdf")
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target.pdf", link); err != nil {
		t.Fatal(err)
	}
	if err := src.SaveWithOptions(link, DefaultSaveOptions()); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("link is no longer a symlink: %v %v", fi, err)
	}
	if b, _ := os.ReadFile(target); !bytes.HasPrefix(b, []byte("%PDF")) {
		t.Errorf("target not updated: %q", b)
	}
	noTempFiles(t, dir)

	// Dangling symlink: refused, nothing created.
	dang := filepath.Join(dir, "dangling.pdf")
	if err := os.Symlink("missing.pdf", dang); err != nil {
		t.Fatal(err)
	}
	if err := src.SaveWithOptions(dang, DefaultSaveOptions()); err == nil {
		t.Error("dangling symlink: expected an error")
	}
	if _, err := os.Lstat(filepath.Join(dir, "missing.pdf")); err == nil {
		t.Error("dangling symlink: target was created")
	}
	noTempFiles(t, dir)
}
