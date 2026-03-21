package api

import "net/http"

// MaxBytesBody returns middleware that limits request body size to n bytes.
// Requests exceeding the limit receive a 413 Request Entity Too Large response.
func MaxBytesBody(n int64) func(http.Handler) http.Handler {
	return MaxBytesBodyByPath(n, nil)
}

// MaxBytesBodyByPath returns middleware that limits request body size to a
// default number of bytes, with optional per-path overrides.
func MaxBytesBodyByPath(defaultLimit int64, overrides map[string]int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := defaultLimit
			if override, ok := overrides[r.URL.Path]; ok {
				limit = override
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
