package middleware

import "net/http"

// CORS adds cross-origin response headers so browser clients running on a
// different origin (e.g. http://localhost:3000 or an ngrok URL) can call the
// API. The preflight OPTIONS request is short-circuited here before it hits
// the rate-limiter or JWT auth middleware.
func CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			origin = "*"
		}

		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Accept, Authorization, Content-Type, X-Request-ID, Idempotency-Key")
		w.Header().Set("Access-Control-Max-Age", "300")

		// Preflight — return immediately without hitting auth/rate-limit.
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}
