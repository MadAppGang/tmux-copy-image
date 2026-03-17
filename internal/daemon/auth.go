package daemon

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

// ErrTokenTooShort is returned when the configured token is shorter than 16 characters.
var ErrTokenTooShort = errors.New("token is too short (minimum 16 characters)")

// ValidateTokenLength returns an error if the token is configured but shorter
// than the minimum recommended length.
func ValidateTokenLength(token string) error {
	if token != "" && len(token) < 16 {
		return ErrTokenTooShort
	}
	return nil
}

// AuthMiddleware enforces bearer token authentication when a token is configured.
// When token is empty, the middleware is a no-op and all requests pass through.
// The /health path is always exempt from authentication.
func AuthMiddleware(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}

	expectedBytes := []byte(token)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /health is always exempt from authentication.
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		authHeader := r.Header.Get("Authorization")
		candidate := strings.TrimPrefix(authHeader, "Bearer ")

		if candidate == authHeader || // no "Bearer " prefix found
			subtle.ConstantTimeCompare([]byte(candidate), expectedBytes) != 1 {
			writeError(w, http.StatusUnauthorized, "unauthorized", "UNAUTHORIZED")
			return
		}

		next.ServeHTTP(w, r)
	})
}
