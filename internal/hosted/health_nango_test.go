package hosted

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestCreateConnectSession_SendsExpectedRequest(t *testing.T) {
	var gotMethod, gotAuth, gotContentType string
	var gotBody connectSessionAPIRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"token":"connect-token"}}`))
	}))
	defer ts.Close()

	client := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "test-secret",
		IntegrationID: "dolthub",
	})

	token, err := client.CreateConnectSession("alice")
	if err != nil {
		t.Fatalf("CreateConnectSession() error = %v", err)
	}
	if token != "connect-token" {
		t.Fatalf("CreateConnectSession() token = %q, want connect-token", token)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %s, want POST", gotMethod)
	}
	if gotAuth != "Bearer test-secret" {
		t.Fatalf("Authorization = %q, want Bearer test-secret", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", gotContentType)
	}
	if gotBody.EndUser.ID != "alice" {
		t.Fatalf("end_user.id = %q, want alice", gotBody.EndUser.ID)
	}
	if len(gotBody.AllowedIntegrations) != 1 || gotBody.AllowedIntegrations[0] != "dolthub" {
		t.Fatalf("allowed_integrations = %+v, want [dolthub]", gotBody.AllowedIntegrations)
	}
}

func TestCreateConnectSession_RetriesRetryableFailures(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("upstream exploded"))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"token":"retry-token"}}`))
	}))
	defer ts.Close()

	client := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "test-secret",
		IntegrationID: "dolthub",
	})

	token, err := client.CreateConnectSession("alice")
	if err != nil {
		t.Fatalf("CreateConnectSession() error = %v", err)
	}
	if token != "retry-token" {
		t.Fatalf("token = %q, want retry-token", token)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

func TestCreateConnectSession_DoesNotRetryClientErrors(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("bad end user"))
	}))
	defer ts.Close()

	client := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "test-secret",
		IntegrationID: "dolthub",
	})

	_, err := client.CreateConnectSession("alice")
	if err == nil || !strings.Contains(err.Error(), "nango returned 400: bad end user") {
		t.Fatalf("CreateConnectSession() error = %v, want 400 error", err)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1", attempts)
	}
}

func TestIsRetryable(t *testing.T) {
	t.Parallel()

	if isRetryable(nil) {
		t.Fatal("isRetryable(nil) = true, want false")
	}
	if !isRetryable(&retryableError{msg: "retry me"}) {
		t.Fatal("retryableError should be retryable")
	}
	if !isRetryable(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded should be retryable")
	}
	if !isRetryable(errors.New("Timeout exceeded while waiting for headers")) {
		t.Fatal("timeout message should be retryable")
	}
	if isRetryable(errors.New("permission denied")) {
		t.Fatal("plain client error should not be retryable")
	}
}

func TestSetMetadata_SendsProviderConfigKeyAndReportsFailures(t *testing.T) {
	var gotHeader string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Provider-Config-Key")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("metadata write failed"))
	}))
	defer ts.Close()

	client := NewNangoClient(NangoConfig{
		BaseURL:       ts.URL,
		SecretKey:     "test-secret",
		IntegrationID: "dolthub",
	})

	err := client.SetMetadata("conn-1", &UserMetadata{RigHandle: "alice"})
	if err == nil || !strings.Contains(err.Error(), "nango returned 500: metadata write failed") {
		t.Fatalf("SetMetadata() error = %v, want 500 error", err)
	}
	if gotHeader != "dolthub" {
		t.Fatalf("Provider-Config-Key = %q, want dolthub", gotHeader)
	}
}

func TestHealthHandler_ReportsProbeStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		roundTripper roundTripFunc
		want         string
	}{
		{
			name: "ok",
			roundTripper: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader("ok")),
					Header:     make(http.Header),
				}, nil
			},
			want: "ok",
		},
		{
			name: "degraded",
			roundTripper: func(_ *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Body:       io.NopCloser(strings.NewReader("bad gateway")),
					Header:     make(http.Header),
				}, nil
			},
			want: "degraded",
		},
		{
			name: "unreachable",
			roundTripper: func(_ *http.Request) (*http.Response, error) {
				return nil, errors.New("dial tcp timeout")
			},
			want: "unreachable",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldTransport := http.DefaultTransport
			http.DefaultTransport = tt.roundTripper
			defer func() { http.DefaultTransport = oldTransport }()

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			rec := httptest.NewRecorder()
			healthHandler().ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rec.Code)
			}
			var body map[string]string
			if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
				t.Fatalf("decoding response: %v", err)
			}
			if body["status"] != "ok" {
				t.Fatalf("status field = %q, want ok", body["status"])
			}
			if body["dolthub"] != tt.want {
				t.Fatalf("dolthub = %q, want %q", body["dolthub"], tt.want)
			}
		})
	}
}

func TestAuthError_Error(t *testing.T) {
	t.Parallel()

	err := &authError{msg: "not authenticated"}
	if got := err.Error(); got != "not authenticated" {
		t.Fatalf("Error() = %q, want not authenticated", got)
	}
}
