package gomupdf

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeBigPDF writes a valid one-page PDF whose content stream is padded with
// about size bytes of comment text, so the file is large but cheap to generate.
func writeBigPDF(t *testing.T, path string, size int) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var off []int
	pos := 0
	w := func(s string) {
		n, err := f.WriteString(s)
		if err != nil {
			t.Fatal(err)
		}
		pos += n
	}
	w("%PDF-1.4\n")
	obj := func(s string) {
		off = append(off, pos)
		w(fmt.Sprintf("%d 0 obj\n%s\nendobj\n", len(off), s))
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj("<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R >>")
	head := "0 g\n"
	pad := "% " + strings.Repeat("x", 1022) + "\n"
	n := size / len(pad)
	off = append(off, pos)
	w(fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n", len(head)+n*len(pad)))
	w(head)
	for i := 0; i < n; i++ {
		w(pad)
	}
	w("\nendstream\nendobj\n")
	xref := pos
	w(fmt.Sprintf("xref\n0 %d\n0000000000 65535 f \n", len(off)+1))
	for _, o := range off {
		w(fmt.Sprintf("%010d 00000 n \n", o))
	}
	w(fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(off)+1, xref))
}

// Open must not hold the file on the Go heap.
func TestOpenPathDoesNotBuffer(t *testing.T) {
	if testing.Short() {
		t.Skip("generates a 50 MB file")
	}
	const size = 50 << 20
	path := filepath.Join(t.TempDir(), "big.pdf")
	writeBigPDF(t, path, size)

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if n := d.PageCount(); n != 1 {
		t.Fatalf("PageCount = %d, want 1", n)
	}
	runtime.ReadMemStats(&after)
	// TotalAlloc is cumulative, so even a transient 50 MB ReadFile would show
	// after a GC. MuPDF's own C allocations are invisible here by design.
	if grown := after.TotalAlloc - before.TotalAlloc; grown > size/10 {
		t.Errorf("Go heap allocated %d bytes opening a %d byte file", grown, size)
	}
}

func TestOpenPathMatchesOpenStream(t *testing.T) {
	const name = res + "small-table.pdf"
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	text := func(d *Document) string {
		defer d.Close()
		var sb strings.Builder
		for i := 0; i < d.PageCount(); i++ {
			p, err := d.LoadPage(i)
			if err != nil {
				t.Fatal(err)
			}
			s, err := p.GetText()
			if err != nil {
				t.Fatal(err)
			}
			sb.WriteString(s)
		}
		return sb.String()
	}
	a, err := Open(name)
	if err != nil {
		t.Fatal(err)
	}
	b, err := OpenStream(data)
	if err != nil {
		t.Fatal(err)
	}
	ta, tb := text(a), text(b)
	if ta == "" || ta != tb {
		t.Errorf("text differs or is empty: %q vs %q", ta, tb)
	}
}

func TestOpenPathErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]string{
		"missing":   filepath.Join(dir, "nope.pdf"),
		"directory": dir,
		"empty":     write("empty.pdf", ""),
		"header":    write("cut.pdf", "%PDF-1"),
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			if d, err := Open(p); err == nil {
				d.Close()
				t.Fatal("expected an error")
			}
			if d, err := OpenWithPassword(p, "x"); err == nil {
				d.Close()
				t.Fatal("OpenWithPassword: expected an error")
			}
		})
	}
}

func TestIncrementalSavePathOpened(t *testing.T) {
	d, err := Open(res + "small-table.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	_, err = d.SaveBytesWithOptions(SaveOptions{Incremental: true})
	if err == nil || !strings.Contains(err.Error(), "needs a document opened with OpenStream") {
		t.Errorf("err = %v", err)
	}
}

func TestOpenRejectsNonPDF(t *testing.T) {
	dir := t.TempDir()
	for name, body := range map[string]string{
		"a.txt":  "hello world\n",
		"b.html": "<html><body><p>hi</p></body></html>",
	} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		d, err := Open(p)
		if err == nil {
			d.Close()
			t.Fatalf("Open(%s): expected an error", name)
		}
		if !strings.Contains(err.Error(), "not a PDF") {
			t.Errorf("Open(%s): err = %v", name, err)
		}
		// OpenAny must keep working for the same file.
		a, err := OpenAny(p)
		if err != nil {
			t.Fatalf("OpenAny(%s): %v", name, err)
		}
		a.Close()
	}
}

func TestOpenRejectsNULPath(t *testing.T) {
	good := res + "small-table.pdf"
	for _, p := range []string{good + "\x00junk", "\x00" + good} {
		if d, err := Open(p); err == nil {
			d.Close()
			t.Errorf("Open(%q): expected an error", p)
		}
	}
}
