package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jacksteamdev/tmux-image-clipboard/internal/clipboard"
	"github.com/jacksteamdev/tmux-image-clipboard/internal/imageutil"
)

const (
	// protocolVersion is the value of X-Clip-Serve-Protocol header.
	protocolVersion = "1"

	// clipboardTimeout is the maximum time allowed for a clipboard read.
	clipboardTimeout = 5 * time.Second

	// maxClipboardBytes is the maximum image size the daemon will read (10 MiB + 1).
	maxClipboardBytes = 10<<20 + 1
)

// Error code constants used in ErrorResponse.Code.
const (
	CodeNoImage            = "NO_IMAGE"
	CodeTooLarge           = "TOO_LARGE"
	CodeBackendUnavailable = "BACKEND_UNAVAILABLE"
	CodeTimeout            = "TIMEOUT"
	CodeUnauthorized       = "UNAUTHORIZED"
	CodeRateLimited        = "RATE_LIMITED"
	CodeMethodNotAllowed   = "METHOD_NOT_ALLOWED"
)

// HealthResponse is the JSON body returned by GET /health.
type HealthResponse struct {
	Status        string `json:"status"`
	Version       string `json:"version"`
	Backend       string `json:"backend"`
	UptimeSeconds int64  `json:"uptime_seconds"`
	Port          int    `json:"port"`
	Protocol      int    `json:"protocol"`
}

// MetaResponse is the JSON body returned by GET /image/meta.
type MetaResponse struct {
	Available bool    `json:"available"`
	Width     *int    `json:"width,omitempty"`
	Height    *int    `json:"height,omitempty"`
	SizeBytes *int    `json:"size_bytes,omitempty"`
	Format    *string `json:"format,omitempty"`
}

// ErrorResponse is the JSON body for all error responses.
type ErrorResponse struct {
	Error     string `json:"error"`
	Code      string `json:"code"`
	SizeBytes *int64 `json:"size_bytes,omitempty"`
	MaxBytes  *int64 `json:"max_bytes,omitempty"`
}

// setProtocolHeader adds the X-Clip-Serve-Protocol header to all responses.
func setProtocolHeader(w http.ResponseWriter) {
	w.Header().Set("X-Clip-Serve-Protocol", protocolVersion)
}

// handleHealth handles GET /health. It is never authenticated.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		setProtocolHeader(w)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", CodeMethodNotAllowed)
		return
	}

	setProtocolHeader(w)
	uptime := int64(time.Since(s.startTime).Seconds())
	writeJSON(w, http.StatusOK, HealthResponse{
		Status:        "ok",
		Version:       s.version,
		Backend:       s.backend.Name(),
		UptimeSeconds: uptime,
		Port:          s.port,
		Protocol:      1,
	})
}

// handleImage handles GET /image. It reads the clipboard and returns raw image bytes.
func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		setProtocolHeader(w)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", CodeMethodNotAllowed)
		return
	}

	setProtocolHeader(w)

	// Acquire semaphore slot or return 503.
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "too many concurrent requests", CodeBackendUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), clipboardTimeout)
	defer cancel()

	data, err := s.readClipboard(ctx)
	if err != nil {
		handleClipboardError(w, err)
		return
	}

	info, err := imageutil.DetectFormat(data)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "could not detect image format", "UNKNOWN_FORMAT")
		return
	}

	// Attempt dimension extraction; failures are non-fatal.
	w32, h32, _ := imageutil.ExtractDimensions(data, info.MIMEType)
	if w32 > 0 {
		w.Header().Set("X-Image-Width", fmt.Sprintf("%d", w32))
	}
	if h32 > 0 {
		w.Header().Set("X-Image-Height", fmt.Sprintf("%d", h32))
	}

	w.Header().Set("X-Image-Format", info.Format)
	w.Header().Set("Content-Type", info.MIMEType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	w.WriteHeader(http.StatusOK)
	w.Write(data) //nolint:errcheck
}

