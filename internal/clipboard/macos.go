//go:build darwin

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

// macOSBackend reads the clipboard on macOS using pngpaste (fast PNG path) or
// osascript (multi-format fallback).
type macOSBackend struct {
	pngpastePath  string // empty if pngpaste is not installed
	osascriptPath string // empty if osascript is not installed (unusual on macOS)
}

func (b *macOSBackend) Name() string {
	if b.pngpastePath != "" {
		return "pngpaste"
	}
	return "osascript"
}

func (b *macOSBackend) Available() bool {
	return b.pngpastePath != "" || b.osascriptPath != ""
}

func (b *macOSBackend) Read(ctx context.Context, maxBytes int64) ([]byte, error) {
	if b.pngpastePath != "" {
		data, err := b.readViaPngpaste(ctx, maxBytes)
		if err == nil {
			return data, nil
		}
		// pngpaste failed (no image or wrong format); fall through to osascript.
		if err != ErrNoImage {
			return nil, err
		}
	}

	if b.osascriptPath != "" {
		return b.readViaOsascript(ctx, maxBytes)
	}

	return nil, ErrBackendUnavailable
}

// readViaPngpaste runs `pngpaste -` and returns PNG bytes.
func (b *macOSBackend) readViaPngpaste(ctx context.Context, maxBytes int64) ([]byte, error) {
	cmd := exec.CommandContext(ctx, b.pngpastePath, "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("pngpaste pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("pngpaste start: %w", err)
	}

	limited := io.LimitReader(stdout, maxBytes)
	data, readErr := io.ReadAll(limited)
	waitErr := cmd.Wait()

	if waitErr != nil {
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}
		return nil, ErrNoImage
	}

	if readErr != nil {
		return nil, fmt.Errorf("pngpaste read: %w", readErr)
	}

	// Enforce the size limit: if we read exactly maxBytes, there may be more.
	if int64(len(data)) >= maxBytes {
		return nil, ErrImageTooLarge
	}

	return data, nil
}

// osascriptPNG is the AppleScript that tries to extract a PNG from the clipboard.
// It writes the data to a temp file and returns "png:<path>" or "error:no image".
const osascriptPNG = `
set tempFile to (POSIX path of (path to temporary items)) & "clip-serve-" & (do shell script "uuidgen") & ".tmp"
try
	set theData to the clipboard as «class PNGf»
	set fileRef to open for access POSIX file tempFile with write permission
	set eof of fileRef to 0
	write theData to fileRef
	close access fileRef
	return "png:" & tempFile
on error
	return "error:no image"
end try
`

// readViaOsascript extracts PNG clipboard data via AppleScript.
// Phase 1: PNG only. Multi-format expansion deferred to Phase 2.
func (b *macOSBackend) readViaOsascript(ctx context.Context, maxBytes int64) ([]byte, error) {
	cmd := exec.CommandContext(ctx, b.osascriptPath, "-e", osascriptPNG)
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ErrTimeout
		}
		return nil, ErrNoImage
	}

	result := strings.TrimSpace(string(out))
	if strings.HasPrefix(result, "error:") {
		return nil, ErrNoImage
	}

	if !strings.HasPrefix(result, "png:") {
		return nil, ErrNoImage
	}

	tempFile := strings.TrimPrefix(result, "png:")
	defer os.Remove(tempFile)

	f, err := os.Open(tempFile)
	if err != nil {
		return nil, fmt.Errorf("osascript temp file: %w", err)
	}
	defer f.Close()

	limited := io.LimitReader(f, maxBytes)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, fmt.Errorf("osascript read: %w", err)
	}

	if int64(len(data)) >= maxBytes {
		return nil, ErrImageTooLarge
	}

	return data, nil
}
