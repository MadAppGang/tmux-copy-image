// Package imageutil provides image format detection and dimension extraction
// without fully decoding image data.
package imageutil

import (
	"errors"
	"net/http"
)

// ImageInfo describes a detected image format and its metadata.
type ImageInfo struct {
	Format    string // "png", "jpeg", "gif", "webp", "unknown"
	MIMEType  string // "image/png", "image/jpeg", "image/gif", "image/webp", "application/octet-stream"
	Extension string // ".png", ".jpg", ".gif", ".webp", ".bin"
	Width     int    // 0 if extraction failed or not attempted
	Height    int    // 0 if extraction failed or not attempted
	SizeBytes int    // total byte count
}

// ErrTooShort is returned when data is too short to detect a format.
var ErrTooShort = errors.New("image data too short to detect format")

// pngMagic is the PNG file signature (8 bytes).
var pngMagic = []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A}

type formatInfo struct {
	format    string
	extension string
}

var mimeToFormat = map[string]formatInfo{
	"image/png":  {"png", ".png"},
	"image/jpeg": {"jpeg", ".jpg"},
	"image/gif":  {"gif", ".gif"},
	"image/webp": {"webp", ".webp"},
}

// DetectFormat detects the image format from raw bytes using magic byte
// detection. It does not perform full image decoding.
//
// Detection steps:
//  1. Length check — minimum 12 bytes required.
//  2. http.DetectContentType — covers PNG, JPEG, GIF natively.
//  3. Explicit WebP check (RIFF....WEBP) for bytes that DetectContentType
//     classifies as application/octet-stream.
//  4. If still unrecognized, returns format "unknown" and extension ".bin".
func DetectFormat(data []byte) (ImageInfo, error) {
	if len(data) < 12 {
		return ImageInfo{}, ErrTooShort
	}

	info := ImageInfo{
		SizeBytes: len(data),
	}

	// Use at most 512 bytes for detection.
	sample := data
	if len(sample) > 512 {
		sample = data[:512]
	}

	mimeType := http.DetectContentType(sample)

	// http.DetectContentType returns "application/octet-stream" for WebP.
	if mimeType == "application/octet-stream" && isWebP(data) {
		mimeType = "image/webp"
	}

	if fi, ok := mimeToFormat[mimeType]; ok {
		info.Format = fi.format
		info.Extension = fi.extension
		info.MIMEType = mimeType
	} else {
		info.Format = "unknown"
		info.Extension = ".bin"
		info.MIMEType = "application/octet-stream"
	}

	return info, nil
}

// isWebP checks whether data starts with the WebP signature: "RIFF" at bytes
// 0-3 and "WEBP" at bytes 8-11.
func isWebP(data []byte) bool {
	if len(data) < 12 {
		return false
	}
	return data[0] == 'R' && data[1] == 'I' && data[2] == 'F' && data[3] == 'F' &&
		data[8] == 'W' && data[9] == 'E' && data[10] == 'B' && data[11] == 'P'
}

// ValidatePNG checks whether data begins with the PNG magic bytes.
// Used as a lightweight security check before serving clipboard content.
func ValidatePNG(data []byte) bool {
	if len(data) < len(pngMagic) {
		return false
	}
	for i, b := range pngMagic {
		if data[i] != b {
			return false
		}
	}
	return true
}
