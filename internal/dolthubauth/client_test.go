package dolthubauth

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestProxyTransport_RewritesDoltHubRequestsToAuthService(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)

	var gotReq *http.Request
	var gotBody []byte
	transport := &ProxyTransport{
		baseURL:      "https://auth.example",
		tenantID:     "tenant-1",
		environment:  "staging",
		keyID:        "kid-1",
		sharedSecret: "shared-secret",
		now:          func() time.Time { return now },
		subjectID:    "subject-1",
		connectionID: "conn-1",
		inner: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			gotReq = req.Clone(req.Context())
			if req.Body != nil {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("read proxied body: %v", err)
				}
				gotBody = body
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	req := httptest.NewRequest(
		http.MethodPatch,
		"https://www.dolthub.com/api/v1alpha1/hop/wl-commons/write/main/topic?q=UPDATE",
		bytes.NewBufferString(`{"sql":"UPDATE"}`),
	)
	req.Header.Set("Authorization", "token raw-user-token")
	req.Header.Set("X-Request-Id", "req-123")
	req.Header.Set(headerServiceTimestamp, "should-not-pass-through")
	req.Header.Set(headerAuthSubjectID, "should-not-pass-through")

	resp, err := transport.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip() error = %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if gotReq == nil {
		t.Fatal("expected proxied request")
	}
	if got := gotReq.URL.String(); got != "https://auth.example/v1/proxy/api/hop/wl-commons/write/main/topic?q=UPDATE" {
		t.Fatalf("proxied URL = %q", got)
	}
	if got := gotReq.Header.Get("Authorization"); !strings.HasPrefix(got, serviceAuthPrefix+"kid-1:") {
		t.Fatalf("Authorization = %q", got)
	}
	if got := gotReq.Header.Get(headerAuthTenantID); got != "tenant-1" {
		t.Fatalf("%s = %q", headerAuthTenantID, got)
	}
	if got := gotReq.Header.Get(headerAuthEnvironment); got != "staging" {
		t.Fatalf("%s = %q", headerAuthEnvironment, got)
	}
	if got := gotReq.Header.Get(headerAuthSubjectID); got != "subject-1" {
		t.Fatalf("%s = %q", headerAuthSubjectID, got)
	}
	if got := gotReq.Header.Get(headerAuthConnectionID); got != "conn-1" {
		t.Fatalf("%s = %q", headerAuthConnectionID, got)
	}
	if got := gotReq.Header.Get("X-Request-Id"); got != "req-123" {
		t.Fatalf("X-Request-Id = %q", got)
	}
	if got := string(gotBody); got != `{"sql":"UPDATE"}` {
		t.Fatalf("proxied body = %q", got)
	}
}

func TestProxyTransport_RejectsUnsupportedTargets(t *testing.T) {
	called := false
	transport := &ProxyTransport{
		baseURL:      "https://auth.example",
		tenantID:     "tenant-1",
		environment:  "staging",
		keyID:        "kid-1",
		sharedSecret: "shared-secret",
		now:          time.Now,
		subjectID:    "subject-1",
		connectionID: "conn-1",
		inner: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			called = true
			return &http.Response{
				StatusCode: http.StatusNoContent,
				Body:       io.NopCloser(strings.NewReader("")),
				Header:     make(http.Header),
			}, nil
		}),
	}

	req := httptest.NewRequest(http.MethodGet, "https://example.com/not-dolthub", nil)
	_, err := transport.RoundTrip(req)
	if err == nil || !strings.Contains(err.Error(), ErrUnsupportedProxyTarget.Error()) {
		t.Fatalf("RoundTrip() error = %v, want unsupported proxy target", err)
	}
	if called {
		t.Fatal("expected unsupported target to fail closed before inner transport")
	}
}

func TestProxyTransport_ReturnsNonceGenerationError(t *testing.T) {
	transport := &ProxyTransport{
		baseURL:      "https://auth.example",
		tenantID:     "tenant-1",
		environment:  "staging",
		keyID:        "kid-1",
		sharedSecret: "shared-secret",
		now:          time.Now,
		subjectID:    "subject-1",
		connectionID: "conn-1",
		nonceFn: func(int) (string, error) {
			return "", io.EOF
		},
		inner: roundTripFunc(func(*http.Request) (*http.Response, error) {
			t.Fatal("inner transport should not be called when nonce generation fails")
			return nil, nil
		}),
	}

	req := httptest.NewRequest(http.MethodGet, "https://www.dolthub.com/api/v1alpha1/hop/wl-commons/main", nil)
	_, err := transport.RoundTrip(req)
	if err == nil || !strings.Contains(err.Error(), "generate proxy nonce") {
		t.Fatalf("RoundTrip() error = %v, want nonce generation failure", err)
	}
}
