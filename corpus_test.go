//go:build !nomupdf

package gomupdf

// Corpus invariant test: the redaction service's whole flow (extract, redact
// by quads and by rect, apply, save, reopen, verify; scrub, save, reopen,
// verify) on real files, each in a child process so that a crash inside MuPDF
// names the file instead of killing the run.
//
// Inputs: testdata/corpus and testdata/fuzz/crashers (committed, always run),
// plus every file under $GOMUPDF_CORPUS when set (the bulk set, never
// committed; its failures are logged and reported, not fatal). Committed files
// that fail for a tracked reason are listed in testdata/corpus/KNOWN_FAILURES.tsv
// (repo-relative path, category, reason); a listed failure that no longer
// happens fails the test so the list cannot rot.
//
// Environment:
//
//	GOMUPDF_CORPUS          bulk directory to walk as well
//	GOMUPDF_CORPUS_REPORT   write one JSON object per file (JSON Lines) here
//	GOMUPDF_CORPUS_TIMEOUT  per-file timeout in seconds (default 60)
//	GOMUPDF_CORPUS_JOBS     concurrent child processes (default 4)
//	GOMUPDF_CORPUS_MATCH    only files whose path contains this string

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"io/fs"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode"
	"unicode/utf16"

	"github.com/tredy-ai/gomupdf/geometry"
)

const (
	corpusKnownPath = "testdata/corpus/KNOWN_FAILURES.tsv"
	corpusChildEnv  = "GOMUPDF_CORPUS_CHILD"
	corpusNoATEnv   = "GOMUPDF_CORPUS_NO_ACTUALTEXT"
	corpusTol       = 0.25 // pt: a character "at its old position" is within this on both axes
)

var corpusCommittedRoots = []string{"testdata/corpus", "testdata/fuzz/crashers"}

// corpusFinding is one observation about a file. Fail marks a broken
// invariant; the rest are notes for the report.
type corpusFinding struct {
	Cat    string `json:"cat"`
	Detail string `json:"detail"`
	Fail   bool   `json:"fail,omitempty"`
}

// corpusChildResult is what the child process reports on its last line.
type corpusChildResult struct {
	Rejected         string          `json:"rejected,omitempty"`
	Encrypted        bool            `json:"encrypted,omitempty"`
	Pages            int             `json:"pages"`
	Tags             []string        `json:"tags,omitempty"`
	Hidden           *HiddenContent  `json:"hidden,omitempty"`
	RedactedWords    int             `json:"redacted_words"`
	LayerWords       *[2]int         `json:"layer_words,omitempty"` // words on the checked pages before and after ShowAllLayers
	ByteConclusive   int             `json:"byte_conclusive"`
	ByteInconclusive int             `json:"byte_inconclusive"`
	Findings         []corpusFinding `json:"findings,omitempty"`
}

// corpusRow is one line of the GOMUPDF_CORPUS_REPORT file.
type corpusRow struct {
	File     string  `json:"file"`
	Outcome  string  `json:"outcome"` // ok, rejected, encrypted, fail, crash, timeout, killed, oom
	Category string  `json:"category"`
	Detail   string  `json:"detail"`
	Seconds  float64 `json:"seconds"`
	Size     int64   `json:"size"`
	corpusChildResult
}

func (r *corpusRow) failCats() []string {
	var out []string
	for _, f := range r.Findings {
		if f.Fail && !slices.Contains(out, f.Cat) {
			out = append(out, f.Cat)
		}
	}
	return out
}

func TestCorpus(t *testing.T) {
	type job struct {
		path      string
		committed bool
	}
	var jobs []job
	skipExt := map[string]bool{".link": true, ".tsv": true, ".md": true, ".err": true, ".txt": true, ".json": true}
	walk := func(root string, committed bool) {
		filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil || !e.Type().IsRegular() || skipExt[strings.ToLower(filepath.Ext(p))] {
				return nil
			}
			if m := os.Getenv("GOMUPDF_CORPUS_MATCH"); m == "" || strings.Contains(p, m) {
				jobs = append(jobs, job{p, committed})
			}
			return nil
		})
	}
	for _, r := range corpusCommittedRoots {
		walk(r, true)
	}
	if bulk := os.Getenv("GOMUPDF_CORPUS"); bulk != "" {
		walk(bulk, false)
	}
	if testing.Short() && len(jobs) > 5 {
		jobs = jobs[:5]
	}
	if len(jobs) == 0 {
		t.Skip("no corpus files")
	}
	known := loadKnownFailures(t)

	// Committed files get at most 30s, and 10s when listed as timing out,
	// so a tracked hang costs the gate little.
	timeout := 60 * time.Second
	if s, err := strconv.Atoi(os.Getenv("GOMUPDF_CORPUS_TIMEOUT")); err == nil && s > 0 {
		timeout = time.Duration(s) * time.Second
	}
	timeoutFor := func(j job) time.Duration {
		switch {
		case !j.committed:
			return timeout
		case known[j.path]["timeout"] != "":
			return time.Duration(min64(int64(timeout), int64(10*time.Second)))
		}
		return time.Duration(min64(int64(timeout), int64(30*time.Second)))
	}
	workers := 4
	if n, err := strconv.Atoi(os.Getenv("GOMUPDF_CORPUS_JOBS")); err == nil && n > 0 {
		workers = n
	}
	var report io.Writer
	if p := os.Getenv("GOMUPDF_CORPUS_REPORT"); p != "" {
		f, err := os.Create(p)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		report = f
	}

	rows := make([]corpusRow, len(jobs))
	var mu sync.Mutex
	next := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range next {
				row := runCorpusChild(jobs[i].path, timeoutFor(jobs[i]))
				mu.Lock()
				rows[i] = row
				if report != nil {
					b, _ := json.Marshal(row)
					report.Write(append(b, '\n'))
				}
				mu.Unlock()
			}
		})
	}
	for i := range jobs {
		next <- i
	}
	close(next)
	wg.Wait()

	outcomes := map[string]int{}
	cats := map[string]int{}
	seen := map[string]bool{}
	for i, row := range rows {
		outcomes[row.Outcome]++
		fails := row.failCats()
		for _, c := range fails {
			cats[c]++
		}
		if !jobs[i].committed {
			if len(fails) > 0 {
				t.Logf("bulk %s: %s %s: %s", row.File, row.Outcome, row.Category, row.Detail)
			}
			continue
		}
		seen[row.File] = true
		listed := known[row.File]
		for _, c := range fails {
			if _, ok := listed[c]; !ok {
				t.Errorf("%s: %s: %s", row.File, c, firstDetail(row, c))
			}
		}
		for c := range listed {
			if !slices.Contains(fails, c) {
				t.Errorf("%s: KNOWN_FAILURES lists %q but the file no longer fails that way: remove this entry", row.File, c)
			}
		}
	}
	if os.Getenv("GOMUPDF_CORPUS_MATCH") == "" && !testing.Short() {
		for p := range known {
			if !seen[p] {
				t.Errorf("KNOWN_FAILURES lists %s, which is not in the corpus: remove this entry", p)
			}
		}
	}
	t.Logf("corpus: %d files, outcomes %v, failure categories %v", len(rows), outcomes, cats)
}

func firstDetail(r corpusRow, cat string) string {
	for _, f := range r.Findings {
		if f.Cat == cat {
			return f.Detail
		}
	}
	return ""
}

