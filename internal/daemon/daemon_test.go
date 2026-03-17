package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"golang.org/x/time/rate"

	"github.com/jacksteamdev/tmux-image-clipboard/internal/clipboard"
)

// ---------------------------------------------------------------------------
// Mock Backend
// ---------------------------------------------------------------------------

type mockBackend struct {
	data  []byte
	err   error
	name  string
	avail bool
	delay time.Duration
}

func (m *mockBackend) Name() string    { return m.name }
func (m *mockBackend) Available() bool { return m.avail }

func (m *mockBackend) Read(ctx context.Context, maxBytes int64) ([]byte, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, clipboard.ErrTimeout
		}
	}
	if m.err != nil {
		return nil, m.err
	}
	if int64(len(m.data)) >= maxBytes {
		return nil, clipboard.ErrImageTooLarge
	}
	return m.data, nil
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func makePNGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func makeJPEGBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{0, 255, 0, 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode test jpeg: %v", err)
	}
	return buf.Bytes()
}

func newTestServer(t *testing.T, backend clipboard.Backend, token string) *Server {
	t.Helper()
	return &Server{
		backend:   backend,
		token:     token,
		version:   "test",
		port:      18339,
		startTime: time.Now(),
		sem:       make(chan struct{}, maxConcurrentClipboard),
		logger:    buildLogger("text", "error"), // suppress test output
	}
}

func buildHandler(t *testing.T, s *Server) http.Handler {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/image/meta", s.handleMeta)
	mux.HandleFunc("/image", s.handleImage)

	limiter := rate.NewLimiter(rate.Every(time.Second/rateLimitPerSecond), rateLimitBurst)
	var h http.Handler = mux
	h = AuthMiddleware(s.token, h)
	h = LogMiddleware(s.logger, h)
	h = RateLimitMiddleware(limiter, h)
	return h
}

