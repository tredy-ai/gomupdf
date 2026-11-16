package gomupdf

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/tredy-ai/gomupdf/geometry"
)

// redactArea is the region redacted in the option tests.
var redactArea = geometry.Rect{X0: 100, Y0: 100, X1: 300, Y1: 300}

func newBlankPage(t *testing.T) (*Document, *Page) {
	t.Helper()
	d, err := NewPDF()
	if err != nil {
		t.Fatalf("NewPDF: %v", err)
	}
	if err := d.NewPage(400, 400); err != nil {
		d.Close()
		t.Fatalf("NewPage: %v", err)
	}
	p, err := d.LoadPage(0)
	if err != nil {
		d.Close()
		t.Fatalf("LoadPage: %v", err)
	}
	return d, p
}

func redPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			img.Set(x, y, color.RGBA{255, 0, 0, 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// px renders the page (RGB, 72 DPI) and returns the R, G, B bytes at (x, y).
func px(t *testing.T, p *Page, x, y int) [3]byte {
	t.Helper()
	pm, err := p.Pixmap()
	if err != nil {
		t.Fatalf("Pixmap: %v", err)
	}
	if pm.N != 3 {
		t.Fatalf("pixmap N = %d, want 3 (RGB)", pm.N)
	}
	s := pm.Pixel(x, y)
	return [3]byte{s[0], s[1], s[2]}
}

func mustApply(t *testing.T, p *Page, o ApplyOptions) {
	t.Helper()
	if err := p.AddRedaction(redactArea, RedactOptions{}); err != nil {
		t.Fatalf("AddRedaction: %v", err)
	}
	n, err := p.ApplyRedactions(o)
	if err != nil {
		t.Fatalf("ApplyRedactions: %v", err)
	}
	if n != 1 {
		t.Fatalf("applied %d, want 1", n)
	}
}

var (
	black = [3]byte{0, 0, 0}
	white = [3]byte{255, 255, 255}
	red   = [3]byte{255, 0, 0}
)

func TestApplyZeroOptionsDrawsBox(t *testing.T) {
	d, p := newBlankPage(t)
	defer d.Close()
	mustApply(t, p, ApplyOptions{})
	if got := px(t, p, 150, 150); got != black {
		t.Errorf("inside area = %v, want black", got)
	}
	if got := px(t, p, 350, 350); got != white {
		t.Errorf("outside area = %v, want white", got)
	}
}

func TestApplyNoBox(t *testing.T) {
	d, p := newBlankPage(t)
	defer d.Close()
	mustApply(t, p, ApplyOptions{NoBox: true})
	if got := px(t, p, 150, 150); got != white {
		t.Errorf("inside area with NoBox = %v, want white", got)
	}
}

func TestApplyImageMethods(t *testing.T) {
	cases := []struct {
		name      string
		method    ImageMethod
		wantImage bool
		wantInner [3]byte // pixel inside the area, drawn without a box
	}{
		{"pixels", ImagePixels, true, white},
		{"none", ImageNone, true, red},
		{"remove", ImageRemove, false, white},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, p := newBlankPage(t)
			defer d.Close()
			if err := p.InsertImage(geometry.NewRect(0, 0, 400, 400), redPNG(t)); err != nil {
				t.Fatalf("InsertImage: %v", err)
			}
			mustApply(t, p, ApplyOptions{NoBox: true, Images: c.method})
			imgs, err := p.GetImages()
			if err != nil {
				t.Fatalf("GetImages: %v", err)
			}
			if (len(imgs) > 0) != c.wantImage {
				t.Errorf("images after apply = %d, want present=%v", len(imgs), c.wantImage)
			}
			wantOuter := red
			if !c.wantImage {
				wantOuter = white
			}
			if got := px(t, p, 350, 350); got != wantOuter {
				t.Errorf("outside area = %v, want %v", got, wantOuter)
			}
			if got := px(t, p, 150, 150); got != c.wantInner {
				t.Errorf("inside area = %v, want %v", got, c.wantInner)
			}
		})
	}
}

func TestApplyTextNoneKeepsText(t *testing.T) {
	d, p := newRedactDoc(t)
	defer d.Close()
	if err := p.AddRedaction(secretRect, RedactOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ApplyRedactions(ApplyOptions{Text: TextNone}); err != nil {
		t.Fatal(err)
	}
	txt, err := p.GetText()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(txt, "SECRET") {
		t.Errorf("TextNone removed the text: %q", strings.TrimSpace(txt))
	}
}

func TestApplyLineArt(t *testing.T) {
	cases := []struct {
		name                  string
		method                LineArtMethod
		wantInside, wantCross [3]byte
	}{
		{"none", LineArtNone, red, red},
		{"covered", LineArtRemoveIfCovered, white, red},
		{"touched", LineArtRemoveIfTouched, white, white},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d, p := newBlankPage(t)
			defer d.Close()
			fill := [3]float64{1, 0, 0}
			o := DrawOptions{Fill: &fill, Stroke: &fill}
			// Raw PDF coordinates (y up), so both are symmetric about the page centre.
			if err := p.DrawRect(geometry.NewRect(150, 150, 250, 250), o); err != nil {
				t.Fatal(err)
			}
			if err := p.DrawRect(geometry.NewRect(250, 150, 350, 250), o); err != nil {
				t.Fatal(err)
			}
			mustApply(t, p, ApplyOptions{NoBox: true, LineArt: c.method})
			if got := px(t, p, 200, 200); got != c.wantInside {
				t.Errorf("covered rect = %v, want %v", got, c.wantInside)
			}
			if got := px(t, p, 330, 200); got != c.wantCross {
				t.Errorf("straddling rect outside area = %v, want %v", got, c.wantCross)
			}
		})
	}
}

func TestApplyInvalidOptions(t *testing.T) {
	for _, o := range []ApplyOptions{
		{Images: ImageMethod(3)},
		{Images: ImageMethod(-1)},
		{LineArt: LineArtMethod(3)},
		{Text: TextMethod(2)},
		// Values that wrap to a valid enum when truncated to C int.
		{Images: ImageMethod(1<<32 + 1)},
		{LineArt: LineArtMethod(1<<32 + 1)},
		{Text: TextMethod(1<<32 + 1)},
	} {
		d, p := newBlankPage(t)
		if err := p.AddRedaction(redactArea, RedactOptions{}); err != nil {
			t.Fatal(err)
		}
		if _, err := p.ApplyRedactions(o); err == nil {
			t.Errorf("ApplyRedactions(%+v): expected error", o)
		}
		d.Close()
	}
}
