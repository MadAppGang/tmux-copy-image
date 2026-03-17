package daemon

import (
	"net/http"

	"golang.org/x/time/rate"
)

// RateLimitMiddleware applies a global token-bucket rate limiter. When the
// limit is exceeded it returns 429 Too Many Requests with a Retry-After: 1
// header.
//
// Configuration per architecture spec: 10 req/s sustained, burst of 20.
func RateLimitMiddleware(limiter *rate.Limiter, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !limiter.Allow() {
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusTooManyRequests, "rate limit exceeded", "RATE_LIMITED")
			return
		}
		next.ServeHTTP(w, r)
	})
}