func doRequest(t *testing.T, h http.Handler, method, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

// ---------------------------------------------------------------------------
// /health tests
// ---------------------------------------------------------------------------

func TestHealthHandler_OK(t *testing.T) {
	s := newTestServer(t, &mockBackend{name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/health", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var resp HealthResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if resp.Status != "ok" {
		t.Errorf("status = %q, want ok", resp.Status)
	}
	if resp.Backend != "test" {
		t.Errorf("backend = %q, want test", resp.Backend)
	}
	if resp.Protocol != 1 {
		t.Errorf("protocol = %d, want 1", resp.Protocol)
	}
}

func TestHealthHandler_WithToken_NoAuth(t *testing.T) {
	// /health should be accessible without auth even when token is configured.
	s := newTestServer(t, &mockBackend{name: "test", avail: true}, "supersecrettoken12345")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/health", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("/health without token when configured: status = %d, want 200", rr.Code)
	}
}

func TestHealthHandler_MethodNotAllowed(t *testing.T) {
	s := newTestServer(t, &mockBackend{name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodPost, "/health", "")

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Protocol header tests
// ---------------------------------------------------------------------------

func TestProtocolHeader_Health(t *testing.T) {
	s := newTestServer(t, &mockBackend{name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/health", "")

	if v := rr.Header().Get("X-Clip-Serve-Protocol"); v != "1" {
		t.Errorf("X-Clip-Serve-Protocol = %q, want 1", v)
	}
}

func TestProtocolHeader_Image(t *testing.T) {
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "")

	if v := rr.Header().Get("X-Clip-Serve-Protocol"); v != "1" {
		t.Errorf("X-Clip-Serve-Protocol = %q, want 1", v)
	}
}

func TestProtocolHeader_Meta(t *testing.T) {
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image/meta", "")

	if v := rr.Header().Get("X-Clip-Serve-Protocol"); v != "1" {
		t.Errorf("X-Clip-Serve-Protocol = %q, want 1", v)
	}
}

// ---------------------------------------------------------------------------
// /image tests
// ---------------------------------------------------------------------------

func TestImageHandler_PNG(t *testing.T) {
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if !bytes.Equal(rr.Body.Bytes(), pngData) {
		t.Error("response body does not match PNG data")
	}
}

func TestImageHandler_NoImage(t *testing.T) {
	s := newTestServer(t, &mockBackend{err: clipboard.ErrNoImage, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "")

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
	var errResp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Code != CodeNoImage {
		t.Errorf("code = %q, want %q", errResp.Code, CodeNoImage)
	}
}

func TestImageHandler_TooLarge(t *testing.T) {
	// mockBackend returns ErrImageTooLarge when data >= maxBytes.
	// We achieve this by setting data to exactly maxClipboardBytes length.
	largeData := make([]byte, maxClipboardBytes)
	// Fill with PNG magic so format detection doesn't fail.
	copy(largeData, []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A})

	s := newTestServer(t, &mockBackend{data: largeData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "")

	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rr.Code)
	}
	var errResp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Code != CodeTooLarge {
		t.Errorf("code = %q, want %q", errResp.Code, CodeTooLarge)
	}
}

func TestImageHandler_BackendUnavailable(t *testing.T) {
	s := newTestServer(t, &mockBackend{err: clipboard.ErrBackendUnavailable, name: "none", avail: false}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "")

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rr.Code)
	}
	var errResp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Code != CodeBackendUnavailable {
		t.Errorf("code = %q, want %q", errResp.Code, CodeBackendUnavailable)
	}
}

func TestImageHandler_Timeout(t *testing.T) {
	s := newTestServer(t, &mockBackend{err: clipboard.ErrTimeout, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "")

	if rr.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504", rr.Code)
	}
	var errResp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Code != CodeTimeout {
		t.Errorf("code = %q, want %q", errResp.Code, CodeTimeout)
	}
}

// ---------------------------------------------------------------------------
// /image/meta tests
// ---------------------------------------------------------------------------

func TestMetaHandler_Available_JSON(t *testing.T) {
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image/meta", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var meta MetaResponse
	if err := json.NewDecoder(rr.Body).Decode(&meta); err != nil {
		t.Fatalf("decode meta response: %v", err)
	}
	if !meta.Available {
		t.Error("available = false, want true")
	}
	if meta.Format == nil || *meta.Format != "png" {
		t.Errorf("format = %v, want png", meta.Format)
	}
}

func TestMetaHandler_NotAvailable(t *testing.T) {
	s := newTestServer(t, &mockBackend{err: clipboard.ErrNoImage, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image/meta", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var meta MetaResponse
	if err := json.NewDecoder(rr.Body).Decode(&meta); err != nil {
		t.Fatalf("decode meta response: %v", err)
	}
	if meta.Available {
		t.Error("available = true, want false")
	}
}

func TestMetaHandler_ShellFormat(t *testing.T) {
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image/meta?format=shell", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "AVAILABLE=true") {
		t.Errorf("shell response missing AVAILABLE=true; got:\n%s", body)
	}
	if !strings.Contains(body, "FORMAT=png") {
		t.Errorf("shell response missing FORMAT=png; got:\n%s", body)
	}
}

func TestMetaHandler_ShellFormat_NotAvailable(t *testing.T) {
	s := newTestServer(t, &mockBackend{err: clipboard.ErrNoImage, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image/meta?format=shell", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "AVAILABLE=false") {
		t.Errorf("shell response missing AVAILABLE=false; got:\n%s", body)
	}
}

// ---------------------------------------------------------------------------
// Auth middleware tests
// ---------------------------------------------------------------------------

func TestAuthMiddleware_Bypass(t *testing.T) {
	// No token configured — all requests should pass without Authorization.
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "") // no token

	if rr.Code != http.StatusOK {
		t.Fatalf("no-token bypass: status = %d, want 200", rr.Code)
	}
}

func TestAuthMiddleware_Required(t *testing.T) {
	// Token configured — missing header on /image should return 401.
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, "supersecrettoken12345")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "") // no token

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("missing token: status = %d, want 401", rr.Code)
	}
	var errResp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if errResp.Code != CodeUnauthorized {
		t.Errorf("code = %q, want %q", errResp.Code, CodeUnauthorized)
	}
}

func TestAuthMiddleware_ValidToken(t *testing.T) {
	const tok = "supersecrettoken12345"
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, tok)
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", tok)

	if rr.Code != http.StatusOK {
		t.Fatalf("valid token: status = %d, want 200", rr.Code)
	}
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	const tok = "supersecrettoken12345"
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, tok)
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "wrongtoken")

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("invalid token: status = %d, want 401", rr.Code)
	}
}

