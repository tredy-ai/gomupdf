//go:build !nomupdf

package gomupdf

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// fuzzMaxInput caps fuzz inputs; larger ones are skipped, not truncated.
const fuzzMaxInput = 256 << 10

// FuzzPipeline runs the redaction service's flow on arbitrary bytes: open,
// RawDict on page 0, redact the first word by its character quads, apply,
// scrub, save, reopen. Errors are fine; crashes and hangs are not. /ActualText
// extraction is left out: it crashes MuPDF 1.26.11 on known input (see
// TestActualTextCrashInput), and TestCorpus covers it in a child process.
//
// Seeds: the small committed PDFs (corpus files listed in KNOWN_FAILURES are
// skipped, since plain go test replays seeds in-process) and two built pages.
// Inputs that crash it are kept as raw PDFs in testdata/fuzz/crashers, which
// TestCorpus runs in child processes.
func FuzzPipeline(f *testing.F) {
	known := map[string]bool{}
	if b, err := os.ReadFile(corpusKnownPath); err == nil {
		for _, ln := range strings.Split(string(b), "\n") {
			known[strings.Split(ln, "\t")[0]] = true
		}
	}
	for _, root := range []string{"testdata/resources", "testdata/corpus"} {
		filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || known[p] || !strings.EqualFold(filepath.Ext(p), ".pdf") {
				return nil
			}
			if b, err := os.ReadFile(p); err == nil && len(b) <= fuzzMaxInput {
				f.Add(b)
			}
			return nil
		})
	}
	f.Add(pdfPage("", "", "BT /F1 12 Tf 20 100 Td (Hello fuzzing world) Tj ET"))
	f.Add(pdfPage("/MediaBox [0 0 300 200] /Rotate 90", "", slantedAt("aaa Hello bbb", 50, 50)))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > fuzzMaxInput {
			t.Skip()
		}
		fuzzPipeline(data)
	})
}

func fuzzPipeline(data []byte) {
	d, err := OpenStream(data)
	if err != nil {
		return
	}
	defer d.Close()
	if d.NeedsPass() || d.PageCount() == 0 {
		return
	}
	p, err := d.LoadPage(0)
	if err != nil {
		return
	}
	if blocks, err := p.RawDict(); err == nil {
		var qs []geometry.Quad
	words:
		for _, b := range blocks {
			for _, l := range b.Lines {
				for _, ch := range l.Chars {
					if ch.Rune == ' ' && len(qs) > 0 {
						break words
					}
					if ch.Rune != ' ' {
						qs = append(qs, ch.Quad)
					}
				}
				if len(qs) > 0 {
					break words
				}
			}
		}
		if len(qs) > 0 && p.AddRedactionQuads(qs, RedactOptions{}) == nil {
			p.ApplyRedactions(ApplyOptions{})
		}
	}
	if d.Scrub(ScrubOptions{}) != nil {
		return
	}
	out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
	if err != nil {
		return
	}
	if d2, err := OpenStream(out); err == nil {
		if p2, err := d2.LoadPage(0); err == nil {
			p2.RawDict()
		}
		d2.Close()
	}
}
