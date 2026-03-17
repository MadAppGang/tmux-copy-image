//go:build linux

package clipboard

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// waylandBackend reads the clipboard on Linux Wayland sessions using wl-paste.
type waylandBackend struct {
	wlPastePath string
}

func (b *waylandBackend) Name() string { return "wl-paste" }

func (b *waylandBackend) Available() bool {
	return os.Getenv("WAYLAND_DISPLAY") != "" && b.wlPastePath != ""
}

func (b *waylandBackend) Read(ctx context.Context, maxBytes int64) ([]byte, error) {
	if !b.Available() {
		return nil, ErrBackendUnavailable
	}

	// Step 1: List available MIME types.
	listCmd := exec.CommandContext(ctx, b.wlPastePath, "--list-types")
	listOut, err := listCmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}
		// wl-paste may exit non-zero when the clipboard is empty.
		return nil, ErrNoImage
	}

	// Also check stderr for "Nothing is copied" indicator.
	listStr := string(listOut)
	if strings.Contains(listStr, "Nothing is copied") {
		return nil, ErrNoImage
	}

	types := strings.Split(strings.TrimSpace(listStr), "\n")
	mimeType, ok := selectImageType(types)
	if !ok {
		return nil, ErrNoImage
	}

	// Step 2: Read the clipboard data in the selected MIME type.
	readCmd := exec.CommandContext(ctx, b.wlPastePath, "--type", mimeType)
	stdout, err := readCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("wl-paste pipe: %w", err)
	}

	if err := readCmd.Start(); err != nil {
		return nil, fmt.Errorf("wl-paste start: %w", err)
	}

	limited := io.LimitReader(stdout, maxBytes)
	data, readErr := io.ReadAll(limited)
	waitErr := readCmd.Wait()

	if waitErr != nil {
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}
		return nil, ErrNoImage
	}

	if readErr != nil {
		return nil, fmt.Errorf("wl-paste read: %w", readErr)
	}

	if int64(len(data)) >= maxBytes {
		return nil, ErrImageTooLarge
	}

	return data, nil
}
