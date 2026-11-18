//go:build !nomupdf

package gomupdf

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// bigOutlinePDF is a one-page PDF with n sibling outline items (a /Next
// chain), each with a destination.
func bigOutlinePDF(n int) []byte {
	objs := []string{
		`<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`,
		fmt.Sprintf(`<< /Type /Outlines /First 5 0 R /Last %d 0 R /Count %d >>`, 4+n, n),
	}
	for i := range n {
		s := fmt.Sprintf(`<< /Title (i%d) /Parent 4 0 R /Dest [3 0 R /Fit]`, i)
		if i > 0 {
			s += fmt.Sprintf(` /Prev %d 0 R`, 4+i)
		}
		if i < n-1 {
			s += fmt.Sprintf(` /Next %d 0 R`, 6+i)
		}
		objs = append(objs, s+" >>")
	}
	return scrubBuildPDF(objs, "")
}

// bigChainPDF is a one-page PDF whose page text is drawn with font 4 and
// whose page holds, under key, a chain of n indirect dictionaries, each
// pointing at the next through /N (link and its /A action chain through
// /Next when key is "Annots").
func bigChainPDF(key string, n int) []byte {
	objs := []string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		"",
		`<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`,
		"\nstream\nBT /F1 12 Tf 20 150 Td (" + scrubPageText + ") Tj ET",
	}
	page := `<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 5 0 R `
	if key == "Annots" {
		page += `/Resources << /Font << /F1 4 0 R >> >> /Annots [6 0 R] >>`
		objs = append(objs, `<< /Type /Annot /Subtype /Link /Rect [0 0 50 50] /A 7 0 R >>`)
		for i := range n {
			next := ""
			if i < n-1 {
				next = fmt.Sprintf(` /Next %d 0 R`, 8+i)
			}
			objs = append(objs, fmt.Sprintf(`<< /S /URI /URI (https://example.com/%d)%s >>`, i, next))
		}
	} else {
		page += `/Resources << /Font << /F1 4 0 R >> /GomupdfChain 6 0 R >> >>`
		for i := range n {
			next := ""
			if i < n-1 {
				next = fmt.Sprintf(` /N %d 0 R`, 7+i)
			}
			objs = append(objs, `<<`+next+` >>`)
		}
	}
	objs[2] = page
	return scrubBuildPDF(objs, "")
}

// bigFormsPDF is a one-page PDF whose content draws form 5, which draws form
// 6, and so on n levels deep; the innermost form shows tok.
func bigFormsPDF(n int, tok string) []byte {
	objs := []string{
		`<< /Type /Catalog /Pages 2 0 R >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /X 5 0 R >> >> /Contents 4 0 R >>`,
		"\nstream\n/X Do",
	}
	for i := range n {
		body := "/X Do"
		res := fmt.Sprintf(`/XObject << /X %d 0 R >>`, 6+i)
		if i == n-1 {
			body, res = "BT /F1 12 Tf 20 150 Td ("+tok+") Tj ET", fmt.Sprintf(`/Font << /F1 %d 0 R >>`, 5+n)
		}
		objs = append(objs, `/Type /XObject /Subtype /Form /BBox [0 0 200 200] /Resources << `+res+" >>\nstream\n"+body)
	}
	objs = append(objs, `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>`)
	return scrubBuildPDF(objs, "")
}

