package observability

import (
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
