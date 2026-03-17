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

// Helper to create a minimal 1x1 PNG in memory.
func makePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// Helper to create a minimal 1x1 JPEG in memory.
func makeJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{0, 255, 0, 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// Helper to create a minimal 1x1 GIF in memory.
func makeGIF(t *testing.T) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White})
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image: []*image.Paletted{img},
		Delay: []int{0},
	}); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

// makeWebP returns a synthetic WebP header (RIFF....WEBP) with enough bytes
// for format detection. This is not a valid decodable WebP — just enough for
// magic byte checking.
func makeWebPHeader() []byte {
	data := make([]byte, 24)
	copy(data[0:4], []byte("RIFF"))
	// bytes 4-7: file size (little-endian) — arbitrary for testing
	data[4] = 0x10
	data[5] = 0x00
	data[6] = 0x00
	data[7] = 0x00
	copy(data[8:12], []byte("WEBP"))
	// VP8 chunk marker
	copy(data[12:16], []byte("VP8 "))
	return data
}

func TestDetectFormat_PNG(t *testing.T) {
	data := makePNG(t)
	info, err := DetectFormat(data)
	if err != nil {
		t.Fatalf("DetectFormat PNG: %v", err)
	}
	if info.Format != "png" {
		t.Errorf("format = %q, want %q", info.Format, "png")
	}
	if info.MIMEType != "image/png" {
		t.Errorf("mime = %q, want %q", info.MIMEType, "image/png")
	}
	if info.Extension != ".png" {
		t.Errorf("ext = %q, want %q", info.Extension, ".png")
	}
	if info.SizeBytes != len(data) {
		t.Errorf("size = %d, want %d", info.SizeBytes, len(data))
	}
}

func TestDetectFormat_JPEG(t *testing.T) {
	data := makeJPEG(t)
	info, err := DetectFormat(data)
	if err != nil {
		t.Fatalf("DetectFormat JPEG: %v", err)
	}
	if info.Format != "jpeg" {
		t.Errorf("format = %q, want %q", info.Format, "jpeg")
	}
	if info.MIMEType != "image/jpeg" {
		t.Errorf("mime = %q, want %q", info.MIMEType, "image/jpeg")
	}
	if info.Extension != ".jpg" {
		t.Errorf("ext = %q, want %q", info.Extension, ".jpg")
	}
}

func TestDetectFormat_GIF(t *testing.T) {
	data := makeGIF(t)
	info, err := DetectFormat(data)
	if err != nil {
		t.Fatalf("DetectFormat GIF: %v", err)
	}
	if info.Format != "gif" {
		t.Errorf("format = %q, want %q", info.Format, "gif")
	}
	if info.MIMEType != "image/gif" {
		t.Errorf("mime = %q, want %q", info.MIMEType, "image/gif")
	}
	if info.Extension != ".gif" {
		t.Errorf("ext = %q, want %q", info.Extension, ".gif")
	}
}

func TestDetectFormat_WebP(t *testing.T) {
	data := makeWebPHeader()
	info, err := DetectFormat(data)
	if err != nil {
		t.Fatalf("DetectFormat WebP: %v", err)
	}
	if info.Format != "webp" {
		t.Errorf("format = %q, want %q", info.Format, "webp")
	}
	if info.MIMEType != "image/webp" {
		t.Errorf("mime = %q, want %q", info.MIMEType, "image/webp")
	}
	if info.Extension != ".webp" {
		t.Errorf("ext = %q, want %q", info.Extension, ".webp")
	}
}

func TestDetectFormat_Unknown(t *testing.T) {
	data := make([]byte, 64)
	// Fill with non-image bytes.
	for i := range data {
		data[i] = byte(i + 1)
	}
	info, err := DetectFormat(data)
	if err != nil {
		t.Fatalf("DetectFormat unknown: %v", err)
	}
	if info.Format != "unknown" {
		t.Errorf("format = %q, want %q", info.Format, "unknown")
	}
	if info.Extension != ".bin" {
		t.Errorf("ext = %q, want %q", info.Extension, ".bin")
	}
}

func TestDetectFormat_TooShort(t *testing.T) {
	data := []byte{0x89, 0x50}
	_, err := DetectFormat(data)
	if err == nil {
		t.Fatal("expected error for too-short data, got nil")
	}
}

func TestValidatePNG_Valid(t *testing.T) {
	data := makePNG(t)
	if !ValidatePNG(data) {
		t.Error("ValidatePNG returned false for valid PNG")
	}
}

func TestValidatePNG_Invalid(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07}
	if ValidatePNG(data) {
		t.Error("ValidatePNG returned true for non-PNG data")
	}
}

func TestValidatePNG_TooShort(t *testing.T) {
	data := []byte{0x89, 0x50}
	if ValidatePNG(data) {
		t.Error("ValidatePNG returned true for too-short data")
	}
}
