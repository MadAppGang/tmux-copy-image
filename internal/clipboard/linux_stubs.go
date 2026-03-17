//go:build !linux

package clipboard

import "context"

// x11Backend stub for non-Linux platforms.
// The real implementation is in linux_x11.go (linux build tag).
type x11Backend struct {
	xclipPath string
}

func (b *x11Backend) Name() string                                    { return "none" }
func (b *x11Backend) Available() bool                                 { return false }
func (b *x11Backend) Read(_ context.Context, _ int64) ([]byte, error) { return nil, ErrBackendUnavailable }

// waylandBackend stub for non-Linux platforms.
// The real implementation is in linux_wayland.go (linux build tag).
type waylandBackend struct {
	wlPastePath string
}

func (b *waylandBackend) Name() string                                    { return "none" }
func (b *waylandBackend) Available() bool                                 { return false }
func (b *waylandBackend) Read(_ context.Context, _ int64) ([]byte, error) { return nil, ErrBackendUnavailable }
