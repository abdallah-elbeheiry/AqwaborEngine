package ui

import (
	"bytes"
	"compress/gzip"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func writeJPEG(t *testing.T, name string, w, h int) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x * 6), uint8(y * 6), 100, 255})
		}
	}
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, nil); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	return p
}

func writeSVG(t *testing.T, name, svg string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(svg), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeSVGZ(t *testing.T, name, svg string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	if _, err := gz.Write([]byte(svg)); err != nil {
		t.Fatal(err)
	}
	_ = gz.Close()
	if err := os.WriteFile(p, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestImageLoadJPEGSVG(t *testing.T) {
	const rectSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24">` +
		`<rect x="0" y="0" width="24" height="24" fill="#ff0000"/></svg>`

	cases := []struct {
		name string
		path string
		// wantMin is the expected minimum raster dimension (SVG is scaled up).
		wantW, wantH int
		checkRed     bool
	}{
		{"jpeg", writeJPEG(t, "a.jpg", 40, 30), 40, 30, false},
		{"svg", writeSVG(t, "a.svg", rectSVG), 512, 512, true},
		{"svgz", writeSVGZ(t, "a.svgz", rectSVG), 512, 512, true},
	}

	mgr := NewImageManager()
	for _, tc := range cases {
		a, err := mgr.Load(tc.path)
		if err != nil {
			t.Fatalf("%s: load: %v", tc.name, err)
		}
		if a.width < tc.wantW || a.height < tc.wantH {
			t.Fatalf("%s: size %dx%d, want >= %dx%d", tc.name, a.width, a.height, tc.wantW, tc.wantH)
		}
		img, ok := a.take()
		if !ok || img == nil {
			t.Fatalf("%s: take returned empty", tc.name)
		}
		if tc.checkRed {
			b := img.Bounds()
			found := false
			for y := b.Min.Y; y < b.Max.Y && !found; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					r, g, bl, al := img.At(x, y).RGBA()
					if r > 0 && al > 0 && g == 0 && bl == 0 {
						found = true
						break
					}
				}
			}
			if !found {
				t.Fatalf("%s: raster missing expected red pixel", tc.name)
			}
		}
	}
}
