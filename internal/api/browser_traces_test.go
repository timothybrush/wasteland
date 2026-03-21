package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeConfigHandler_ReportsBrowserTracingSettings(t *testing.T) {
	t.Setenv("WL_ENVIRONMENT", "staging")
	t.Setenv("WL_BROWSER_OTLP_TRACES_TARGET", "http://collector.internal/v1/traces")
	t.Setenv("WL_BROWSER_OTEL_TRACES_SAMPLE_RATIO", "0.35")

	srv := New(nil)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/runtime-config")
	if err != nil {
		t.Fatalf("GET /api/runtime-config: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var cfg RuntimeConfigResponse
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode runtime config: %v", err)
	}
	if cfg.Environment != "staging" {
		t.Fatalf("environment = %q, want %q", cfg.Environment, "staging")
	}
	if !cfg.BrowserTracingEnabled {
		t.Fatal("expected browser tracing to be enabled")
	}
	if cfg.BrowserTraceEndpoint != "/api/telemetry/v1/traces" {
		t.Fatalf("browser_trace_endpoint = %q, want %q", cfg.BrowserTraceEndpoint, "/api/telemetry/v1/traces")
	}
	if cfg.BrowserTraceSampleRatio != 0.35 {
		t.Fatalf("browser_trace_sample_ratio = %v, want %v", cfg.BrowserTraceSampleRatio, 0.35)
	}
}

func TestBrowserTracesHandler_ProxiesPayload(t *testing.T) {
	t.Setenv("WL_BROWSER_OTLP_TRACES_TARGET", "")
	t.Setenv("WL_BROWSER_OTEL_TRACES_SAMPLE_RATIO", "0.5")

	var (
		gotBody            []byte
		gotContentType     string
		gotContentEncoding string
		gotSharedToken     string
	)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("collector method = %s, want POST", r.Method)
		}
		gotContentType = r.Header.Get("Content-Type")
		gotContentEncoding = r.Header.Get("Content-Encoding")
		gotSharedToken = r.Header.Get("X-OTLP-Shared-Token")
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read proxied body: %v", err)
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("ok"))
	}))
	defer collector.Close()

	t.Setenv("WL_BROWSER_OTLP_TRACES_TARGET", collector.URL+"/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-OTLP-Shared-Token=abc123TOKEN")

	srv := New(nil)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/telemetry/v1/traces", bytes.NewReader([]byte("trace-data")))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	req.Header.Set("Content-Encoding", "gzip")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/telemetry/v1/traces: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusAccepted, string(body))
	}
	if gotContentType != "application/x-protobuf" {
		t.Fatalf("collector content-type = %q, want %q", gotContentType, "application/x-protobuf")
	}
	if gotContentEncoding != "gzip" {
		t.Fatalf("collector content-encoding = %q, want %q", gotContentEncoding, "gzip")
	}
	if gotSharedToken != "abc123TOKEN" {
		t.Fatalf("collector shared token = %q, want %q", gotSharedToken, "abc123TOKEN")
	}
	if string(gotBody) != "trace-data" {
		t.Fatalf("collector body = %q, want %q", string(gotBody), "trace-data")
	}
}

func TestBrowserTracesHandler_RejectsOversizedPayload(t *testing.T) {
	t.Setenv("WL_BROWSER_OTLP_TRACES_TARGET", "http://collector.internal/v1/traces")
	t.Setenv("WL_BROWSER_OTEL_TRACES_SAMPLE_RATIO", "0.5")

	srv := New(nil)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	body := bytes.Repeat([]byte("x"), maxBrowserTracePayloadBytes+1)
	resp, err := http.Post(ts.URL+"/api/telemetry/v1/traces", "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/telemetry/v1/traces: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusRequestEntityTooLarge, string(payload))
	}
}

func TestBrowserTracesHandler_RejectsUnsupportedContentType(t *testing.T) {
	t.Setenv("WL_BROWSER_OTLP_TRACES_TARGET", "http://collector.internal/v1/traces")
	t.Setenv("WL_BROWSER_OTEL_TRACES_SAMPLE_RATIO", "0.5")

	srv := New(nil)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/telemetry/v1/traces", "text/plain", bytes.NewReader([]byte("trace-data")))
	if err != nil {
		t.Fatalf("POST /api/telemetry/v1/traces: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusUnsupportedMediaType {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusUnsupportedMediaType, string(payload))
	}
}

func TestBrowserTracesHandler_DisabledReturnsNotFound(t *testing.T) {
	t.Setenv("WL_BROWSER_OTLP_TRACES_TARGET", "http://collector.internal/v1/traces")
	t.Setenv("WL_BROWSER_OTEL_TRACES_SAMPLE_RATIO", "0")

	srv := New(nil)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/api/telemetry/v1/traces", "application/x-protobuf", bytes.NewReader([]byte("trace-data")))
	if err != nil {
		t.Fatalf("POST /api/telemetry/v1/traces: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // test cleanup

	if resp.StatusCode != http.StatusNotFound {
		payload, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want %d: %s", resp.StatusCode, http.StatusNotFound, string(payload))
	}
}
