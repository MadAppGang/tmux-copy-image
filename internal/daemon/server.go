package daemon

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"golang.org/x/time/rate"

	"github.com/jacksteamdev/tmux-image-clipboard/internal/clipboard"
)

const (
	// maxConcurrentClipboard is the maximum number of simultaneous clipboard reads.
	maxConcurrentClipboard = 4

	// cacheTTL is how long clipboard data is cached to serve /image/meta + /image
	// from a single clipboard read. Rev 3: reduced from 5s to 2s (SYNTH-8).
	cacheTTL = 2 * time.Second

	// rateLimitPerSecond is the sustained request rate (requests per second).
	rateLimitPerSecond = 10

	// rateLimitBurst is the token-bucket burst capacity.
	rateLimitBurst = 20
)

// Config holds the configuration for the HTTP daemon.
type Config struct {
	Port       int
	Token      string
	LogFormat  string // "text" or "json"
	LogLevel   string // "debug", "info", "warn", "error"
	PIDFile    string
	Version    string
	Backend    clipboard.Backend
}

// Server is the clip-serve HTTP daemon.
type Server struct {
	httpServer *http.Server
	backend    clipboard.Backend
	token      string
	version    string
	port       int
	startTime  time.Time
	logger     *slog.Logger

	// Clipboard cache: reduces double-reads for /image/meta + /image in the
	// same paste operation, and serialises concurrent clipboard calls.
	cacheMu        sync.Mutex
	cacheData      []byte
	cacheFetchedAt time.Time

	// sem is a buffered channel used as a counting semaphore to limit
	// simultaneous clipboard subprocess invocations.
	sem chan struct{}

	// pidFile is the path to the PID file; empty if not configured.
	pidFile string

	// pidFd is the file descriptor of the open PID file, held open to maintain
	// the flock() advisory lock for the duration of the process lifetime.
	pidFd *os.File
}

// New constructs a Server from the given Config.
func New(cfg Config) *Server {
	return &Server{
		backend:   cfg.Backend,
		token:     cfg.Token,
		version:   cfg.Version,
		port:      cfg.Port,
		startTime: time.Now(),
		sem:       make(chan struct{}, maxConcurrentClipboard),
		pidFile:   cfg.PIDFile,
		logger:    buildLogger(cfg.LogFormat, cfg.LogLevel),
	}
}

// Start begins listening on 127.0.0.1:<port> and serves requests until a
// SIGTERM or SIGINT is received, then performs a graceful shutdown.
func (s *Server) Start() error {
	if err := s.writePIDFile(); err != nil {
		return fmt.Errorf("pid file: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/image/meta", s.handleMeta)
	mux.HandleFunc("/image", s.handleImage)

	// Middleware stack (outermost to innermost): rate limiter → logger → auth → handler.
	limiter := rate.NewLimiter(rate.Every(time.Second/rateLimitPerSecond), rateLimitBurst)
	var handler http.Handler = mux
	handler = AuthMiddleware(s.token, handler)
	handler = LogMiddleware(s.logger, handler)
	handler = RateLimitMiddleware(limiter, handler)

	s.httpServer = &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", s.port),
		Handler: handler,
		// HTTP/2 disabled: empty map prevents the server from advertising h2
		// upgrade, ensuring HTTP/1.1 only over the SSH tunnel.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}

	listener, err := net.Listen("tcp", s.httpServer.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", s.httpServer.Addr, err)
	}

	s.logger.Info("clip-serve starting",
		"version", s.version,
		"port", s.port,
		"backend", s.backend.Name(),
		"token_auth", s.token != "",
	)
	s.logger.Info("listening", "addr", s.httpServer.Addr)

	// Handle signals for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Serve in background.
	serveErr := make(chan error, 1)
	go func() {
		if err := s.httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	// Wait for shutdown signal or serve error.
	select {
	case <-ctx.Done():
		s.logger.Info("shutdown signal received, stopping")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("graceful shutdown failed", "error", err)
		}
		return <-serveErr
	case err := <-serveErr:
		return err
	}
}

// writePIDFile creates (or reuses) the PID file and acquires an exclusive
// advisory flock() lock. If another instance already holds the lock, Start
// returns an error with the conflicting PID.
func (s *Server) writePIDFile() error {
	if s.pidFile == "" {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(s.pidFile), 0700); err != nil {
		return fmt.Errorf("create pid dir: %w", err)
	}

	f, err := os.OpenFile(s.pidFile, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return fmt.Errorf("open pid file: %w", err)
	}

	// Try to acquire an exclusive non-blocking flock.
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		// Another instance holds the lock.
		buf := make([]byte, 32)
		n, _ := f.Read(buf)
		f.Close()
		if n > 0 {
			return fmt.Errorf("clip-serve already running (PID %s)", string(buf[:n]))
		}
		return fmt.Errorf("clip-serve already running (could not read PID)")
	}

	// We hold the lock. Write our PID.
	if err := f.Truncate(0); err != nil {
		return fmt.Errorf("truncate pid file: %w", err)
	}
	if _, err := fmt.Fprintf(f, "%d", os.Getpid()); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	// Keep the file open — the lock is held until f is closed (process exit).
	s.pidFd = f
	return nil
}

// readClipboard returns cached clipboard data if still fresh, otherwise reads
// fresh data from the backend. The cache mutex also serialises concurrent reads.
func (s *Server) readClipboard(ctx context.Context) ([]byte, error) {
	s.cacheMu.Lock()
	defer s.cacheMu.Unlock()

	if s.cacheData != nil && time.Since(s.cacheFetchedAt) < cacheTTL {
		return s.cacheData, nil
	}

	data, err := s.backend.Read(ctx, maxClipboardBytes)
	if err != nil {
		return nil, err
	}

	s.cacheData = data
	s.cacheFetchedAt = time.Now()
	return data, nil
}

// buildLogger constructs a slog.Logger with the requested format and level.
func buildLogger(format, level string) *slog.Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stderr, opts)
	} else {
		handler = slog.NewTextHandler(os.Stderr, opts)
	}

	return slog.New(handler)
}
