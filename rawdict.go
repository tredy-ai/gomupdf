package gomupdf

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tredy-ai/gomupdf/geometry"
)

// Char is a single extracted glyph with its geometry.
type Char struct {
	Rune   rune           // the Unicode code point; U+FFFD if MuPDF reported an invalid one
	Origin geometry.Point // glyph origin (baseline start), PDF points
	BBox   geometry.Rect  // glyph bounding box, PDF points
}

// TextLine is a line of characters within a block.
type TextLine struct {
	BBox  geometry.Rect
	Chars []Char
}

// TextSpan groups consecutive chars sharing font and size (PyMuPDF span).
type TextSpan struct {
	Font string // "" when the font has no name
	Size float64
	BBox geometry.Rect
	Text string
}

// RawBlock is a text block with its lines (rawdict: per-char) or spans (dict).
type RawBlock struct {
	BBox  geometry.Rect
	Lines []TextLine // populated by RawDict (per-char detail)
	Spans []TextSpan // populated by Dict (font/size runs)
}

// rawDictC returns the backend's raw text output.
func (p *Page) rawDictC(opts TextOptions) (string, error) {
	d := p.doc
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.b == nil {
		return "", errors.New("gomupdf: document closed")
	}
	return d.b.rawDictRaw(p.Number, opts)
}

// charMeta is a character's font and size, which Dict groups spans by.
type charMeta struct {
	font string
	size float64
}

// parseRawDict parses the backend output (see gomupdf_rawdict) into blocks,
// plus each character's font and size in output order. Each B record starts a
// block, L a line, C a character. Fields are separated by single spaces; the
// font name comes last and is "-" when the font has none.
//
// A record it cannot parse (too few fields, a bad number, an unknown type, or
// an L or C record with no block or line to belong to) is an error naming the
// line. It never skips one: a skipped character would shift every later
// offset on the page and hide its text.
func parseRawDict(s string) ([]RawBlock, []charMeta, error) {
	var (
		blocks   []RawBlock
		metas    []charMeta
		curBlock *RawBlock
		curLine  *TextLine
	)
	for n, line := range strings.Split(s, "\n") {
		if line == "" {
			continue // the output ends with a newline
		}
		fail := func(why string) ([]RawBlock, []charMeta, error) {
			return nil, nil, fmt.Errorf("gomupdf: rawdict line %d: %s: %q", n+1, why, line)
		}
		typ, _, _ := strings.Cut(line, " ")
		want := 5 // B|L x0 y0 x1 y1
		if typ == "C" {
			want = 10 // C rune ox oy x0 y0 x1 y1 size fontname
		} else if typ != "B" && typ != "L" {
			return fail("unknown record type")
		}
		f := strings.SplitN(line, " ", want)
		if len(f) < want {
			return fail("too few fields")
		}
		nums := f[1:]
		if typ == "C" {
			nums = f[2:9]
		}
		v := make([]float64, len(nums))
		for i, x := range nums {
			var err error
			if v[i], err = strconv.ParseFloat(x, 64); err != nil {
				return fail("bad number " + strconv.Quote(x))
			}
		}
		switch typ {
		case "B":
			blocks = append(blocks, RawBlock{BBox: geometry.Rect{X0: v[0], Y0: v[1], X1: v[2], Y1: v[3]}})
			curBlock = &blocks[len(blocks)-1]
			curLine = nil
		case "L":
			if curBlock == nil {
				return fail("L record outside a block")
			}
			curBlock.Lines = append(curBlock.Lines, TextLine{BBox: geometry.Rect{X0: v[0], Y0: v[1], X1: v[2], Y1: v[3]}})
			curLine = &curBlock.Lines[len(curBlock.Lines)-1]
		case "C":
			if curLine == nil {
				return fail("C record outside a line")
			}
			cp, err := strconv.ParseInt(f[1], 10, 32)
			if err != nil {
				return fail("bad code point " + strconv.Quote(f[1]))
			}
			r := rune(cp)
			if !utf8.ValidRune(r) { // negative, surrogate or above U+10FFFF
				r = utf8.RuneError
			}
			curLine.Chars = append(curLine.Chars, Char{
				Rune:   r,
				Origin: geometry.Point{X: v[0], Y: v[1]},
				BBox:   geometry.Rect{X0: v[2], Y0: v[3], X1: v[4], Y1: v[5]},
			})
			font := f[9]
			if font == "-" {
				font = ""
			}
			metas = append(metas, charMeta{font: font, size: v[6]})
		}
	}
	return blocks, metas, nil
}

// buildSpans converts RawBlocks (with Lines) into span-grouped RawBlocks.
// Consecutive chars in a line sharing (font, size) are merged into one TextSpan.
// metas holds each char's font and size in order, as parseRawDict returns them.
func buildSpans(raw []RawBlock, metas []charMeta) []RawBlock {
	mi := 0
	out := make([]RawBlock, 0, len(raw))
	for _, rb := range raw {
		nb := RawBlock{BBox: rb.BBox}
		for _, tl := range rb.Lines {
			var curSpan *TextSpan
			for _, ch := range tl.Chars {
				font, size := metas[mi].font, metas[mi].size
				mi++
				if curSpan == nil || curSpan.Font != font || curSpan.Size != size {
					nb.Spans = append(nb.Spans, TextSpan{
						Font: font,
						Size: size,
						BBox: ch.BBox,
						Text: string(ch.Rune),
					})
					curSpan = &nb.Spans[len(nb.Spans)-1]
				} else {
					curSpan.Text += string(ch.Rune)
					curSpan.BBox = curSpan.BBox.IncludeRect(ch.BBox)
				}
			}
		}
		out = append(out, nb)
	}
	return out
}

// RawDict returns the page's text blocks down to per-character geometry.
// It is RawDictWithOptions(TextOptions{}).
//
// Changed from upstream: a character whose font has no name is returned
// (upstream dropped it), an invalid code point becomes U+FFFD, and output the
// parser cannot read is an error instead of being skipped line by line.
func (p *Page) RawDict() ([]RawBlock, error) {
	return p.RawDictWithOptions(TextOptions{})
}

// RawDictWithOptions is RawDict with explicit structured-text options, which
// map to the same MuPDF flags as for ExtractText. Callers that map text
// offsets to geometry should pin their options and keep them fixed.
//
// opts.Clip is honored as ExtractText honors it: only characters whose bbox
// intersects Clip are returned, and blocks and lines left with no characters
// are omitted. Block and line bboxes are MuPDF's, not trimmed to Clip.
//
// New in the tredy-ai fork.
func (p *Page) RawDictWithOptions(opts TextOptions) ([]RawBlock, error) {
	s, err := p.rawDictC(opts)
	if err != nil {
		return nil, err
	}
	blocks, _, err := parseRawDict(s)
	return blocks, err
}

// Dict returns the page's text blocks grouped into font/size spans.
//
// Changed from upstream: as for RawDict, no character is dropped; a font with
// no name gives a span with Font "", and unreadable output is an error.
func (p *Page) Dict() ([]RawBlock, error) {
	s, err := p.rawDictC(TextOptions{})
	if err != nil {
		return nil, err
	}
	raw, metas, err := parseRawDict(s)
	if err != nil {
		return nil, err
	}
	return buildSpans(raw, metas), nil
}