// loadKnownFailures reads KNOWN_FAILURES.tsv: path, category, reason per line.
func loadKnownFailures(t *testing.T) map[string]map[string]string {
	out := map[string]map[string]string{}
	b, err := os.ReadFile(corpusKnownPath)
	if errors.Is(err, fs.ErrNotExist) {
		return out
	}
	if err != nil {
		t.Fatal(err)
	}
	for n, ln := range strings.Split(string(b), "\n") {
		if ln = strings.TrimSpace(ln); ln == "" || ln[0] == '#' {
			continue
		}
		f := strings.Split(ln, "\t")
		if len(f) < 3 || f[2] == "" {
			t.Fatalf("%s:%d: want path<TAB>category<TAB>reason", corpusKnownPath, n+1)
		}
		if out[f[0]] == nil {
			out[f[0]] = map[string]string{}
		}
		out[f[0]][f[1]] = f[2]
	}
	return out
}

// runCorpusChild checks one file in a child process. A child that dies with
// /ActualText extraction on the stack is rerun without it, so the file's other
// invariants still get a verdict.
func runCorpusChild(path string, timeout time.Duration) corpusRow {
	row := corpusRow{File: path}
	if fi, err := os.Stat(path); err == nil {
		row.Size = fi.Size()
	}
	start := time.Now()
	res, outcome, detail := execCorpusChild(path, timeout, false)
	if outcome == "actualtext-crash" {
		res2, outcome2, detail2 := execCorpusChild(path, timeout, true)
		res = res2
		res.Findings = append([]corpusFinding{{Cat: "actualtext-crash", Detail: detail, Fail: true}}, res.Findings...)
		if outcome2 != "" {
			res.Findings = append(res.Findings, corpusFinding{Cat: outcome2, Detail: detail2, Fail: true})
		}
		outcome, detail = "crash", ""
	} else if outcome != "" {
		res.Findings = append(res.Findings, corpusFinding{Cat: outcome, Detail: detail, Fail: true})
	}
	row.corpusChildResult = res
	row.Seconds = math.Round(time.Since(start).Seconds()*100) / 100
	switch {
	case outcome == "crash" || outcome == "go-panic":
		row.Outcome = "crash"
	case outcome == "timeout" || outcome == "killed" || outcome == "oom":
		row.Outcome = outcome
	case res.Rejected != "":
		row.Outcome = "rejected"
	case res.Encrypted:
		row.Outcome = "encrypted"
	default:
		row.Outcome = "ok"
	}
	for _, f := range res.Findings {
		if f.Fail && row.Outcome == "ok" {
			row.Outcome = "fail"
		}
	}
	for _, f := range res.Findings {
		if f.Fail || row.Category == "" {
			row.Category, row.Detail = f.Cat, f.Detail
			if f.Fail {
				break
			}
		}
	}
	if row.Outcome == "rejected" {
		row.Category, row.Detail = "rejected", res.Rejected
	}
	return row
}

// execCorpusChild runs TestCorpusChild on path. outcome is "" when the child
// reported a result, else crash, actualtext-crash, go-panic, oom, timeout or
// killed.
func execCorpusChild(path string, timeout time.Duration, noAT bool) (res corpusChildResult, outcome, detail string) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCorpusChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), corpusChildEnv+"="+path)
	if noAT {
		cmd.Env = append(cmd.Env, corpusNoATEnv+"=1")
	}
	var stdout bytes.Buffer
	stderr := &tailBuffer{max: 64 << 10}
	cmd.Stdout, cmd.Stderr = &stdout, stderr
	runErr := cmd.Run()
	step := ""
	sc := bufio.NewScanner(&stdout)
	sc.Buffer(nil, 64<<20)
	for sc.Scan() {
		ln := sc.Text()
		if s, ok := strings.CutPrefix(ln, "STEP "); ok {
			step = s
		} else if s, ok := strings.CutPrefix(ln, "RESULT "); ok {
			if err := json.Unmarshal([]byte(s), &res); err == nil {
				return res, "", ""
			}
		}
	}
	errText := stderr.String()
	switch {
	case ctx.Err() != nil:
		return res, "timeout", fmt.Sprintf("after %v in step %q", timeout, step)
	case strings.Contains(errText, "out of memory"):
		return res, "oom", fmt.Sprintf("step %q: %s", step, crashSummary(errText))
	case strings.Contains(errText, "panic: ") && !strings.Contains(errText, "unexpected signal"):
		return res, "go-panic", fmt.Sprintf("step %q: %s", step, crashSummary(errText))
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() && ws.Signal() == syscall.SIGKILL {
			return res, "killed", fmt.Sprintf("SIGKILL (out of memory?) in step %q", step)
		}
	}
	outcome = "crash"
	if strings.Contains(step, "actualtext") {
		outcome = "actualtext-crash"
	}
	return res, outcome, fmt.Sprintf("step %q: %v: %s", step, runErr, crashSummary(errText))
}

var (
	crashSignalRe = regexp.MustCompile(`(?m)^(\[signal .*\]|SIG[A-Z]+: .*|fatal error: .*|panic: .*)$`)
	crashFrameRe  = regexp.MustCompile(`(?m)^(github\.com/tredy-ai/gomupdf\.[^\n(]+|_cgo_[^\n(]+)\(`)
)

// crashSummary picks the signal line and the first two package frames out of a
// Go crash dump.
func crashSummary(s string) string {
	parts := crashSignalRe.FindAllString(s, 2)
	for _, m := range crashFrameRe.FindAllStringSubmatch(s, 2) {
		parts = append(parts, m[1])
	}
	if len(parts) == 0 {
		s = strings.TrimSpace(s)
		return s[max(0, len(s)-300):]
	}
	return strings.Join(parts, " | ")
}

