package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestSPAHandler_ServesAssetsAndClientRoutes(t *testing.T) {
	var apiHits int
	handler := SPAHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		apiHits++
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("api"))
	}), fstest.MapFS{
		"dist/index.html":          {Data: []byte("<html>index</html>")},
		"dist/assets/app.123.js":   {Data: []byte("console.log('app');")},
		"dist/assets/logo.456.svg": {Data: []byte("<svg></svg>")},
	})

	t.Run("api route bypasses spa", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusCreated)
		}
		if apiHits != 1 {
			t.Fatalf("apiHits = %d, want 1", apiHits)
		}
	})

	t.Run("hashed assets are immutable", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/app.123.js", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
			t.Fatalf("Cache-Control = %q, want immutable asset cache header", got)
		}
		if !strings.Contains(rec.Body.String(), "console.log") {
			t.Fatalf("unexpected asset body %q", rec.Body.String())
		}
	})

	t.Run("root serves index with no-cache", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("Cache-Control = %q, want %q", got, "no-cache")
		}
		if !strings.Contains(rec.Body.String(), "<html>index</html>") {
			t.Fatalf("unexpected index body %q", rec.Body.String())
		}
	})

	t.Run("client route falls back to index", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/wanted/w-1", nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
			t.Fatalf("Cache-Control = %q, want %q", got, "no-cache")
		}
		if !strings.Contains(rec.Body.String(), "<html>index</html>") {
			t.Fatalf("unexpected fallback body %q", rec.Body.String())
		}
	})
}

func TestSPAHandler_FallsBackWhenAssetsAreMissing(t *testing.T) {
	var apiHits int
	handler := SPAHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		apiHits++
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("api"))
	}), fstest.MapFS{})

	req := httptest.NewRequest(http.MethodGet, "/app", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain", got)
	}
	if !strings.Contains(rec.Body.String(), "Web UI not built") {
		t.Fatalf("unexpected fallback body %q", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("api status = %d, want %d", rec.Code, http.StatusAccepted)
	}
	if apiHits != 1 {
		t.Fatalf("apiHits = %d, want 1", apiHits)
	}

	body, err := io.ReadAll(rec.Result().Body)
	if err != nil {
		t.Fatalf("read api body: %v", err)
	}
	if string(body) != "api" {
		t.Fatalf("api body = %q, want %q", string(body), "api")
	}
}
