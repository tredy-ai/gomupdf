//go:build !nomupdf

package gomupdf

import (
	"fmt"
	"strings"
	"testing"
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

// When the large-stack thread cannot be created (here: a stack size no
// system accepts), the call runs on the calling thread as before.
func TestLargeStackFallback(t *testing.T) {
	old := setBigStackSize(1 << 62)
	defer setBigStackSize(old)
	out, re := scrubRoundTrip(t, scrubFixture(), ScrubOptions{})
	if len(out) == 0 || !strings.Contains(scrubText(t, re), scrubPageText) {
		t.Errorf("fallback: page text %q", scrubText(t, re))
	}
	if h := scrubHidden(t, re); h != (HiddenContent{}) {
		t.Errorf("fallback: after scrub %+v", h)
	}
}