// Deeply recursive MuPDF calls run on a large stack: saves of 20,000 sibling
// outline items (unscrubbed and after Scrub with KeepOutlines), of a
// 50,000-deep dictionary chain under the page /Resources and of a
// 20,000-long Link /Next action chain, and extraction and rendering through
// 60 levels of nested forms, all complete from goroutines whose own threads
// have 128 KiB stacks on musl (before, each crashed with SIGSEGV; the save
// at about 1,600 outline items). 200 levels of forms exceed MuPDF's own
// limit (its exception stack has 256 slots, enough for about 65 levels) and
// fail with an error, without crashing.
func TestDeepRecursionOnLargeStack(t *testing.T) {
	save := func(pdf []byte, opts *ScrubOptions) func() {
		return func() {
			d, err := OpenStream(pdf)
			if err != nil {
				t.Error(err)
				return
			}
			defer d.Close()
			if opts != nil {
				if err := d.Scrub(*opts); err != nil {
					t.Error(err)
					return
				}
			}
			out, err := d.SaveBytesWithOptions(scrubSaveOpts)
			if err != nil {
				t.Error(err)
				return
			}
			re, err := OpenStream(out)
			if err != nil {
				t.Error(err)
				return
			}
			re.Close()
		}
	}
	outline := bigOutlinePDF(20000)
	t.Run("outline", func(t *testing.T) { scrubConcurrently(save(outline, nil)) })
	t.Run("outline KeepOutlines", func(t *testing.T) { scrubConcurrently(save(outline, &ScrubOptions{KeepOutlines: true})) })
	t.Run("resources chain", func(t *testing.T) { scrubConcurrently(save(bigChainPDF("Resources", 50000), nil)) })
	t.Run("link action chain", func(t *testing.T) { scrubConcurrently(save(bigChainPDF("Annots", 20000), nil)) })
	for _, levels := range []int{60, 200} {
		t.Run(fmt.Sprintf("%d nested forms", levels), func(t *testing.T) { bigForms(t, levels) })
	}
}

// bigForms extracts and renders bigFormsPDF(levels) from several goroutines;
// up to 60 levels both work, deeper both fail.
func bigForms(t *testing.T, levels int) {
	const tok = "GomupdfDeepForm4b1c"
	pdf := bigFormsPDF(levels, tok)
	ok := levels <= 60
	scrubConcurrently(func() {
		d, err := OpenStream(pdf)
		if err != nil {
			t.Error(err)
			return
		}
		defer d.Close()
		p, err := d.LoadPage(0)
		if err != nil {
			t.Error(err)
			return
		}
		s, err := p.GetText()
		if (err == nil && strings.Contains(s, tok)) != ok {
			t.Errorf("text %q, %v", s, err)
		}
		if _, err := p.Pixmap(); (err == nil) != ok {
			t.Errorf("render: %v", err)
		}
	})
}

// bigNestedOutlinePDF is a one-page PDF whose outline nests n items, each
// the only child of the one before.
func bigNestedOutlinePDF(n int) []byte {
	objs := []string{`<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R >>`, `<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>`, `<< /Type /Outlines /First 5 0 R /Last 5 0 R >>`}
	for i := range n {
		s := fmt.Sprintf(`<< /Title (i%d) /Parent %d 0 R /Dest [3 0 R /Fit]`, i, 4+i)
		if i < n-1 {
			s += fmt.Sprintf(` /First %d 0 R /Last %d 0 R`, 6+i, 6+i)
		}
		objs = append(objs, s+" >>")
	}
	return scrubBuildPDF(objs, "")
}

// bigAnnotsPDF is a one-page PDF whose /Annots lists n Link annotations
// (one shared object) and a widget.
func bigAnnotsPDF(n int) []byte {
	return scrubBuildPDF([]string{
		`<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [5 0 R] >> >>`,
		`<< /Type /Pages /Kids [3 0 R] /Count 1 >>`,
		`<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Annots [5 0 R` + strings.Repeat(" 4 0 R", n) + `] >>`,
		`<< /Type /Annot /Subtype /Link /Rect [0 0 10 10] /A << /S /URI /URI (https://example.com/) >> >>`,
		`<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /Rect [20 20 80 40] /P 3 0 R >>`,
	}, "")
}

