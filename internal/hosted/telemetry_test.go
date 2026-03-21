package hosted

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/gastownhall/wasteland/internal/api"
)

func TestHandler_PublicTelemetryRoutesBypassAuth(t *testing.T) {
	t.Setenv("WL_ENVIRONMENT", "wrong-env")
	t.Setenv("WL_BROWSER_OTEL_TRACES_SAMPLE_RATIO", "0.5")

	var proxiedBody []byte
	collector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		proxiedBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read collector body: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer collector.Close()
	t.Setenv("WL_BROWSER_OTLP_TRACES_TARGET", collector.URL+"/v1/traces")

	hostedServer := NewServer(nil, nil, nil, testSecret, "staging")
	apiServer := api.New(nil)
	apiServer.SetEnvironment("staging")
	ts := httptest.NewServer(hostedServer.Handler(apiServer, fstest.MapFS{
		"index.html": {Data: []byte("<html></html>")},
	}))
	defer ts.Close()

	runtimeResp, err := http.Get(ts.URL + "/api/runtime-config")
	if err != nil {
		t.Fatalf("GET /api/runtime-config: %v", err)
	}
	defer runtimeResp.Body.Close() //nolint:errcheck // test cleanup

	if runtimeResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(runtimeResp.Body)
		t.Fatalf("runtime-config status = %d, want %d: %s", runtimeResp.StatusCode, http.StatusOK, string(body))
	}

	var cfg api.RuntimeConfigResponse
	if err := json.NewDecoder(runtimeResp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode runtime config: %v", err)
	}
	if !cfg.BrowserTracingEnabled {
		t.Fatal("expected browser tracing enabled for public runtime config")
	}
	if cfg.Environment != "staging" {
		t.Fatalf("runtime config environment = %q, want %q", cfg.Environment, "staging")
	}

	traceResp, err := http.Post(ts.URL+"/api/telemetry/v1/traces", "application/x-protobuf", bytes.NewReader([]byte("public-trace")))
	if err != nil {
		t.Fatalf("POST /api/telemetry/v1/traces: %v", err)
	}
	defer traceResp.Body.Close() //nolint:errcheck // test cleanup

	if traceResp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(traceResp.Body)
		t.Fatalf("trace status = %d, want %d: %s", traceResp.StatusCode, http.StatusAccepted, string(body))
	}
	if string(proxiedBody) != "public-trace" {
		t.Fatalf("proxied body = %q, want %q", string(proxiedBody), "public-trace")
	}
}