// handleMeta handles GET /image/meta. It returns clipboard metadata without
// downloading the full image to the client.
func (s *Server) handleMeta(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		setProtocolHeader(w)
		writeError(w, http.StatusMethodNotAllowed, "method not allowed", CodeMethodNotAllowed)
		return
	}

	setProtocolHeader(w)

	// Acquire semaphore slot or return 503.
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		writeError(w, http.StatusServiceUnavailable, "too many concurrent requests", CodeBackendUnavailable)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), clipboardTimeout)
	defer cancel()

	data, err := s.readClipboard(ctx)
	if err != nil {
		if errors.Is(err, clipboard.ErrNoImage) {
			// Not an error for /image/meta — return available: false.
			writeMetaResponse(w, r, MetaResponse{Available: false})
			return
		}
		handleClipboardError(w, err)
		return
	}

	info, err := imageutil.DetectFormat(data)
	if err != nil {
		// Return available: false on format detection failure.
		writeMetaResponse(w, r, MetaResponse{Available: false})
		return
	}

	meta := MetaResponse{Available: true}
	format := info.Format
	meta.Format = &format

	size := info.SizeBytes
	meta.SizeBytes = &size

	// Dimension extraction is best-effort.
	w32, h32, _ := imageutil.ExtractDimensions(data, info.MIMEType)
	if w32 > 0 {
		meta.Width = &w32
	}
	if h32 > 0 {
		meta.Height = &h32
	}

	w.Header().Set("X-Image-Format", info.Format)
	writeMetaResponse(w, r, meta)
}

// writeMetaResponse writes the meta response in either JSON (default) or
// shell KEY=VALUE format depending on the ?format= query parameter.
func writeMetaResponse(w http.ResponseWriter, r *http.Request, meta MetaResponse) {
	if r.URL.Query().Get("format") == "shell" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		if meta.Available {
			fmt.Fprintf(w, "AVAILABLE=true\n")
			if meta.Format != nil {
				fmt.Fprintf(w, "FORMAT=%s\n", *meta.Format)
			}
			if meta.Width != nil {
				fmt.Fprintf(w, "WIDTH=%d\n", *meta.Width)
			}
			if meta.Height != nil {
				fmt.Fprintf(w, "HEIGHT=%d\n", *meta.Height)
			}
			if meta.SizeBytes != nil {
				fmt.Fprintf(w, "SIZE_BYTES=%d\n", *meta.SizeBytes)
			}
		} else {
			fmt.Fprintf(w, "AVAILABLE=false\n")
		}
		return
	}

	writeJSON(w, http.StatusOK, meta)
}

// handleClipboardError maps clipboard errors to HTTP error responses.
func handleClipboardError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, clipboard.ErrNoImage):
		writeError(w, http.StatusNotFound, "no image in clipboard", CodeNoImage)
	case errors.Is(err, clipboard.ErrImageTooLarge):
		maxBytes := int64(10 << 20)
		errResp := ErrorResponse{
			Error:    "image too large",
			Code:     CodeTooLarge,
			MaxBytes: &maxBytes,
		}
		setJSON(w, http.StatusRequestEntityTooLarge)
		json.NewEncoder(w).Encode(errResp) //nolint:errcheck
	case errors.Is(err, clipboard.ErrTimeout):
		writeError(w, http.StatusGatewayTimeout, "clipboard read timed out", CodeTimeout)
	case errors.Is(err, clipboard.ErrBackendUnavailable):
		writeError(w, http.StatusServiceUnavailable, "clipboard backend unavailable", CodeBackendUnavailable)
	default:
		writeError(w, http.StatusInternalServerError, "internal error", "INTERNAL")
	}
}

// writeJSON encodes v as JSON with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	setJSON(w, status)
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

// setJSON sets Content-Type: application/json and the given status code.
func setJSON(w http.ResponseWriter, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
}

// writeError writes an ErrorResponse JSON body.
func writeError(w http.ResponseWriter, status int, msg, code string) {
	writeJSON(w, status, ErrorResponse{Error: msg, Code: code})
}
