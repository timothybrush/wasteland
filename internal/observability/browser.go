package observability

import (
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

const (
	// BrowserTraceIngressPath is the same-origin browser OTLP trace ingress route.
	BrowserTraceIngressPath        = "/api/telemetry/v1/traces"
	defaultBrowserTraceSampleRatio = 0.1
	browserTraceProxyTargetEnvVar  = "WL_BROWSER_OTLP_TRACES_TARGET"
	browserTraceHeadersEnvVar      = "WL_BROWSER_OTLP_HEADERS"
	browserTraceSampleRatioEnvVar  = "WL_BROWSER_OTEL_TRACES_SAMPLE_RATIO"
)

// BrowserTracingEnabled reports whether browser trace export should be enabled.
func BrowserTracingEnabled() bool {
	return BrowserTraceProxyTarget() != "" && BrowserTraceSampleRatio() > 0
}

// BrowserTraceSampleRatio returns the browser trace sampling ratio, clamped to [0,1].
func BrowserTraceSampleRatio() float64 {
	raw := os.Getenv(browserTraceSampleRatioEnvVar)
	if raw == "" {
		if BrowserTraceProxyTarget() == "" {
			return 0
		}
		return defaultBrowserTraceSampleRatio
	}
	ratio, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		if BrowserTraceProxyTarget() == "" {
			return 0
		}
		return defaultBrowserTraceSampleRatio
	}
	if ratio < 0 {
		return 0
	}
	if ratio > 1 {
		return 1
	}
	return ratio
}

// BrowserTraceProxyTarget resolves the internal collector endpoint used by the
// same-origin browser trace proxy.
func BrowserTraceProxyTarget() string {
	if target := strings.TrimSpace(os.Getenv(browserTraceProxyTargetEnvVar)); target != "" {
		return target
	}
	if target := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")); target != "" {
		return target
	}
	if target := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")); target != "" {
		return appendTracePath(target)
	}
	return ""
}

// BrowserTraceProxyHeaders resolves static headers that should be forwarded to
// the upstream collector for browser OTLP traces.
func BrowserTraceProxyHeaders() http.Header {
	for _, raw := range []string{
		strings.TrimSpace(os.Getenv(browserTraceHeadersEnvVar)),
		strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_TRACES_HEADERS")),
		strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")),
	} {
		if raw == "" {
			continue
		}
		return parseOTLPHeaders(raw)
	}
	return nil
}

func appendTracePath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if strings.HasSuffix(u.Path, "/v1/traces") {
		return u.String()
	}
	if u.Path == "" || u.Path == "/" {
		u.Path = "/v1/traces"
		return u.String()
	}
	u.Path = strings.TrimRight(u.Path, "/") + "/v1/traces"
	return u.String()
}

func parseOTLPHeaders(raw string) http.Header {
	headers := http.Header{}
	for _, pair := range strings.Split(raw, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		name, value, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		value = strings.TrimSpace(value)
		if name == "" || value == "" {
			continue
		}
		if decoded, err := url.QueryUnescape(value); err == nil {
			value = decoded
		}
		headers.Add(name, value)
	}
	if len(headers) == 0 {
		return nil
	}
	return headers
}
