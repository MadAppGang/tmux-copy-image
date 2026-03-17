package imageutil

import (
	"bytes"
	"errors"
	"image"

	// Register standard image decoders.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"golang.org/x/image/webp"
)

// ErrUnsupportedFormat is returned when the MIME type has no dimension extractor.
var ErrUnsupportedFormat = errors.New("unsupported image format for dimension extraction")

// ExtractDimensions extracts image width and height from the raw bytes without
// fully decoding the image. On failure it returns 0, 0, nil (graceful failure)
// so the caller can still serve the image without dimension metadata.
func ExtractDimensions(data []byte, mimeType string) (width, height int, err error) {
	switch mimeType {
	case "image/png":
		return pngDimensions(data)
	case "image/jpeg":
		return jpegDimensions(data)
	case "image/gif":
		return gifDimensions(data)
	case "image/webp":
		return webpDimensions(data)
	default:
		return 0, 0, ErrUnsupportedFormat
	}
}

func pngDimensions(data []byte) (int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, nil // graceful failure
	}
	return cfg.Width, cfg.Height, nil
}

func jpegDimensions(data []byte) (int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, nil // graceful failure
	}
	return cfg.Width, cfg.Height, nil
}

func gifDimensions(data []byte) (int, int, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, nil // graceful failure
	}
	return cfg.Width, cfg.Height, nil
}

// webpDimensions extracts dimensions from WebP data.
// The golang.org/x/image/webp package can panic on malformed VP8/VP8L bitstreams,
// so we wrap the call in a recover() to prevent daemon crashes.
func webpDimensions(data []byte) (width, height int, err error) {
	defer func() {
		if r := recover(); r != nil {
			// Malformed WebP: return graceful zero values.
			width, height, err = 0, 0, nil
		}
	}()

	cfg, err := webp.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, nil // graceful failure
	}
	return cfg.Width, cfg.Height, nil
}
