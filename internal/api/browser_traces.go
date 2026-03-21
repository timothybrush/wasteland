package api

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/gastownhall/wasteland/internal/observability"
)

const maxBrowserTracePayloadBytes = 1 << 20

var browserTraceProxyClient = &http.Client{
	Timeout:   10 * time.Second,
	Transport: observability.NewTransport(http.DefaultTransport),
}

func (s *Server) handleBrowserTraces(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	target := observability.BrowserTraceProxyTarget()
	if target == "" || !observability.BrowserTracingEnabled() {
		writeError(w, http.StatusNotFound, "browser tracing not enabled")
		return
	}
	if !allowedBrowserTraceContentType(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "unsupported browser trace content type")
		return
	}

	reader := http.MaxBytesReader(w, r.Body, maxBrowserTracePayloadBytes)
	defer reader.Close() //nolint:errcheck // request cleanup

	payload, err := io.ReadAll(reader)
	if err != nil {
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "http: request body too large") {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, status, "invalid browser trace payload")
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare browser trace proxy request")
		return
	}
	req.Header.Set("Content-Type", contentTypeOrDefault(r.Header.Get("Content-Type"), "application/x-protobuf"))
	if enc := r.Header.Get("Content-Encoding"); enc != "" {
		req.Header.Set("Content-Encoding", enc)
	}
	for name, values := range observability.BrowserTraceProxyHeaders() {
		for _, value := range values {
			req.Header.Add(name, value)
		}
	}

	resp, err := browserTraceProxyClient.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "browser trace collector unavailable")
		return
	}
	defer resp.Body.Close() //nolint:errcheck // upstream cleanup

	for _, header := range []string{"Content-Type"} {
		if value := resp.Header.Get(header); value != "" {
			w.Header().Set(header, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, copyErr := io.Copy(w, resp.Body); copyErr != nil && !errors.Is(copyErr, io.EOF) {
		return
	}
}

func contentTypeOrDefault(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func allowedBrowserTraceContentType(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(value)
	if err != nil {
		return false
	}
	switch mediaType {
	case "application/x-protobuf", "application/json":
		return true
	default:
		return false
	}
}
