//go:build !darwin

package clipboard

import "context"

// macOSBackend stub for non-Darwin platforms.
// The real implementation is in macos.go (darwin build tag).
type macOSBackend struct {
	pngpastePath  string
	osascriptPath string
}

func (b *macOSBackend) Name() string                                    { return "none" }
func (b *macOSBackend) Available() bool                                 { return false }
func (b *macOSBackend) Read(_ context.Context, _ int64) ([]byte, error) { return nil, ErrBackendUnavailable }
