// Package observability configures tracing, metrics, and trace correlation helpers.
package observability

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

const defaultTraceSampleRatio = 1.0

var (
	newTraceExporter  = func(ctx context.Context) (sdktrace.SpanExporter, error) { return otlptracehttp.New(ctx) }
	newMetricExporter = func(ctx context.Context) (sdkmetric.Exporter, error) { return otlpmetrichttp.New(ctx) }
)

// Config controls OTEL resource attributes for this process.
type Config struct {
	ServiceName      string
	ServiceNamespace string
	ServiceVersion   string
	Environment      string
}

// Enabled reports whether OTLP export is configured.
func Enabled() bool {
	return traceExportEnabled() || metricsExportEnabled()
}

// SentryTraceSampleRate disables Sentry performance tracing when OTEL is active.
func SentryTraceSampleRate() float64 {
	if traceExportEnabled() {
		return 0
	}
	return 0.2
}

// Init configures global OTEL providers when OTLP export is configured.
// It returns a shutdown function and whether OTEL was enabled.
func Init(ctx context.Context, cfg Config) (func(context.Context) error, bool, error) {
	if !Enabled() {
		return func(context.Context) error { return nil }, false, nil
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithContainer(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceNamespace(cfg.ServiceNamespace),
			semconv.ServiceVersion(cfg.ServiceVersion),
			semconv.DeploymentEnvironmentName(cfg.Environment),
		),
	)
	if err != nil && !errors.Is(err, resource.ErrPartialResource) {
		return nil, true, fmt.Errorf("building otel resource: %w", err)
	}

	prevPropagator := otel.GetTextMapPropagator()
	prevTracerProvider := otel.GetTracerProvider()
	prevMeterProvider := otel.GetMeterProvider()

	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdowns := make([]func(context.Context) error, 0, 2)
	cleanupInitFailure := func(initErr error) (func(context.Context) error, bool, error) {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownAll(cleanupCtx, shutdowns)
		otel.SetTextMapPropagator(prevPropagator)
		otel.SetTracerProvider(prevTracerProvider)
		otel.SetMeterProvider(prevMeterProvider)
		return nil, true, initErr
	}

	if traceExportEnabled() {
		traceExporter, err := newTraceExporter(ctx)
		if err != nil {
			return cleanupInitFailure(fmt.Errorf("creating trace exporter: %w", err))
		}
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithResource(res),
			sdktrace.WithBatcher(traceExporter),
			sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(traceSampleRatio()))),
		)
		otel.SetTracerProvider(tp)
		shutdowns = append(shutdowns, tp.Shutdown)
	}

	if metricsExportEnabled() {
		metricExporter, err := newMetricExporter(ctx)
		if err != nil {
			return cleanupInitFailure(fmt.Errorf("creating metric exporter: %w", err))
		}
		mp := sdkmetric.NewMeterProvider(
			sdkmetric.WithResource(res),
			sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExporter, sdkmetric.WithInterval(15*time.Second))),
		)
		otel.SetMeterProvider(mp)
		shutdowns = append(shutdowns, mp.Shutdown)
	}

	return func(ctx context.Context) error {
		return shutdownAll(ctx, shutdowns)
	}, true, nil
}

// NewHTTPHandler wraps an HTTP handler with OTEL server instrumentation.
func NewHTTPHandler(next http.Handler) http.Handler {
	return otelhttp.NewHandler(next, "http.server",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			return r.Method + " " + r.URL.Path
		}),
	)
}

// NewTransport wraps an HTTP transport with OTEL client instrumentation.
func NewTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return otelhttp.NewTransport(base)
}

// TraceIDs extracts the current trace/span IDs from context.
func TraceIDs(ctx context.Context) (string, string) {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return "", ""
	}
	return sc.TraceID().String(), sc.SpanID().String()
}

func traceExportEnabled() bool {
	return exporterTypeEnabled("OTEL_TRACES_EXPORTER") && traceEndpointConfigured()
}

func metricsExportEnabled() bool {
	return exporterTypeEnabled("OTEL_METRICS_EXPORTER") && metricsEndpointConfigured()
}

func exporterTypeEnabled(envVar string) bool {
	return os.Getenv(envVar) != "none"
}

func traceEndpointConfigured() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""
}

func metricsEndpointConfigured() bool {
	return os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") != "" ||
		os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != ""
}

func traceSampleRatio() float64 {
	raw := os.Getenv("WL_OTEL_TRACES_SAMPLE_RATIO")
	if raw == "" {
		return defaultTraceSampleRatio
	}
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return defaultTraceSampleRatio
	}
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}

func shutdownAll(ctx context.Context, shutdowns []func(context.Context) error) error {
	var joined error
	for i := len(shutdowns) - 1; i >= 0; i-- {
		joined = errors.Join(joined, shutdowns[i](ctx))
	}
	return joined
}
