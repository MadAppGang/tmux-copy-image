// Package clipboard provides the Backend interface and implementations for
// reading image data from the system clipboard on various platforms.
package clipboard

import (
	"context"
	"errors"
)

// Backend is the platform-specific clipboard access interface.
// The daemon holds one instance, selected at startup by DetectBackend.
type Backend interface {
	// Name returns the backend identifier used in logging and /health responses.
	// Examples: "pngpaste", "osascript", "xclip", "wl-paste", "none"
	Name() string

	// Read reads the current clipboard image and returns raw bytes.
	// The returned bytes may be any supported image format (PNG, JPEG, GIF, WebP).
	// maxBytes caps the read via io.LimitReader to prevent OOM from huge clipboard objects.
	// Pass 10<<20 + 1 (10MB + 1 byte) as the standard limit.
	//
	// Returns ErrNoImage if the clipboard contains no image.
	// Returns ErrBackendUnavailable if the clipboard tool cannot be executed.
	// Returns ErrImageTooLarge if the clipboard data exceeds maxBytes.
	// Returns ErrTimeout if the context deadline is exceeded.
	Read(ctx context.Context, maxBytes int64) ([]byte, error)

	// Available reports whether the backend tool is present and executable.
	// Called once at startup. If false the backend returns ErrBackendUnavailable on Read.
	Available() bool
}

// Sentinel errors returned by Backend.Read.
var (
	ErrNoImage            = errors.New("no image in clipboard")
	ErrBackendUnavailable = errors.New("clipboard backend unavailable")
	ErrTimeout            = errors.New("clipboard read timed out")
	ErrImageTooLarge      = errors.New("image too large")
)

// noneBackend is the null implementation returned when no suitable clipboard
// tool is detected on the current platform.
type noneBackend struct {
	reason string
}

func (n *noneBackend) Name() string { return "none" }

func (n *noneBackend) Available() bool { return false }

func (n *noneBackend) Read(_ context.Context, _ int64) ([]byte, error) {
	return nil, ErrBackendUnavailable
}
