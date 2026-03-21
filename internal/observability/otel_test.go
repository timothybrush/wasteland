package observability

import (
	"context"
	"errors"
	"testing"

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