// tailBuffer keeps the last max bytes written (MuPDF can print megabytes of
// warnings on a broken file).
type tailBuffer struct {
	buf []byte
	max int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.buf = append(b.buf, p...)
	if len(b.buf) > 2*b.max {
		b.buf = append(b.buf[:0], b.buf[len(b.buf)-b.max:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string { return string(b.buf[max(0, len(b.buf)-b.max):]) }

// TestCorpusChild is the per-file worker that TestCorpus runs in a child
// process; it does nothing unless GOMUPDF_CORPUS_CHILD names a file.
func TestCorpusChild(t *testing.T) {
	path := os.Getenv(corpusChildEnv)
	if path == "" {
		t.Skip("helper process for TestCorpus")
	}
	// ponytail: a 3 GiB address-space cap stands in for per-child memory
	// accounting; a hostile file then fails an allocation instead of starving
	// its siblings.
	lim := uint64(3 << 30)
	syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{Cur: lim, Max: lim})
	c := &corpusCheck{useAT: os.Getenv(corpusNoATEnv) == "", path: path}
	c.run(path)
	b, err := json.Marshal(c.r)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("RESULT %s\n", b)
}

type corpusChar struct {
	Char
	WMode int
	Dir   geometry.Point
}

type corpusPage struct {
	n      int
	lines  [][]corpusChar
	text   string // GetText
	atText string // ExtractText with UseActualText
	raw    string // RawDict characters, for the scrub comparison
	links  []string
	bound  geometry.Rect
	render *Pixmap
	zoom   float64
	ok     bool // RawDict and GetText worked
}

type corpusWord struct {
	text   string
	chars  []corpusChar
	unique bool // occurs exactly once (as a substring) in the page's GetText
}

type corpusCheck struct {
	r      corpusChildResult
	useAT  bool
	path   string
	prefix string // prepended to categories: "layers:" in the layers flow
}

func (c *corpusCheck) step(format string, a ...any) { fmt.Printf("STEP "+format+"\n", a...) }

func (c *corpusCheck) add(fail bool, cat, format string, a ...any) {
	if len(c.r.Findings) >= 80 {
		return
	}
	d := fmt.Sprintf(format, a...)
	if len(d) > 700 {
		d = d[:700] + "..."
	}
	c.r.Findings = append(c.r.Findings, corpusFinding{Cat: c.prefix + cat, Detail: d, Fail: fail})
}

func (c *corpusCheck) fail(cat, format string, a ...any) { c.add(true, cat, format, a...) }
func (c *corpusCheck) note(cat, format string, a ...any) { c.add(false, cat, format, a...) }

func (c *corpusCheck) run(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		c.r.Rejected = err.Error()
		return
	}
	c.step("tags")
	c.r.Tags = pdfTags(pdfBlobs(data))

	// 1. open, by path and from bytes
	c.step("open path")
	dp, perr := Open(path)
	if perr == nil {
		c.r.Pages = dp.PageCount()
		dp.Close()
	}
	c.step("open stream")
	d, err := OpenStream(data)
	if err != nil {
		c.r.Rejected = err.Error()
		if perr == nil {
			c.note("open-mismatch", "Open succeeded, OpenStream failed: %v", err)
		}
		return
	}
	defer d.Close()
	if perr != nil {
		c.note("open-mismatch", "OpenStream succeeded, Open failed: %v", perr)
	} else if c.r.Pages != d.PageCount() {
		c.note("open-mismatch", "Open has %d pages, OpenStream %d", c.r.Pages, d.PageCount())
	}
	if d.NeedsPass() {
		c.r.Encrypted = true
		return
	}
	c.r.Pages = d.PageCount()

	// 2. extraction on the first five and last two pages
	var pages []*corpusPage
	for _, n := range corpusPageSet(c.r.Pages) {
		pages = append(pages, c.extract(d, n))
	}
	// 3. redaction round trip
	c.redaction(data, d, pages)
	// 4. scrub round trip
	c.scrub(data, pages)
	// 5. with optional content: the same again after ShowAllLayers
	if h := c.r.Hidden; h != nil && (h.Layers > 0 || h.HiddenLayers > 0) {
		c.layers(data, pages)
	}
}

// layers runs the extraction, redaction and scrub round trips again on the
// document after ShowAllLayers (saved and reopened), with categories
// prefixed "layers:". The layers must be gone and no word may be lost.
func (c *corpusCheck) layers(data []byte, pages []*corpusPage) {
	c.step("layers show")
	d, err := OpenStream(data)
	if err != nil {
		return
	}
	err = d.ShowAllLayers()
	var shown []byte
	if err == nil {
		shown, err = d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
	}
	d.Close()
	if err != nil {
		c.fail("layers-error", "ShowAllLayers and save: %v", err)
		return
	}
	ds, err := OpenStream(shown)
	if err != nil {
		c.fail("layers-error", "reopen after ShowAllLayers: %v", err)
		return
	}
	defer ds.Close()
	if h, err := ds.HiddenContent(); err != nil || h.Layers != 0 || h.HiddenLayers != 0 {
		c.fail("layers-remain", "after ShowAllLayers and save: Layers %d, HiddenLayers %d, %v", h.Layers, h.HiddenLayers, err)
	}
	var lp []*corpusPage
	c.prefix = "layers:"
	defer func() { c.prefix = "" }()
	for _, pg := range pages {
		lp = append(lp, c.extract(ds, pg.n))
	}
	c.r.LayerWords = &[2]int{countWords(pages), countWords(lp)}
	if c.r.LayerWords[1] < c.r.LayerWords[0] {
		c.fail("words-lost", "%d words on the checked pages before ShowAllLayers, %d after", c.r.LayerWords[0], c.r.LayerWords[1])
	}
	c.redaction(shown, ds, lp)
	c.scrub(shown, lp)
}

func countWords(pages []*corpusPage) int {
	n := 0
	for _, pg := range pages {
		for _, l := range pg.lines {
			in := false
			for _, ch := range l {
				sp := unicode.IsSpace(ch.Rune)
				if !sp && !in {
					n++
				}
				in = !sp
			}
		}
	}
	return n
}

func corpusPageSet(n int) []int {
	var out []int
	for i := 0; i < n; i++ {
		if i < 5 || i >= n-2 {
			out = append(out, i)
		}
	}
	return out
}

// pageChars reads p's characters with their line's writing mode.
func corpusChars(p *Page, opts TextOptions) ([][]corpusChar, error) {
	blocks, err := p.RawDictWithOptions(opts)
	if err != nil {
		return nil, err
	}
	var out [][]corpusChar
	for _, b := range blocks {
		for _, l := range b.Lines {
			line := make([]corpusChar, len(l.Chars))
			for i, ch := range l.Chars {
				line[i] = corpusChar{ch, l.WMode, l.Dir}
			}
			out = append(out, line)
		}
	}
	return out, nil
}

func flatChars(lines [][]corpusChar) []corpusChar {
	var out []corpusChar
	for _, l := range lines {
		out = append(out, l...)
	}
	return out
}

func charsText(lines [][]corpusChar) string {
	var sb strings.Builder
	for _, l := range lines {
		for _, ch := range l {
			sb.WriteRune(ch.Rune)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// lowRender renders p in gray without annotations at about 36 dpi, capped at
// 600 pixels on the long side.
func lowRender(p *Page, bound geometry.Rect, annots bool) (*Pixmap, float64, error) {
	side := math.Max(bound.Width(), bound.Height())
	if !(side > 0) {
		return nil, 0, errors.New("empty page")
	}
	z := math.Min(0.5, 600/side)
	pm, err := p.Pixmap(PixmapOptions{Zoom: z, Gray: true, NoAnnots: !annots})
	return pm, z, err
}

func (c *corpusCheck) extract(d *Document, n int) *corpusPage {
	pg := &corpusPage{n: n}
	c.step("page %d load", n)
	p, err := d.LoadPage(n)
	if err != nil {
		c.note("page-error", "page %d: LoadPage: %v", n, err)
		return pg
	}
	if pg.bound, err = p.Bound(); err != nil {
		c.note("page-error", "page %d: Bound: %v", n, err)
	}
	c.step("page %d rawdict", n)
	lines, rerr := corpusChars(p, TextOptions{})
	if rerr != nil {
		c.note("page-error", "page %d: RawDict: %v", n, rerr)
	}
	pg.lines, pg.raw = lines, charsText(lines)
	if c.useAT {
		c.step("page %d actualtext rawdict", n)
		if _, err := corpusChars(p, TextOptions{UseActualText: true}); err != nil {
			c.note("page-error", "page %d: RawDictWithOptions(UseActualText): %v", n, err)
		}
		c.step("page %d actualtext text", n)
		pg.atText, _ = p.ExtractText(TextOptions{UseActualText: true})
	}
	c.step("page %d gettext", n)
	var terr error
	if pg.text, terr = p.GetText(); terr != nil {
		c.note("page-error", "page %d: GetText: %v", n, terr)
	}
	pg.ok = rerr == nil && terr == nil
	c.step("page %d words", n)
	if _, err := p.Words(); err != nil {
		c.note("page-error", "page %d: Words: %v", n, err)
	}
	c.step("page %d widgets", n)
	if _, err := p.Widgets(); err != nil {
		c.note("page-error", "page %d: Widgets: %v", n, err)
	}
	c.step("page %d links", n)
	links, err := p.Links()
	if err != nil {
		c.note("page-error", "page %d: Links: %v", n, err)
	}
	pg.links = webLinks(links)
	c.step("page %d render", n)
	if _, _, err := lowRender(p, pg.bound, true); err != nil {
		c.note("page-error", "page %d: render: %v", n, err)
	}
	pg.render, pg.zoom, err = lowRender(p, pg.bound, false)
	if err != nil {
		c.note("page-error", "page %d: render without annotations: %v", n, err)
	}
	return pg
}

func webLinks(ls []Link) []string {
	var out []string
	for _, l := range ls {
		u := strings.ToLower(l.URI)
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "mailto:") {
			out = append(out, l.URI)
		}
	}
	return out
}

// pickWords chooses up to three words (runs of non-space characters within a
// line), preferring words of four or more characters that occur exactly once
// in the page text, so a string check after redaction is unambiguous.
func pickWords(pg *corpusPage, rng *rand.Rand) []corpusWord {
	var unique, other []corpusWord
	for _, l := range pg.lines {
		for i := 0; i < len(l); {
			if unicode.IsSpace(l[i].Rune) {
				i++
				continue
			}
			j := i
			for j < len(l) && !unicode.IsSpace(l[j].Rune) {
				j++
			}
			w := corpusWord{chars: l[i:j]}
			for _, ch := range w.chars {
				w.text += string(ch.Rune)
			}
			n := len(w.chars)
			if n >= 4 && !strings.ContainsRune(w.text, unicode.ReplacementChar) && strings.Count(pg.text, w.text) == 1 {
				w.unique = true
				unique = append(unique, w)
			} else if n >= 2 {
				other = append(other, w)
			}
			i = j
		}
	}
	pool, k := unique, 3
	if len(pool) == 0 {
		pool, k = other, 2
	}
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	return pool[:min(k, len(pool))]
}

func seedOf(path string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(filepath.Base(path)))
	return h.Sum64()
}

type corpusMode struct {
	name string
	add  func(p *Page, w corpusWord) error
	area func(w corpusWord) []geometry.Quad // the area the redaction covers
}

var corpusModes = []corpusMode{
	{"quads", func(p *Page, w corpusWord) error { return p.AddRedactionQuads(wordQuads(w), RedactOptions{}) }, wordQuads},
	{"rect", func(p *Page, w corpusWord) error { return p.AddRedaction(wordRect(w), RedactOptions{}) },
		func(w corpusWord) []geometry.Quad { return []geometry.Quad{wordRect(w).Quad()} }},
}

func wordQuads(w corpusWord) []geometry.Quad {
	qs := make([]geometry.Quad, len(w.chars))
	for i, ch := range w.chars {
		qs[i] = ch.Quad
	}
	return qs
}

func wordRect(w corpusWord) geometry.Rect {
	r := w.chars[0].BBox
	for _, ch := range w.chars[1:] {
		r = r.IncludeRect(ch.BBox)
	}
	return r
}

func (c *corpusCheck) redaction(data []byte, d *Document, pages []*corpusPage) {
	rng := rand.New(rand.NewPCG(seedOf(c.path), 1))
	picks := map[int][]corpusWord{}
	for _, pg := range pages {
		if pg.ok {
			if ws := pickWords(pg, rng); len(ws) > 0 {
				picks[pg.n] = ws
				c.r.RedactedWords += len(ws)
			}
		}
	}
	if len(picks) == 0 {
		c.note("no-text", "no page with words to redact")
		return
	}
	// The unredacted document saved the same way, for the byte comparison.
	c.step("redact baseline save")
	base, err := saveFresh(data)
	if err != nil {
		c.note("save-error", "unredacted save: %v", err)
		return
	}
	bc := byteCtx{baseDoc: base, base: pdfBlobs(base)}
	if n := d.PageCount(); n <= 300 {
		c.step("redact page texts")
		bc.text = make([]string, n)
		for i := range n {
			if p, err := d.LoadPage(i); err == nil {
				bc.text[i], _ = p.GetText()
			}
		}
	}

	for _, mode := range corpusModes {
		c.step("redact %s add", mode.name)
		dm, err := OpenStream(data)
		if err != nil {
			c.fail("reopen-error", "%s: second OpenStream: %v", mode.name, err)
			return
		}
		// Only words whose redaction was added, on pages where applying
		// worked, are verified: an error there is reported, and a service
		// would not ship that page.
		added := map[int][]corpusWord{}
		applied := map[int]int{}
		changed := false   // a RedactionCheckError: saves must now be refused
		var other []string // other ApplyRedactions errors
		for _, pg := range pages {
			p, err := dm.LoadPage(pg.n)
			if err != nil || len(picks[pg.n]) == 0 {
				continue
			}
			for _, w := range picks[pg.n] {
				if err := mode.add(p, w); err != nil {
					// An empty or non-finite rect is refused by design
					// (zero-height glyph boxes in rect mode).
					cat := "refused-add"
					if strings.Contains(err.Error(), "AddRedactionQuads") {
						cat = "refused-empty-rect"
					}
					c.note(cat, "%s page %d %q: %v", mode.name, pg.n, w.text, err)
				} else {
					added[pg.n] = append(added[pg.n], w)
				}
			}
			if len(added[pg.n]) == 0 {
				continue
			}
			c.step("redact %s apply page %d", mode.name, pg.n)
			n, err := p.ApplyRedactions(ApplyOptions{})
			var rce *RedactionCheckError
			switch {
			case err == nil:
			case errors.As(err, &rce):
				changed = true
				var kinds []string
				if errors.Is(err, ErrTextRemains) {
					kinds = append(kinds, "remains")
				}
				if errors.Is(err, ErrTextMoved) {
					kinds = append(kinds, "moved")
				}
				c.note("refused-check:"+strings.Join(kinds, "+"), "%s page %d words %s: %v", mode.name, pg.n, wordList(added[pg.n]), err)
			default:
				other = append(other, fmt.Sprintf("%s page %d: %v", mode.name, pg.n, err))
			}
			if err != nil {
				delete(added, pg.n)
			}
			applied[pg.n] = n
		}
		c.step("redact %s save", mode.name)
		out, err := dm.SaveBytesWithOptions(SaveOptions{Garbage: 1})
		// After a failed apply that changed the page, every save must be
		// refused with ErrRedactionFailed; after a preflight refusal, which
		// changes nothing, the save works.
		switch {
		case errors.Is(err, ErrRedactionFailed):
			dm.Close()
			if !changed && len(other) == 0 {
				c.fail("save-error", "%s: %v, with no failed ApplyRedactions", mode.name, err)
			}
			for _, o := range other {
				c.note("refused-apply", "%s (save then refused)", o)
			}
			c.note("refused-save", "%s: %v", mode.name, err)
			continue
		case err != nil:
			dm.Close()
			c.fail("save-error", "%s: %v", mode.name, err)
			continue
		case changed:
			c.fail("save-not-refused", "%s: save succeeded after ApplyRedactions returned a RedactionCheckError", mode.name)
		}
		for _, o := range other {
			c.note("refused-preflight", "%s (save allowed)", o)
		}
		// The whole pipeline: the same document after a default Scrub, for
		// the "byte-hit-scrubbed" notes (what survives redaction and scrub).
		bc.scrubbed = nil
		c.step("redact %s scrub save", mode.name)
		if dm.Scrub(ScrubOptions{}) == nil {
			if b, err := dm.SaveBytesWithOptions(SaveOptions{Garbage: 1}); err == nil {
				bc.scrubbed = pdfBlobs(b)
			}
		}
		dm.Close()
		c.step("redact %s reopen", mode.name)
		da, err := OpenStream(out)
		if err != nil {
			c.fail("reopen-error", "%s: %v", mode.name, err)
			continue
		}
		if da.PageCount() != d.PageCount() {
			c.fail("page-count-changed", "%s: %d pages, was %d", mode.name, da.PageCount(), d.PageCount())
		}
		bc.out = pdfBlobs(out)
		for _, pg := range pages {
			if ws := added[pg.n]; len(ws) > 0 {
				c.verifyPage(d, da, mode, pg, ws, len(ws), applied[pg.n], bc)
			}
		}
		da.Close()
	}
}

// byteCtx holds the unredacted and redacted saves split by pdfBlobs, and the
// text of every page (nil past 300 pages).
type byteCtx struct {
	baseDoc   []byte
	base, out [][]byte
	scrubbed  [][]byte // out's document after a default Scrub; nil if Scrub failed
	text      []string
}

// elsewhere reports whether word is in the text of a page other than n, or
// the document is too long to tell.
func (bc byteCtx) elsewhere(n int, word string) bool {
	if bc.text == nil {
		return true
	}
	for i, t := range bc.text {
		if i != n && strings.Contains(t, word) {
			return true
		}
	}
	return false
}

func (c *corpusCheck) verifyPage(d, da *Document, mode corpusMode, pg *corpusPage, words []corpusWord, added, applied int, bc byteCtx) {
	n := pg.n
	where := fmt.Sprintf("%s page %d", mode.name, n)
	p, err := da.LoadPage(n)
	if err != nil {
		c.fail("reopen-error", "%s: LoadPage: %v", where, err)
		return
	}
	c.step("verify %s rawdict", where)
	afterLines, err := corpusChars(p, TextOptions{})
	if err != nil {
		c.fail("reopen-error", "%s: RawDict: %v", where, err)
		return
	}
	after := indexChars(flatChars(afterLines))
	afterText, _ := p.GetText()
	var afterAT charIndex
	var afterATText string
	if c.useAT {
		c.step("verify %s actualtext rawdict", where)
		atLines, err := corpusChars(p, TextOptions{UseActualText: true})
		if err == nil {
			afterAT = indexChars(flatChars(atLines))
		}
		c.step("verify %s actualtext text", where)
		afterATText, _ = p.ExtractText(TextOptions{UseActualText: true})
	}

	// a. the redacted words are gone, at their positions and as strings
	var areas []geometry.Quad
	targets := map[geometry.Point]bool{}
	for _, w := range words {
		areas = append(areas, mode.area(w)...)
		var left, leftAT []string
		for _, ch := range w.chars {
			targets[ch.Origin] = true
			if after.has(ch) {
				left = append(left, fmt.Sprintf("%q@(%.1f,%.1f)", ch.Rune, ch.Origin.X, ch.Origin.Y))
			} else if afterAT != nil && afterAT.has(ch) {
				leftAT = append(leftAT, fmt.Sprintf("%q@(%.1f,%.1f)", ch.Rune, ch.Origin.X, ch.Origin.Y))
			}
		}
		ch0 := w.chars[0]
		ctx := func() string {
			return fmt.Sprintf("wmode=%d dir=(%.2f,%.2f) font=%q size=%.1fx%.1f", ch0.WMode, ch0.Dir.X, ch0.Dir.Y,
				fontAt(d, n, ch0), ch0.BBox.Width(), ch0.BBox.Height())
		}
		if len(left) > 0 {
			c.fail("under-removal", "%s word %q: %d/%d chars still extracted at their positions: %s; %s",
				where, w.text, len(left), len(w.chars), strings.Join(left, " "), ctx())
		}
		if w.unique && strings.Contains(afterText, w.text) {
			c.fail("text-survived", "%s word %q still in GetText; %s", where, w.text, ctx())
		}
		if len(leftAT) > 0 {
			c.fail("actualtext-survived", "%s word %q: with UseActualText, %s still extracted; %s", where, w.text, strings.Join(leftAT, " "), ctx())
		}
		if c.useAT && !strings.ContainsRune(w.text, unicode.ReplacementChar) {
			if before, now := strings.Count(pg.atText, w.text), strings.Count(afterATText, w.text); before > 0 && now >= before {
				c.fail("actualtext-survived", "%s word %q: occurs %d times in UseActualText text, %d before; %s", where, w.text, now, before, ctx())
			}
		}
		// b. byte search; see pdfNeedles. Extraction above is authoritative.
		// Conclusive only for a word whose text is on no other page and that
		// the unredacted save holds as a string; then every occurrence left
		// is reported, with the key naming it (/Alt, /URI, ...) or "stream".
		if !w.unique || bc.elsewhere(n, w.text) || countNeedles(bc.base, w.text) == 0 {
			c.r.ByteInconclusive++
		} else {
			c.r.ByteConclusive++
			if k, where2 := byteHits(bc.out, w.text); k > 0 {
				c.note("byte-hit", "%s word %q: %d occurrences in the save: %s", where, w.text, k, where2)
			}
			if bc.scrubbed != nil {
				if k, where2 := byteHits(bc.scrubbed, w.text); k > 0 {
					c.note("byte-hit-scrubbed", "%s word %q: %d occurrences after redaction and Scrub: %s", where, w.text, k, where2)
				}
			}
		}
	}

	// c. collateral: characters outside every redaction area survive in place
	if applied > added {
		c.note("preexisting-redaction", "%s: %d redactions applied, %d added; over-removal not checked", where, applied, added)
	} else {
		// A character found again within 6pt along x (and 1pt in y) has
		// moved rather than gone: a text-state change, also a bug.
		var lost, moved []string
		nLost, nMoved := 0, 0
		for _, ch := range flatChars(pg.lines) {
			if unicode.IsSpace(ch.Rune) || targets[ch.Origin] || after.has(ch) || touchesAny(ch.Quad, areas) {
				continue
			}
			desc := fmt.Sprintf("%q@(%.2f,%.2f)", ch.Rune, ch.Origin.X, ch.Origin.Y)
			if to, ok := after.near(ch, 6, 1); ok {
				if nMoved++; len(moved) < 6 {
					moved = append(moved, fmt.Sprintf("%s→(%.2f,%.2f)", desc, to.X, to.Y))
				}
			} else if nLost++; len(lost) < 8 {
				lost = append(lost, desc)
			}
		}
		if nLost > 0 {
			c.fail("over-removal", "%s: %d chars outside every redaction area are gone, e.g. %s (words %s)", where, nLost, strings.Join(lost, " "), wordList(words))
		}
		if nMoved > 0 {
			c.fail("text-moved", "%s: %d chars outside every redaction area moved, e.g. %s (words %s)", where, nMoved, strings.Join(moved, " "), wordList(words))
		}
	}

	// d. render, and pixels well outside the areas are unchanged
	c.step("verify %s render", where)
	if _, _, err := lowRender(p, pg.bound, true); err != nil {
		c.fail("render-error", "%s: %v", where, err)
	}
	pm, _, err := lowRender(p, pg.bound, false)
	if err != nil || pg.render == nil {
		return
	}
	if changed, total, at := pixelDiff(pg.render, pm, pg.zoom, pg.bound, areas); changed < 0 {
		c.fail("render-changed", "%s: render size %dx%d, was %dx%d", where, pm.Width, pm.Height, pg.render.Width, pg.render.Height)
	} else if changed > max(8, total/1000) {
		c.fail("render-changed", "%s: %d of %d pixels outside the redaction areas changed, first near (%.0f,%.0f) (words %s); %s",
			where, changed, total, at.X, at.Y, wordList(words), baseRenderDiff(bc.baseDoc, pg, areas))
	}
}

func wordList(ws []corpusWord) string {
	var s []string
	for _, w := range ws {
		s = append(s, strconv.Quote(w.text))
	}
	return strings.Join(s, ",")
}

// fontAt names the font of the span containing ch on page n of d, for reports.
func fontAt(d *Document, n int, ch corpusChar) string {
	p, err := d.LoadPage(n)
	if err != nil {
		return ""
	}
	blocks, err := p.Dict()
	if err != nil {
		return ""
	}
	cx, cy := (ch.BBox.X0+ch.BBox.X1)/2, (ch.BBox.Y0+ch.BBox.Y1)/2
	for _, b := range blocks {
		for _, s := range b.Spans {
			if s.BBox.ContainsPoint(geometry.Point{X: cx, Y: cy}) {
				return s.Font
			}
		}
	}
	return ""
}

type charIndex map[rune][]geometry.Point

func indexChars(cs []corpusChar) charIndex {
	ix := charIndex{}
	for _, ch := range cs {
		ix[ch.Rune] = append(ix[ch.Rune], ch.Origin)
	}
	return ix
}

// near returns the closest origin of ch's rune within dx and dy of ch.
func (ix charIndex) near(ch corpusChar, dx, dy float64) (geometry.Point, bool) {
	best, ok := geometry.Point{}, false
	for _, o := range ix[ch.Rune] {
		if math.Abs(o.X-ch.Origin.X) <= dx && math.Abs(o.Y-ch.Origin.Y) <= dy &&
			(!ok || o.DistanceToPoint(ch.Origin) < best.DistanceToPoint(ch.Origin)) {
			best, ok = o, true
		}
	}
	return best, ok
}

func (ix charIndex) has(ch corpusChar) bool {
	for _, o := range ix[ch.Rune] {
		if math.Abs(o.X-ch.Origin.X) <= corpusTol && math.Abs(o.Y-ch.Origin.Y) <= corpusTol {
			return true
		}
	}
	return false
}

// touchesAny reports whether q, shrunk towards its centre by a tenth of its
// width and height on each side (the test ApplyRedactions documents), meets
// any of the areas, so its removal is legitimate.
func touchesAny(q geometry.Quad, areas []geometry.Quad) bool {
	u := q.UR.Sub(q.UL).Mul(0.1)
	v := q.LL.Sub(q.UL).Mul(0.1)
	s := [4]geometry.Point{q.UL.Add(u).Add(v), q.UR.Sub(u).Add(v), q.LR.Sub(u).Sub(v), q.LL.Add(u).Sub(v)}
	for _, a := range areas {
		if polysMeet(s, [4]geometry.Point{a.UL, a.UR, a.LR, a.LL}, 0.05) {
			return true
		}
	}
	return false
}

// polysMeet is the separating-axis test for two convex quadrilaterals
// (degenerate ones included), with eps of slack.
func polysMeet(a, b [4]geometry.Point, eps float64) bool {
	axes := 0
	for _, poly := range [2][4]geometry.Point{a, b} {
		for i := range 4 {
			e := poly[(i+1)%4].Sub(poly[i])
			l := math.Hypot(e.X, e.Y)
			if l < 1e-9 {
				continue
			}
			axes++
			nx, ny := -e.Y/l, e.X/l
			amin, amax, bmin, bmax := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
			for k := range 4 {
				pa, pb := a[k].X*nx+a[k].Y*ny, b[k].X*nx+b[k].Y*ny
				amin, amax = math.Min(amin, pa), math.Max(amax, pa)
				bmin, bmax = math.Min(bmin, pb), math.Max(bmax, pb)
			}
			if amax < bmin-eps || bmax < amin-eps {
				return false
			}
		}
	}
	return axes > 0 || a[0].DistanceToPoint(b[0]) <= eps
}

// pixelDiff counts pixels that differ by more than 48 levels between two gray
// renders, ignoring a margin of 4pt plus two pixels around every area.
// changed is -1 when the renders differ in size.
func pixelDiff(a, b *Pixmap, z float64, bound geometry.Rect, areas []geometry.Quad) (changed, total int, first geometry.Point) {
	if a.Width != b.Width || a.Height != b.Height || a.N != b.N || a.Stride != b.Stride {
		return -1, 0, first
	}
	m := 4 + 2/z
	var boxes []geometry.Rect
	for _, q := range areas {
		r := q.Rect()
		boxes = append(boxes, geometry.Rect{X0: r.X0 - m, Y0: r.Y0 - m, X1: r.X1 + m, Y1: r.Y1 + m})
	}
	for y := range a.Height {
		for x := range a.Width {
			pt := geometry.Point{X: bound.X0 + (float64(x)+0.5)/z, Y: bound.Y0 + (float64(y)+0.5)/z}
			if slices.ContainsFunc(boxes, func(r geometry.Rect) bool { return r.ContainsPoint(pt) }) {
				continue
			}
			total++
			o := y*a.Stride + x*a.N
			for k := range a.N {
				if d := int(a.Samples[o+k]) - int(b.Samples[o+k]); d > 48 || d < -48 {
					if changed == 0 {
						first = pt
					}
					changed++
					break
				}
			}
		}
	}
	return changed, total, first
}

// baseRenderDiff says whether the unredacted save already renders pg
// differently, which separates a save problem from a redaction one.
func baseRenderDiff(base []byte, pg *corpusPage, areas []geometry.Quad) string {
	d, err := OpenStream(base)
	if err != nil {
		return "unredacted save does not open"
	}
	defer d.Close()
	p, err := d.LoadPage(pg.n)
	if err != nil {
		return "unredacted save: " + err.Error()
	}
	pm, _, err := lowRender(p, pg.bound, false)
	if err != nil {
		return "unredacted save: " + err.Error()
	}
	changed, _, _ := pixelDiff(pg.render, pm, pg.zoom, pg.bound, areas)
	return fmt.Sprintf("unredacted save: %d pixels changed", changed)
}

func saveFresh(data []byte) ([]byte, error) {
	d, err := OpenStream(data)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	return d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
}

// scrubWant is what HiddenContent must read after Scrub with opts, given
// ctrl, the unscrubbed document saved and reopened the same way: zero for
// everything Scrub owns; layers, and what the options keep, as in ctrl.
func scrubWant(opts ScrubOptions, ctrl HiddenContent) HiddenContent {
	w := HiddenContent{Layers: ctrl.Layers, HiddenLayers: ctrl.HiddenLayers}
	if opts.KeepFormFields || opts.KeepAnnotations {
		w.AcroForm, w.Widgets, w.Perms, w.DSS = ctrl.AcroForm, ctrl.Widgets, ctrl.Perms, ctrl.DSS
	}
	if opts.KeepAnnotations {
		w.NonLinkAnnotations = ctrl.NonLinkAnnotations
	}
	if opts.KeepOutlines {
		w.Outlines = ctrl.Outlines
	}
	return w
}

// outlinesKeepJS reports whether data, scrubbed keeping only its outlines,
// saved and reopened, still reads JavaScript (an outline walk cut short).
func outlinesKeepJS(data []byte) bool {
	d, err := OpenStream(data)
	if err != nil {
		return false
	}
	defer d.Close()
	if d.Scrub(ScrubOptions{KeepOutlines: true}) != nil {
		return false
	}
	out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
	if err != nil {
		return false
	}
	da, err := OpenStream(out)
	if err != nil {
		return false
	}
	defer da.Close()
	h, err := da.HiddenContent()
	return err == nil && h.JavaScript
}

// hcDiff lists the HiddenContent fields where got and want differ.
func hcDiff(got, want HiddenContent) string {
	var out []string
	g, w := reflect.ValueOf(got), reflect.ValueOf(want)
	for i := range g.NumField() {
		if !g.Field(i).Equal(w.Field(i)) {
			out = append(out, fmt.Sprintf("%s=%v want %v", g.Type().Field(i).Name, g.Field(i), w.Field(i)))
		}
	}
	return strings.Join(out, ", ")
}

func (c *corpusCheck) scrub(data []byte, pages []*corpusPage) {
	// The control: the same file saved and reopened without Scrub. The
	// keepannots mode also keeps outlines: an annotation on no page that only
	// an outline item's action references is otherwise dropped by the save
	// (as KeepAnnotations documents), which the control cannot predict.
	c.step("scrub control")
	d0, err := OpenStream(data)
	if err != nil {
		c.fail("reopen-error", "scrub control: %v", err)
		return
	}
	before, err := d0.HiddenContent()
	if err != nil {
		c.note("scrub-rejected", "HiddenContent: %v", err)
		d0.Close()
		return
	}
	if c.prefix == "" {
		c.r.Hidden = &before
	}
	ctrlPDF, err := d0.SaveBytesWithOptions(SaveOptions{Garbage: 1})
	d0.Close()
	if err != nil {
		c.fail("save-error", "scrub control: %v", err)
		return
	}
	dc, err := OpenStream(ctrlPDF)
	if err != nil {
		c.fail("reopen-error", "scrub control: %v", err)
		return
	}
	defer dc.Close()
	ctrl, err := dc.HiddenContent()
	if err != nil {
		c.fail("control-hidden-error", "HiddenContent of the unscrubbed save: %v", err)
		return
	}
	ctrlText, ctrlLinks := map[int]string{}, map[int][]string{}
	for _, pg := range pages {
		if p, err := dc.LoadPage(pg.n); err == nil {
			if lines, err := corpusChars(p, TextOptions{}); err == nil {
				ctrlText[pg.n] = charsText(lines)
			}
			if ls, err := p.Links(); err == nil {
				ctrlLinks[pg.n] = webLinks(ls)
			}
		}
	}

	for _, mode := range []struct {
		name string
		opts ScrubOptions
	}{{"default", ScrubOptions{}}, {"keepforms", ScrubOptions{KeepFormFields: true}}, {"keepannots", ScrubOptions{KeepAnnotations: true, KeepOutlines: true}}} {
		c.step("scrub %s", mode.name)
		d, err := OpenStream(data)
		if err != nil {
			c.fail("reopen-error", "scrub %s: %v", mode.name, err)
			return
		}
		if err := d.Scrub(mode.opts); err != nil {
			c.note("scrub-rejected", "Scrub %s: %v", mode.name, err)
			d.Close()
			continue
		}
		c.step("scrub %s save", mode.name)
		out, err := d.SaveBytesWithOptions(SaveOptions{Garbage: 1})
		d.Close()
		if err != nil {
			c.fail("save-error", "scrub %s: %v", mode.name, err)
			continue
		}
		c.step("scrub %s reopen", mode.name)
		da, err := OpenStream(out)
		if err != nil {
			c.fail("reopen-error", "scrub %s: %v", mode.name, err)
			continue
		}
		after, err := da.HiddenContent()
		if err != nil {
			c.fail("scrub-residue", "%s: HiddenContent after: %v", mode.name, err)
		} else if want := scrubWant(mode.opts, ctrl); after != want {
			// A kept outline that HiddenContent cannot walk in full (a /First
			// cycle) reads as JavaScript by design; accept that when a Scrub
			// that only keeps outlines reads the same.
			if mode.opts.KeepOutlines && after.JavaScript && !want.JavaScript && outlinesKeepJS(data) {
				want.JavaScript = true
			}
			if after != want {
				c.fail("scrub-residue", "%s: %s", mode.name, hcDiff(after, want))
			}
		}
		if da.PageCount() != dc.PageCount() {
			c.fail("page-count-changed", "scrub %s: %d pages, unscrubbed save has %d", mode.name, da.PageCount(), dc.PageCount())
		}
		for _, pg := range pages {
			c.step("scrub %s verify page %d", mode.name, pg.n)
			p, err := da.LoadPage(pg.n)
			if err != nil {
				continue
			}
			if want, ok := ctrlText[pg.n]; ok {
				lines, err := corpusChars(p, TextOptions{})
				if got := charsText(lines); err != nil || got != want {
					c.fail("scrub-text-changed", "%s page %d: %v; %s", mode.name, pg.n, err, firstDiff(want, got))
				}
			}
			if ls, err := p.Links(); err == nil {
				got := webLinks(ls)
				for _, u := range ctrlLinks[pg.n] {
					if !slices.Contains(got, u) {
						c.fail("scrub-lost-link", "%s page %d: %q", mode.name, pg.n, u)
						break
					}
				}
			}
			if _, _, err := lowRender(p, pg.bound, true); err != nil && pg.render != nil {
				c.fail("render-error", "scrub %s page %d: %v", mode.name, pg.n, err)
			}
		}
		da.Close()
	}
}

func firstDiff(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	clip := func(s string) string { return s[i:min(len(s), i+40)] }
	return fmt.Sprintf("lengths %d/%d, first difference at byte %d: %q vs %q", len(a), len(b), i, clip(a), clip(b))
}

// ---------------------------------------------------------------------------
// Raw bytes: stream decoding, byte search and tags. Pure Go; nothing here
// trusts the file.

var (
	streamRe = regexp.MustCompile(`stream\r?\n`)
	imageRe  = regexp.MustCompile(`/Subtype\s*/Image\b`)
)

// pdfBlobs splits data into everything outside stream bodies, followed by
// each stream body: inflated when the stream's dictionary mentions
// /FlateDecode (16 MiB per stream, 128 MiB in all; images are left alone),
// raw otherwise. Other filters are not decoded, so text under them is not
// searched.
func pdfBlobs(data []byte) [][]byte {
	var rest []byte
	var streams [][]byte
	budget := int64(128 << 20)
	prev := 0
	for _, loc := range streamRe.FindAllIndex(data, -1) {
		if loc[0] < prev || loc[0] >= 3 && string(data[loc[0]-3:loc[0]]) == "end" {
			continue
		}
		body := data[loc[1]:]
		if e := bytes.Index(body, []byte("endstream")); e >= 0 {
			body = body[:e]
		}
		rest = append(rest, data[prev:loc[1]]...)
		prev = loc[1] + len(body)
		dict := data[max(0, loc[0]-1024):loc[0]]
		if i := bytes.LastIndex(dict, []byte(" obj")); i >= 0 {
			dict = dict[i:]
		}
		if bytes.Contains(dict, []byte("/FlateDecode")) && !imageRe.Match(dict) && budget > 0 {
			if zr, err := zlib.NewReader(bytes.NewReader(body)); err == nil {
				var out bytes.Buffer
				io.Copy(&out, io.LimitReader(zr, min64(budget, 16<<20)))
				budget -= int64(out.Len())
				if out.Len() > 0 {
					body = out.Bytes()
				}
			}
		}
		streams = append(streams, body)
	}
	rest = append(rest, data[prev:]...)
	return append([][]byte{rest}, streams...)
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}

type needle struct {
	b   []byte
	hex bool
}

// pdfNeedles returns the encodings searched for word: its bytes when every
// rune is below U+0100 (ASCII/PDFDocEncoding/Latin-1 literal strings),
// UTF-16BE, and both as upper- and lower-case hex strings. A font with a
// custom encoding, or a word split by TJ kerning, escapes this search, which
// is why a count of zero before redaction is "inconclusive".
func pdfNeedles(word string) []needle {
	var out []needle
	var lat []byte
	for _, r := range word {
		if r > 0xff {
			lat = nil
			break
		}
		lat = append(lat, byte(r))
	}
	var u16 []byte
	for _, v := range utf16.Encode([]rune(word)) {
		u16 = append(u16, byte(v>>8), byte(v))
	}
	for _, b := range [][]byte{lat, u16} {
		if len(b) > 0 {
			h := hex.EncodeToString(b)
			out = append(out, needle{escapeLiteral(b), false}, needle{[]byte(h), true}, needle{[]byte(strings.ToUpper(h)), true})
		}
	}
	return out
}

// escapeLiteral writes b as it appears inside a literal string: '(', ')' and
// '\\' escaped, so the string's own closing delimiter never matches.
func escapeLiteral(b []byte) []byte {
	var out []byte
	for _, c := range b {
		if c == '(' || c == ')' || c == '\\' {
			out = append(out, '\\')
		}
		out = append(out, c)
	}
	return out
}

// needleHits returns the offsets of the needles in b that lie inside a
// string, with the offset of that string's opening delimiter: the nearest
// '(' (for hex, '<') in the 256 bytes before comes after the nearest ')' (for
// hex, '>'). That skips names such as /Image.
func needleHits(b []byte, nds []needle) (at, open []int) {
	for _, nd := range nds {
		o, cl := byte('('), byte(')')
		if nd.hex {
			o, cl = '<', '>'
		}
		for off := 0; ; {
			i := bytes.Index(b[off:], nd.b)
			if i < 0 {
				break
			}
			i += off
			lo := max(0, i-256)
			if k := bytes.LastIndexByte(b[lo:i], o); k > bytes.LastIndexByte(b[lo:i], cl) {
				at, open = append(at, i), append(open, lo+k)
			}
			off = i + 1
		}
	}
	return at, open
}

func countNeedles(blobs [][]byte, word string) int {
	nds, n := pdfNeedles(word), 0
	for _, b := range blobs {
		at, _ := needleHits(b, nds)
		n += len(at)
	}
	return n
}

var keyBeforeRe = regexp.MustCompile(`/([A-Za-z0-9]+)\s*$`)

// byteHits counts word in blobs (see needleHits) and says where: per PDF key
// naming the string (/Alt, /ActualText, /URI, /Title, ...), or "stream" for a
// string in a stream with no key before it (page content, mostly), with the
// first occurrence in context.
func byteHits(blobs [][]byte, word string) (int, string) {
	nds := pdfNeedles(word)
	keys := map[string]int{}
	n, first := 0, ""
	for k, b := range blobs {
		at, open := needleHits(b, nds)
		for j, i := range at {
			key := "stream"
			if k == 0 {
				key = "object"
			}
			if m := keyBeforeRe.FindSubmatch(b[max(0, open[j]-40):open[j]]); m != nil {
				key = "/" + string(m[1])
			}
			keys[key]++
			if n++; first == "" {
				first = fmt.Sprintf("%q", b[max(0, open[j]-30):min(len(b), i+len(word)*2+30)])
			}
		}
	}
	var parts []string
	for k, v := range keys {
		parts = append(parts, fmt.Sprintf("%s×%d", k, v))
	}
	sort.Strings(parts)
	return n, strings.Join(parts, " ") + "; first: " + first
}

var (
	tagRe = regexp.MustCompile(`/(Encrypt|AcroForm|XFA|JavaScript|JS|Launch|EmbeddedFile|StructTreeRoot|ActualText|Alt|Identity-V|` +
		`OCProperties|Linearized|ObjStm|ByteRange|Type3|SMask|Pattern|PieceInfo|URI|RichMedia)\b`)
	annotRe  = regexp.MustCompile(`/Subtype\s*/(Text|Highlight|Underline|StrikeOut|Squiggly|FreeText|Stamp|Ink|FileAttachment|Popup|Widget|Link|Redact|Square|Circle|Line|Polygon|PolyLine|Caret|Sound|Movie|Screen|Watermark)\b`)
	rotateRe = regexp.MustCompile(`/Rotate\s+(-?\d+)`)
	offRe    = regexp.MustCompile(`/OFF\s*\[\s*\d`)
	tr3Re    = regexp.MustCompile(`(^|\s)3\s+Tr\b`)
	uriRe    = regexp.MustCompile(`/URI\s*\(`)
	// producerRe reads /Producer, /Creator and their XMP forms.
	producerRe = regexp.MustCompile(`(?:/Producer|/Creator)\s*\(([^)]{1,120})|<(?:pdf:Producer|xmp:CreatorTool)>([^<]{1,120})`)
	producers  = map[string]*regexp.Regexp{
		"msoffice": regexp.MustCompile(`(?i)microsoft|word|excel|powerpoint`), "libreoffice": regexp.MustCompile(`(?i)libreoffice|openoffice|staroffice`),
		"chrome": regexp.MustCompile(`(?i)skia|chrom`), "quartz": regexp.MustCompile(`(?i)quartz|mac os`), "tex": regexp.MustCompile(`(?i)tex|dvips|dvipdf`),
		"google": regexp.MustCompile(`(?i)google`), "adobe": regexp.MustCompile(`(?i)acrobat|distiller|adobe|indesign|illustrator`),
		"ghostscript": regexp.MustCompile(`(?i)ghostscript|gpl`), "scanner": regexp.MustCompile(`(?i)scan|ocr|abbyy|finereader|tesseract|paper ?port`),
	}
)

// pdfTags lists cheap features found by scanning the raw and inflated bytes.
func pdfTags(blobs [][]byte) []string {
	set := map[string]bool{}
	for _, b := range blobs {
		for _, m := range tagRe.FindAllSubmatch(b, -1) {
			set[strings.ToLower(string(m[1]))] = true
		}
		for _, m := range annotRe.FindAllSubmatch(b, -1) {
			set["annot:"+string(m[1])] = true
		}
		for _, m := range rotateRe.FindAllSubmatch(b, -1) {
			if v, _ := strconv.Atoi(string(m[1])); v%360 != 0 {
				set["rotated"] = true
			}
		}
		if offRe.Match(b) {
			set["oc-off"] = true
		}
		if tr3Re.Match(b) {
			set["invisible-text"] = true
		}
		if n := len(uriRe.FindAllIndex(b, 1001)); n > 1000 {
			set["uri:1000+"] = true
		}
		for _, m := range producerRe.FindAllSubmatch(b, 8) {
			v := string(m[1]) + string(m[2])
			for tag, re := range producers {
				if re.MatchString(v) {
					set["producer:"+tag] = true
				}
			}
		}
	}
	if imageRe.Match(blobs[0]) {
		set["image"] = true
	}
	if n := bytes.Count(blobs[0], []byte("%%EOF")); n > 1 {
		set[fmt.Sprintf("eof:%d", n)] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
