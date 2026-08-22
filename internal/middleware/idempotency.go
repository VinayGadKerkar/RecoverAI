package middleware

import (
	"net/http"
	"time"

	redisclient "recoverai/internal/redis"
)

// Idempotency enforces idempotent POST requests via the Idempotency-Key header.
// If the same key has been seen within TTL, returns the cached response status.
func Idempotency(r *redisclient.Client) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.Method != http.MethodPost {
				next.ServeHTTP(w, req)
				return
			}

			key := req.Header.Get("Idempotency-Key")
			if key == "" {
				next.ServeHTTP(w, req)
				return
			}

			cacheKey := "idempotency:" + key
			seen, err := r.SetNXWithTTL(req.Context(), cacheKey, "1", 24*time.Hour)
			if err != nil {
				// Fail open
				next.ServeHTTP(w, req)
				return
			}

			if seen {
				// Key already processed — return 200 without re-executing
				w.Header().Set("X-Idempotency-Replayed", "true")
				w.WriteHeader(http.StatusOK)
				return
			}

			next.ServeHTTP(w, req)
		})
	}
}