// The remaining document-reading calls run on the large stack too, from
// goroutines: TOC of an outline nested 5,000 levels deep (which crashed at
// about 950 on the caller's stack) and of 100,000 siblings, SetTOC of a deep
// and a long outline with its save, InsertPDF and InsertPDFRange of a source
// whose page holds a 20,000-long reference chain, and Links, Annotations and
// Widgets of a page whose /Annots has 200,000 entries. Each returns a result
// or an error, never a crash.
func TestDeepDocumentCalls(t *testing.T) {
	t.Run("TOC", func(t *testing.T) {
		for _, c := range []struct {
			pdf  []byte
			want int
		}{{bigNestedOutlinePDF(5000), 5000}, {bigOutlinePDF(100000), 100000}} {
			scrubConcurrently(func() {
				d, err := OpenStream(c.pdf)
				if err != nil {
					t.Error(err)
					return
				}
				defer d.Close()
				if toc, err := d.TOC(); err == nil && len(toc) != c.want {
					t.Errorf("TOC has %d entries, want %d", len(toc), c.want)
				}
			})
		}
	})
	t.Run("SetTOC", func(t *testing.T) {
		deep, long := make([]TOCEntry, 5000), make([]TOCEntry, 100000)
		for i := range deep {
			deep[i] = TOCEntry{Level: i + 1, Title: "d", Page: 0}
		}
		for i := range long {
			long[i] = TOCEntry{Level: 1, Title: "l", Page: 0}
		}
		for _, entries := range [][]TOCEntry{deep, long} {
			scrubConcurrently(func() {
				d, err := OpenStream(bigOutlinePDF(1))
				if err != nil {
					t.Error(err)
					return
				}
				defer d.Close()
				if err := d.SetTOC(entries); err != nil {
					return
				}
				out, err := d.SaveBytesWithOptions(scrubSaveOpts)
				if err != nil {
					t.Error(err)
					return
				}
				re, err := OpenStream(out)
				if err != nil {
					t.Error(err)
					return
				}
				defer re.Close()
				if toc, err := re.TOC(); err == nil && len(toc) != len(entries) {
					t.Errorf("TOC after SetTOC has %d entries, want %d", len(toc), len(entries))
				}
			})
		}
	})
	t.Run("InsertPDF", func(t *testing.T) {
		src := bigChainPDF("Resources", 20000)
		for _, rng := range []bool{false, true} {
			scrubConcurrently(func() {
				d, err := OpenStream(bigOutlinePDF(1))
				if err != nil {
					t.Error(err)
					return
				}
				defer d.Close()
				if rng {
					err = d.InsertPDFRange(src, 0, 0, "")
				} else {
					err = d.InsertPDF(src, "")
				}
				if err != nil {
					return // a clean refusal
				}
				// Success must mean the whole chain was copied.
				out, err := d.SaveBytesWithOptions(scrubSaveOpts)
				if err != nil {
					t.Error(err)
					return
				}
				if n := strings.Count(string(out), "/N "); n < 20000-1 {
					t.Errorf("InsertPDF succeeded but copied %d of the chain's 19,999 links", n)
				}
			})
		}
	})
	t.Run("annotations", func(t *testing.T) {
		pdf := bigAnnotsPDF(200000)
		scrubConcurrently(func() {
			d, err := OpenStream(pdf)
			if err != nil {
				t.Error(err)
				return
			}
			defer d.Close()
			p, err := d.LoadPage(0)
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = p.Links()
			_, _ = p.Annotations()
			_, _ = p.Widgets()
		})
	})
}

