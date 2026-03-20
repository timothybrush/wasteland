package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestNewDoltHubProviderWithClient_UsesInjectedClient(t *testing.T) {
	t.Parallel()

	client := &http.Client{Timeout: 123 * time.Millisecond}
	provider := NewDoltHubProviderWithClient(client)
	if got := provider.getClient(time.Second); got != client {
		t.Fatalf("getClient() = %p, want injected client %p", got, client)
	}
}

func TestDoltHubProvider_CreatePR_ReturnsPRURLAndPayload(t *testing.T) {
	var gotAuth string
	var gotBody map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("authorization")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decoding request body: %v", err)
		}
		_, _ = w.Write([]byte(`{"status":"ok","_id":"42"}`))
	}))
	defer server.Close()

	oldAPI := dolthubAPIBase
	oldRepo := dolthubRepoBase
	dolthubAPIBase = server.URL
	dolthubRepoBase = "https://repo.example"
	defer func() {
		dolthubAPIBase = oldAPI
		dolthubRepoBase = oldRepo
	}()

	provider := NewDoltHubProvider("api-token")
	url, err := provider.CreatePR("fork-org", "upstream-org", "wl-commons", "wl/alice/w-1", "Title", "Body")
	if err != nil {
		t.Fatalf("CreatePR() error = %v", err)
	}
	if url != "https://repo.example/upstream-org/wl-commons/pulls/42" {
		t.Fatalf("CreatePR() URL = %q, want PR URL", url)
	}
	if gotAuth != "token api-token" {
		t.Fatalf("authorization = %q, want token api-token", gotAuth)
	}
	if gotBody["fromBranchOwnerName"] != "fork-org" || gotBody["toBranchOwnerName"] != "upstream-org" {
		t.Fatalf("payload = %+v", gotBody)
	}
	if gotBody["fromBranchName"] != "wl/alice/w-1" || gotBody["toBranchName"] != "main" {
		t.Fatalf("payload = %+v", gotBody)
	}
}

func TestDoltHubProvider_CreatePR_FallsBackToPullsPageWithoutID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	oldAPI := dolthubAPIBase
	oldRepo := dolthubRepoBase
	dolthubAPIBase = server.URL
	dolthubRepoBase = "https://repo.example"
	defer func() {
		dolthubAPIBase = oldAPI
		dolthubRepoBase = oldRepo
	}()

	provider := NewDoltHubProvider("api-token")
	url, err := provider.CreatePR("fork-org", "upstream-org", "wl-commons", "wl/alice/w-1", "Title", "Body")
	if err != nil {
		t.Fatalf("CreatePR() error = %v", err)
	}
	if url != "https://repo.example/upstream-org/wl-commons/pulls" {
		t.Fatalf("CreatePR() URL = %q, want pulls page URL", url)
	}
}

func TestDoltHubProvider_UpdatePR_And_ClosePR(t *testing.T) {
	var requests []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make(map[string]string)
		_ = json.NewDecoder(r.Body).Decode(&body)
		requests = append(requests, r.Method+" "+r.URL.Path+" "+body["title"]+body["description"]+body["state"])
		if strings.Contains(r.URL.Path, "/pulls/500") {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("upstream failed"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	oldAPI := dolthubAPIBase
	dolthubAPIBase = server.URL
	defer func() { dolthubAPIBase = oldAPI }()

	provider := NewDoltHubProvider("api-token")
	if err := provider.UpdatePR("upstream-org", "wl-commons", "42", "New title", "New body"); err != nil {
		t.Fatalf("UpdatePR() error = %v", err)
	}
	if err := provider.ClosePR("upstream-org", "wl-commons", "42"); err != nil {
		t.Fatalf("ClosePR() error = %v", err)
	}
	if err := provider.UpdatePR("upstream-org", "wl-commons", "500", "Broken", "Body"); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("UpdatePR() error = %v, want HTTP 502", err)
	}
	if err := provider.ClosePR("upstream-org", "wl-commons", "500"); err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("ClosePR() error = %v, want HTTP 502", err)
	}

	if len(requests) != 4 {
		t.Fatalf("len(requests) = %d, want 4", len(requests))
	}
	if !strings.Contains(requests[0], "PATCH /upstream-org/wl-commons/pulls/42 New titleNew body") {
		t.Fatalf("update request = %q", requests[0])
	}
	if !strings.Contains(requests[1], "PATCH /upstream-org/wl-commons/pulls/42 closed") {
		t.Fatalf("close request = %q", requests[1])
	}
}
