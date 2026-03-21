package observability

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

type noopTraceExporter struct{}

func (noopTraceExporter) ExportSpans(context.Context, []sdktrace.ReadOnlySpan) error { return nil }
func (noopTraceExporter) Shutdown(context.Context) error                             { return nil }

func TestEnabled(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	if Enabled() {
		t.Fatal("Enabled() = true with no OTLP endpoints")
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://otel.example")
	if !Enabled() {
		t.Fatal("Enabled() = false with OTLP endpoint configured")
	}
}

func TestSentryTraceSampleRate(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	if got := SentryTraceSampleRate(); got != 0.2 {
		t.Fatalf("SentryTraceSampleRate() = %v, want 0.2", got)
	}

	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "https://otel.example")
	if got := SentryTraceSampleRate(); got != 0 {
		t.Fatalf("SentryTraceSampleRate() = %v, want 0", got)
	}
}

func TestTraceIDs(t *testing.T) {
	traceID := trace.TraceID{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	spanID := trace.SpanID{1, 2, 3, 4, 5, 6, 7, 8}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))

	gotTraceID, gotSpanID := TraceIDs(ctx)
	if gotTraceID != traceID.String() {
		t.Fatalf("TraceIDs() trace = %q, want %q", gotTraceID, traceID.String())
	}
	if gotSpanID != spanID.String() {
		t.Fatalf("TraceIDs() span = %q, want %q", gotSpanID, spanID.String())
	}
}

func TestNewTransport_IsIdempotent(t *testing.T) {
	first := NewTransport(nil)
	second := NewTransport(first)
	if second != first {
		t.Fatal("NewTransport() should not double-wrap an already instrumented transport")
	}
}

type roundTripStub struct{}

func (roundTripStub) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }

func TestWrapClient_InstrumentsCloneWithoutMutatingInput(t *testing.T) {
	baseTransport := roundTripStub{}
	client := &http.Client{Transport: baseTransport}
	wrapped := WrapClient(client)
	if wrapped == client {
		t.Fatal("WrapClient() should return a cloned client pointer")
	}
	if wrapped.Transport == nil {
		t.Fatal("WrapClient() should install a transport")
	}
	if wrapped.Transport != NewTransport(wrapped.Transport) {
		t.Fatal("WrapClient() transport should already be instrumented")
	}
	if client.Transport != baseTransport {
		t.Fatal("WrapClient() should not mutate the input client transport")
	}
}

func TestInit_RestoresProvidersWhenMetricExporterInitFails(t *testing.T) {
	originalTracerProvider := sdktrace.NewTracerProvider()
	originalMeterProvider := sdkmetric.NewMeterProvider()
	originalPropagator := propagation.TraceContext{}
	originalTraceExporter := newTraceExporter
	originalMetricExporter := newMetricExporter
	otel.SetTextMapPropagator(originalPropagator)
	otel.SetTracerProvider(originalTracerProvider)
	otel.SetMeterProvider(originalMeterProvider)
	t.Cleanup(func() {
		otel.SetTextMapPropagator(originalPropagator)
		otel.SetTracerProvider(originalTracerProvider)
		otel.SetMeterProvider(originalMeterProvider)
		newTraceExporter = originalTraceExporter
		newMetricExporter = originalMetricExporter
	})

	newTraceExporter = func(context.Context) (sdktrace.SpanExporter, error) {
		return noopTraceExporter{}, nil
	}
	newMetricExporter = func(context.Context) (sdkmetric.Exporter, error) {
		return nil, errors.New("boom")
	}
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "https://otel.example/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "https://otel.example/v1/metrics")

	shutdown, enabled, err := Init(context.Background(), Config{
		ServiceName:      "wasteland-hosted",
		ServiceNamespace: "wasteland",
		ServiceVersion:   "test",
		Environment:      "test",
	})
	if !enabled {
		t.Fatal("Init() enabled = false, want true")
	}
	if err == nil {
		t.Fatal("Init() error = nil, want exporter init failure")
	}
	if shutdown != nil {
		t.Fatal("Init() shutdown != nil on init failure")
	}
	if got := otel.GetTracerProvider(); got != originalTracerProvider {
		t.Fatal("Init() did not restore the original tracer provider")
	}
	if got := otel.GetMeterProvider(); got != originalMeterProvider {
		t.Fatal("Init() did not restore the original meter provider")
	}
	if got := otel.GetTextMapPropagator(); got != originalPropagator {
		t.Fatal("Init() did not restore the original propagator")
	}
}

func TestInit_ExportsTracesAndMetricsToConfiguredOTLPEndpoints(t *testing.T) {
	originalTracerProvider := otel.GetTracerProvider()
	originalMeterProvider := otel.GetMeterProvider()
	originalPropagator := otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTextMapPropagator(originalPropagator)
		otel.SetTracerProvider(originalTracerProvider)
		otel.SetMeterProvider(originalMeterProvider)
	})

	type requestRecord struct {
		path        string
		sharedToken string
		bodyLen     int
	}

	requests := make(chan requestRecord, 8)
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read collector body: %v", err)
		}
		requests <- requestRecord{
			path:        r.URL.Path,
			sharedToken: r.Header.Get("X-OTLP-Shared-Token"),
			bodyLen:     len(body),
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer collector.Close()

	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", collector.URL+"/v1/traces")
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", collector.URL+"/v1/metrics")
	t.Setenv("OTEL_EXPORTER_OTLP_HEADERS", "X-OTLP-Shared-Token=abc123TOKEN")

	shutdown, enabled, err := Init(context.Background(), Config{
		ServiceName:      "wasteland-hosted",
		ServiceNamespace: "wasteland",
		ServiceVersion:   "test",
		Environment:      "test",
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	if !enabled {
		t.Fatal("Init() enabled = false, want true")
	}

	tracer := otel.Tracer("test")
	_, span := tracer.Start(context.Background(), "wasteland.otlp.proof")
	span.End()

	meter := otel.Meter("test")
	counter, err := meter.Int64Counter("wasteland_otlp_proof_total")
	if err != nil {
		t.Fatalf("Int64Counter() error = %v", err)
	}
	counter.Add(context.Background(), 1)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}

	seenPaths := map[string]bool{}
	timeout := time.After(5 * time.Second)
	for len(seenPaths) < 2 {
		select {
		case req := <-requests:
			if req.sharedToken != "abc123TOKEN" {
				t.Fatalf("collector shared token = %q, want %q", req.sharedToken, "abc123TOKEN")
			}
			if req.bodyLen == 0 {
				t.Fatalf("collector body for %s was empty", req.path)
			}
			seenPaths[req.path] = true
		case <-timeout:
			t.Fatalf("timed out waiting for OTLP exports, saw %v", seenPaths)
		}
	}

	if !seenPaths["/v1/traces"] {
		t.Fatal("expected OTLP trace export to hit /v1/traces")
	}
	if !seenPaths["/v1/metrics"] {
		t.Fatal("expected OTLP metrics export to hit /v1/metrics")
	}
}