func TestAuthMiddleware_HealthExempt(t *testing.T) {
	// /health must be accessible without auth even when token is configured.
	s := newTestServer(t, &mockBackend{name: "test", avail: true}, "supersecrettoken12345")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/health", "") // no token

	if rr.Code != http.StatusOK {
		t.Fatalf("/health exempt: status = %d, want 200", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Rate limiter tests
// ---------------------------------------------------------------------------

func TestRateLimitMiddleware_BurstAllowed(t *testing.T) {
	// The rate limiter has a burst of rateLimitBurst=20. All 20 requests should succeed.
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, "")

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/image/meta", s.handleMeta)
	mux.HandleFunc("/image", s.handleImage)

	// Use a fresh limiter for this test to avoid interference.
	limiter := rate.NewLimiter(rate.Every(time.Second/rateLimitPerSecond), rateLimitBurst)
	h := RateLimitMiddleware(limiter, mux)

	for i := 0; i < rateLimitBurst; i++ {
		req := httptest.NewRequest(http.MethodGet, "/health", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code == http.StatusTooManyRequests {
			t.Fatalf("request %d/%d: got 429, want 200 (burst=%d)", i+1, rateLimitBurst, rateLimitBurst)
		}
	}
}

func TestRateLimitMiddleware_ExceedRate(t *testing.T) {
	// Send more requests than the burst capacity — the (burst+1)th must be 429.
	s := newTestServer(t, &mockBackend{name: "test", avail: true}, "")

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)

	// Very restrictive limiter: burst 1.
	limiter := rate.NewLimiter(rate.Every(time.Hour), 1)
	h := RateLimitMiddleware(limiter, mux)

	// First request should succeed.
	req1 := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr1 := httptest.NewRecorder()
	h.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Fatalf("first request: status = %d, want 200", rr1.Code)
	}

	// Second request must be rate limited.
	req2 := httptest.NewRequest(http.MethodGet, "/health", nil)
	rr2 := httptest.NewRecorder()
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusTooManyRequests {
		t.Fatalf("second request: status = %d, want 429", rr2.Code)
	}

	// Verify Retry-After header is set.
	if v := rr2.Header().Get("Retry-After"); v != "1" {
		t.Errorf("Retry-After = %q, want 1", v)
	}

	var errResp ErrorResponse
	if err := json.NewDecoder(rr2.Body).Decode(&errResp); err != nil {
		t.Fatalf("decode 429 response: %v", err)
	}
	if errResp.Code != CodeRateLimited {
		t.Errorf("code = %q, want %q", errResp.Code, CodeRateLimited)
	}
}

// ---------------------------------------------------------------------------
// Method guard tests
// ---------------------------------------------------------------------------

func TestMethodNotAllowed_Image(t *testing.T) {
	s := newTestServer(t, &mockBackend{name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodPost, "/image", "")

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /image: status = %d, want 405", rr.Code)
	}
}

func TestMethodNotAllowed_Meta(t *testing.T) {
	s := newTestServer(t, &mockBackend{name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodPut, "/image/meta", "")

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("PUT /image/meta: status = %d, want 405", rr.Code)
	}
}

// ---------------------------------------------------------------------------
// Content-Type and X-Image-Format header tests (Phase 2)
// ---------------------------------------------------------------------------

func TestImageHandler_ContentType_PNG(t *testing.T) {
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if xf := rr.Header().Get("X-Image-Format"); xf != "png" {
		t.Errorf("X-Image-Format = %q, want png", xf)
	}
}

func TestImageHandler_ContentType_JPEG(t *testing.T) {
	jpegData := makeJPEGBytes(t)
	s := newTestServer(t, &mockBackend{data: jpegData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	if xf := rr.Header().Get("X-Image-Format"); xf != "jpeg" {
		t.Errorf("X-Image-Format = %q, want jpeg", xf)
	}
}

func TestImageHandler_XImageFormat_PNG(t *testing.T) {
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image", "")

	if xf := rr.Header().Get("X-Image-Format"); xf != "png" {
		t.Errorf("X-Image-Format = %q, want png", xf)
	}
	if !bytes.Equal(rr.Body.Bytes(), pngData) {
		t.Error("response body does not match PNG data")
	}
}

func TestMetaHandler_XImageFormat_PNG(t *testing.T) {
	pngData := makePNGBytes(t)
	s := newTestServer(t, &mockBackend{data: pngData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image/meta", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if xf := rr.Header().Get("X-Image-Format"); xf != "png" {
		t.Errorf("X-Image-Format = %q, want png", xf)
	}
}

func TestMetaHandler_XImageFormat_JPEG(t *testing.T) {
	jpegData := makeJPEGBytes(t)
	s := newTestServer(t, &mockBackend{data: jpegData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image/meta", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if xf := rr.Header().Get("X-Image-Format"); xf != "jpeg" {
		t.Errorf("X-Image-Format = %q, want jpeg", xf)
	}

	var meta MetaResponse
	if err := json.NewDecoder(rr.Body).Decode(&meta); err != nil {
		t.Fatalf("decode meta response: %v", err)
	}
	if !meta.Available {
		t.Error("available = false, want true")
	}
	if meta.Format == nil || *meta.Format != "jpeg" {
		t.Errorf("format = %v, want jpeg", meta.Format)
	}
}

func TestMetaHandler_ShellFormat_JPEG(t *testing.T) {
	jpegData := makeJPEGBytes(t)
	s := newTestServer(t, &mockBackend{data: jpegData, name: "test", avail: true}, "")
	h := buildHandler(t, s)
	rr := doRequest(t, h, http.MethodGet, "/image/meta?format=shell", "")

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "AVAILABLE=true") {
		t.Errorf("shell response missing AVAILABLE=true; got:\n%s", body)
	}
	if !strings.Contains(body, "FORMAT=jpeg") {
		t.Errorf("shell response missing FORMAT=jpeg; got:\n%s", body)
	}
}
