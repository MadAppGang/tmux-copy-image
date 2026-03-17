//go:build linux

package clipboard

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// x11Backend reads the clipboard on Linux X11 using xclip.
type x11Backend struct {
	xclipPath string
}

func (b *x11Backend) Name() string { return "xclip" }

func (b *x11Backend) Available() bool {
	return os.Getenv("DISPLAY") != "" && b.xclipPath != ""
}

func (b *x11Backend) Read(ctx context.Context, maxBytes int64) ([]byte, error) {
	if !b.Available() {
		return nil, ErrBackendUnavailable
	}

	// Step 1: List available MIME types.
	targetsCmd := exec.CommandContext(ctx, b.xclipPath,
		"-selection", "clipboard", "-t", "TARGETS", "-o")
	var targetsOut bytes.Buffer
	targetsCmd.Stdout = &targetsOut
	if err := targetsCmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}
		return nil, ErrBackendUnavailable
	}

	targets := strings.Split(strings.TrimSpace(targetsOut.String()), "\n")
	mimeType, ok := selectImageType(targets)
	if !ok {
		return nil, ErrNoImage
	}

	// Step 2: Read the clipboard data in the selected format.
	readCmd := exec.CommandContext(ctx, b.xclipPath,
		"-selection", "clipboard", "-t", mimeType, "-o")
	stdout, err := readCmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("xclip pipe: %w", err)
	}

	if err := readCmd.Start(); err != nil {
		return nil, fmt.Errorf("xclip start: %w", err)
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
		return nil, fmt.Errorf("xclip read: %w", readErr)
	}

	if int64(len(data)) >= maxBytes {
		return nil, ErrImageTooLarge
	}

	return data, nil
}
