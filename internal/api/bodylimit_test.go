package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMaxBytesBody_AllowsSmallBody(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read failed", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := MaxBytesBody(1024)(inner)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"title":"hello"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("small body: got %d, want 200", rec.Code)
	}
}

func TestMaxBytesBody_RejectsLargeBody(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read failed", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := MaxBytesBody(16)(inner)
	body := strings.Repeat("x", 100)
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("large body: got %d, want 413", rec.Code)
	}
}

func TestMaxBytesBodyByPath_UsesPerPathOverride(t *testing.T) {
	called := false
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read failed", http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := MaxBytesBodyByPath(16, map[string]int64{
		"/api/telemetry/v1/traces": 128,
	})(inner)
	req := httptest.NewRequest(http.MethodPost, "/api/telemetry/v1/traces", strings.NewReader(strings.Repeat("x", 64)))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("override body: got %d, want 200", rec.Code)
	}
	if !called {
		t.Fatal("expected inner handler to be called")
	}
}
