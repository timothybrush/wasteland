package observability

import "testing"

func TestBrowserTraceProxyTarget(t *testing.T) {
	t.Run("prefers explicit browser target", func(t *testing.T) {
		t.Setenv(browserTraceProxyTargetEnvVar, "http://browser-proxy/v1/traces")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://trace-exporter/v1/traces")
		if got := BrowserTraceProxyTarget(); got != "http://browser-proxy/v1/traces" {
			t.Fatalf("BrowserTraceProxyTarget() = %q, want explicit browser target", got)
		}
	})

	t.Run("falls back to trace endpoint", func(t *testing.T) {
		t.Setenv(browserTraceProxyTargetEnvVar, "")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "http://trace-exporter/v1/traces")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
		if got := BrowserTraceProxyTarget(); got != "http://trace-exporter/v1/traces" {
			t.Fatalf("BrowserTraceProxyTarget() = %q, want trace endpoint", got)
		}
	})

	t.Run("derives traces path from generic endpoint", func(t *testing.T) {
		t.Setenv(browserTraceProxyTargetEnvVar, "")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://collector:4318")
		if got := BrowserTraceProxyTarget(); got != "http://collector:4318/v1/traces" {
			t.Fatalf("BrowserTraceProxyTarget() = %q, want generic endpoint with /v1/traces", got)
		}
	})
}

func TestBrowserTraceSampleRatio(t *testing.T) {
	t.Run("disabled when no proxy target exists", func(t *testing.T) {
		t.Setenv(browserTraceProxyTargetEnvVar, "")
		t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
		t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
		t.Setenv(browserTraceSampleRatioEnvVar, "")
		if got := BrowserTraceSampleRatio(); got != 0 {
			t.Fatalf("BrowserTraceSampleRatio() = %v, want 0", got)
		}
		if BrowserTracingEnabled() {
			t.Fatal("BrowserTracingEnabled() = true without a proxy target")
		}
	})

	t.Run("defaults when target exists", func(t *testing.T) {
		t.Setenv(browserTraceProxyTargetEnvVar, "http://collector/v1/traces")
		t.Setenv(browserTraceSampleRatioEnvVar, "")
		if got := BrowserTraceSampleRatio(); got != defaultBrowserTraceSampleRatio {
			t.Fatalf("BrowserTraceSampleRatio() = %v, want %v", got, defaultBrowserTraceSampleRatio)
		}
		if !BrowserTracingEnabled() {
			t.Fatal("BrowserTracingEnabled() = false with proxy target and default ratio")
		}
	})

	t.Run("clamps explicit values", func(t *testing.T) {
		t.Setenv(browserTraceProxyTargetEnvVar, "http://collector/v1/traces")
		t.Setenv(browserTraceSampleRatioEnvVar, "5")
		if got := BrowserTraceSampleRatio(); got != 1 {
			t.Fatalf("BrowserTraceSampleRatio() = %v, want 1", got)
		}
		t.Setenv(browserTraceSampleRatioEnvVar, "-1")
		if got := BrowserTraceSampleRatio(); got != 0 {
			t.Fatalf("BrowserTraceSampleRatio() = %v, want 0", got)
		}
	})
}
