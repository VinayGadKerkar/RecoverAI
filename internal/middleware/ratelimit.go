package middleware

import (
	"fmt"
	"net/http"
	"time"

	redisclient "recoverai/internal/redis"
)

const (
	rateLimitRequests = 100
	rateLimitWindow   = time.Minute
)

// RateLimit applies a sliding-window rate limit per IP using Redis.
// Limit: 100 requests per minute per IP.
func RateLimit(r *redisclient.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			ip := req.RemoteAddr
			key := fmt.Sprintf("ratelimit:%s", ip)

			count, err := r.Incr(req.Context(), key)
			if err != nil {
				// Fail open — allow request if Redis is unavailable
				next.ServeHTTP(w, req)
				return
			}

			if count == 1 {
				// First request in window — set expiry
				r.Expire(req.Context(), key, rateLimitWindow)
			}

			if count > rateLimitRequests {
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}

			w.Header().Set("X-RateLimit-Limit", fmt.Sprintf("%d", rateLimitRequests))
			w.Header().Set("X-RateLimit-Remaining", fmt.Sprintf("%d", rateLimitRequests-count))
			next.ServeHTTP(w, req)
		})
	}
}