// When the large-stack thread cannot be created (here: a stack size no
// system accepts), a wrapped call fails with an error and runs nothing, not
// even on the caller's stack, which is the one known to overflow: Open,
// reads, edits, Scrub and saves all return the error, Authenticate returns
// false, and the document is unchanged and usable once threads can be
// created again.
func TestLargeStackUnavailable(t *testing.T) {
	const msg = "cannot create a thread with a large stack"
	d, err := OpenStream(scrubFixture())
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := d.LoadPage(0)
	if err != nil {
		t.Fatal(err)
	}
	old := setBigStackSize(1 << 62)
	errs := map[string]error{}
	_, errs["OpenStream"] = OpenStream(scrubFixture())
	_, errs["GetText"] = p.GetText()
	_, errs["Pixmap"] = p.Pixmap()
	_, errs["Links"] = p.Links()
	_, errs["TOC"] = d.TOC()
	_, errs["Metadata"] = d.Metadata()
	_, errs["HiddenContent"] = d.HiddenContent()
	errs["Scrub"] = d.Scrub(ScrubOptions{})
	errs["AddRedaction"] = p.AddRedaction(geometry.Rect{X0: 10, Y0: 10, X1: 50, Y1: 50}, RedactOptions{})
	errs["InsertPDF"] = d.InsertPDF(scrubFixture(), "")
	_, errs["SaveBytesWithOptions"] = d.SaveBytesWithOptions(scrubSaveOpts)
	authenticated := d.Authenticate("x")
	setBigStackSize(old)
	for name, err := range errs {
		if err == nil || !strings.Contains(err.Error(), msg) {
			t.Errorf("%s: %v, want an error saying %q", name, err, msg)
		}
	}
	if authenticated {
		t.Error("Authenticate succeeded without its thread")
	}
	saved, err := d.SaveBytesWithOptions(scrubSaveOpts)
	if err != nil {
		t.Fatal(err)
	}
	if _, re := scrubRoundTrip(t, saved, ScrubOptions{}); !strings.Contains(scrubText(t, re), scrubPageText) {
		t.Errorf("after: page text %q", scrubText(t, re))
	}
}

// Under an address-space limit, many concurrent saves of documents a small
// stack cannot save either succeed or fail with the thread error; none runs
// on the goroutine's 128 KiB stack and crashes. Each case runs in a child
// process, as RLIMIT_AS cannot be undone. With no limit, 500 concurrent
// saves all succeed.
func TestLargeStackAddressLimit(t *testing.T) {
	if arg := os.Getenv("GOMUPDF_BIGSTACK_CHILD"); arg != "" {
		bigStackChild(t, arg)
		return
	}
	for _, c := range []struct {
		limit string // bytes of address space, "" for none
		calls int
		allOK bool
	}{{"3221225472", 8, true}, {"3221225472", 100, false}, {"", 500, true}} {
		t.Run(fmt.Sprintf("limit %s calls %d", c.limit, c.calls), func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestLargeStackAddressLimit$", "-test.count=1")
			cmd.Env = append(os.Environ(), "GOMUPDF_BIGSTACK_CHILD="+c.limit+":"+strconv.Itoa(c.calls))
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("child: %v\n%s", err, out[max(0, len(out)-2000):])
			}
			m := regexp.MustCompile(`saved (\d+) refused (\d+)`).FindSubmatch(out)
			if m == nil {
				t.Fatalf("child printed no result:\n%s", out[max(0, len(out)-2000):])
			}
			t.Logf("saved %s, refused %s", m[1], m[2])
			if c.allOK && string(m[2]) != "0" {
				t.Errorf("%s saves refused, want none", m[2])
			}
		})
	}
}

// bigStackChild runs in the child: it opens calls documents with a
// 2,000-item outline, sets RLIMIT_AS when given a limit, then saves them all
// from as many goroutines at once.
func bigStackChild(t *testing.T, arg string) {
	limit, calls, _ := strings.Cut(arg, ":")
	n, _ := strconv.Atoi(calls)
	pdf := bigOutlinePDF(2000)
	docs := make([]*Document, n)
	for i := range docs {
		d, err := OpenStream(pdf)
		if err != nil {
			t.Fatal(err)
		}
		defer d.Close()
		docs[i] = d
	}
	if limit != "" {
		l, _ := strconv.ParseUint(limit, 10, 64)
		if err := syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{Cur: l, Max: l}); err != nil {
			t.Fatal(err)
		}
	}
	var saved, refused atomic.Int64
	var wg sync.WaitGroup
	start := make(chan struct{})
	for _, d := range docs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			switch _, err := d.SaveBytesWithOptions(scrubSaveOpts); {
			case err == nil:
				saved.Add(1)
			case strings.Contains(err.Error(), "cannot create a thread with a large stack"):
				refused.Add(1)
			default:
				t.Error(err)
			}
		}()
	}
	close(start)
	wg.Wait()
	fmt.Printf("saved %d refused %d\n", saved.Load(), refused.Load())
}
