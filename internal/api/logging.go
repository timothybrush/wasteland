package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gastownhall/wasteland/internal/observability"
)

// statusRecorder wraps http.ResponseWriter to capture the response status code.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (sr *statusRecorder) WriteHeader(code int) {
	if sr.wroteHeader {
		return
	}
	sr.status = code
	sr.wroteHeader = true
	sr.ResponseWriter.WriteHeader(code)
}

func (sr *statusRecorder) Write(b []byte) (int, error) {
	if !sr.wroteHeader {
		sr.WriteHeader(http.StatusOK)
	}
	return sr.ResponseWriter.Write(b)
}

// RequestLog returns middleware that logs every HTTP request with method, path,
// status, duration, and client IP. Responses with status >= 500 are logged at
// ERROR level; all others at INFO.
func RequestLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}
			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Duration("duration", time.Since(start)),
				slog.String("client_ip", clientIP(r)),
			}
			if traceID, spanID := observability.TraceIDs(r.Context()); traceID != "" {
				attrs = append(attrs,
					slog.String("trace_id", traceID),
					slog.String("span_id", spanID),
				)
			}
			logger.LogAttrs(r.Context(), level, "http request", attrs...)
		})
	}
}
