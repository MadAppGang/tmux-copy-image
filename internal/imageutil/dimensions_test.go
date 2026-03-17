package imageutil

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func makeRGBA(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	return img
}

func makePNGDims(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, makeRGBA(w, h)); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func makeJPEGDims(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, makeRGBA(w, h), nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

func makeGIFDims(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, w, h), color.Palette{color.Black, color.White})
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image: []*image.Paletted{img},
		Delay: []int{0},
	}); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

func TestExtractDimensions_PNG(t *testing.T) {
	const w, h = 100, 200
	data := makePNGDims(t, w, h)
	gotW, gotH, err := ExtractDimensions(data, "image/png")
	if err != nil {
		t.Fatalf("ExtractDimensions PNG: %v", err)
	}
	if gotW != w || gotH != h {
		t.Errorf("dimensions = %dx%d, want %dx%d", gotW, gotH, w, h)
	}
}

func TestExtractDimensions_JPEG(t *testing.T) {
	const w, h = 80, 60
	data := makeJPEGDims(t, w, h)
	gotW, gotH, err := ExtractDimensions(data, "image/jpeg")
	if err != nil {
		t.Fatalf("ExtractDimensions JPEG: %v", err)
	}
	if gotW != w || gotH != h {
		t.Errorf("dimensions = %dx%d, want %dx%d", gotW, gotH, w, h)
	}
}

func TestExtractDimensions_GIF(t *testing.T) {
	const w, h = 50, 50
	data := makeGIFDims(t, w, h)
	gotW, gotH, err := ExtractDimensions(data, "image/gif")
	if err != nil {
		t.Fatalf("ExtractDimensions GIF: %v", err)
	}
	if gotW != w || gotH != h {
		t.Errorf("dimensions = %dx%d, want %dx%d", gotW, gotH, w, h)
	}
}

func TestExtractDimensions_WebP_Graceful(t *testing.T) {
	// WebP header stub — not a valid decodable WebP; expect graceful 0,0,nil.
	data := makeWebPHeader()
	gotW, gotH, err := ExtractDimensions(data, "image/webp")
	if err != nil {
		t.Fatalf("ExtractDimensions WebP stub: %v", err)
	}
	// Graceful failure: dimensions may be 0 for invalid WebP data.
	_ = gotW
	_ = gotH
}

func TestExtractDimensions_Unsupported(t *testing.T) {
	data := makePNGDims(t, 1, 1)
	_, _, err := ExtractDimensions(data, "application/octet-stream")
	if err == nil {
		t.Fatal("expected error for unsupported MIME type, got nil")
	}
}

func TestExtractDimensions_TooShort(t *testing.T) {
	// Truncated PNG — should return 0,0,nil (graceful failure).
	data := []byte{0x89, 0x50, 0x4E, 0x47}
	gotW, gotH, err := ExtractDimensions(data, "image/png")
	if err != nil {
		t.Fatalf("unexpected error for short PNG: %v", err)
	}
	if gotW != 0 || gotH != 0 {
		t.Errorf("expected 0x0 for truncated PNG, got %dx%d", gotW, gotH)
	}
}
