package hosted

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupHostedServerWithCustomNango(t *testing.T, nangoHandler http.HandlerFunc) (*SessionStore, *httptest.Server) {
	t.Helper()

	nangoTS := httptest.NewServer(nangoHandler)
	t.Cleanup(nangoTS.Close)

	nango := NewNangoClient(NangoConfig{
		BaseURL:       nangoTS.URL,
		SecretKey:     "nango-secret",
		IntegrationID: "dolthub",
	})
	sessions := NewSessionStore()
	resolver := NewWorkspaceResolver(nango, sessions)
	server := NewServer(resolver, sessions, nango, testSecret, "staging")

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth/status", server.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/connect", server.handleConnect)
	mux.HandleFunc("POST /api/auth/connect-session", server.handleConnectSession)
	mux.HandleFunc("POST /api/auth/join", server.handleJoin)
	mux.HandleFunc("DELETE /api/auth/wastelands/{upstream...}", server.handleLeaveWasteland)

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return sessions, ts
}

func TestHandleAuthStatus_NangoFailureReportsDisconnected(t *testing.T) {
	sessions, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	sessionID, _ := sessions.Create("conn-1")
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/status", nil)
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: SignSessionCookie(sessionID, "conn-1", testSecret),
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var status authStatusResponse
	_ = json.NewDecoder(resp.Body).Decode(&status)
	if !status.Authenticated {
		t.Fatal("expected authenticated=true")
	}
	if status.Connected {
		t.Fatal("expected connected=false")
	}
	if status.Environment != "staging" {
		t.Fatalf("environment = %q, want staging", status.Environment)
	}
}

func TestHandleAuthStatus_AuthenticatedWithoutConnection(t *testing.T) {
	sessions, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	sessionID, _ := sessions.Create("")
	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/status", nil)
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: SignSessionID(sessionID, testSecret),
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var status authStatusResponse
	_ = json.NewDecoder(resp.Body).Decode(&status)
	if !status.Authenticated || status.Connected {
		t.Fatalf("status = %+v", status)
	}
}

func TestHandleAuthStatus_ExpiredSessionReportsAnonymous(t *testing.T) {
	sessions, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	sessionID, _ := sessions.Create("conn-1")
	sessions.Delete(sessionID)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/status", nil)
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: SignSessionCookie(sessionID, "conn-1", testSecret),
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	var status authStatusResponse
	_ = json.NewDecoder(resp.Body).Decode(&status)
	if status.Authenticated || status.Connected {
		t.Fatalf("status = %+v", status)
	}
}

func TestHandleConnectSession_ReportsNangoFailures(t *testing.T) {
	_, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/connect/sessions" {
			http.Error(w, "nango unavailable", http.StatusBadGateway)
			return
		}
		http.NotFound(w, r)
	})

	resp, err := http.Post(ts.URL+"/api/auth/connect-session", "application/json", strings.NewReader(`{"end_user_id":"alice"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestHandleConnect_InvalidAPIKeyIsRejected(t *testing.T) {
	oldProbe := ProbeDoltHubToken
	ProbeDoltHubToken = func(string) error { return io.EOF }
	defer func() { ProbeDoltHubToken = oldProbe }()

	_, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/connection/conn-1":
			resp := nangoConnectionResponse{ConnectionID: "conn-1"}
			resp.Credentials.APIKey = "bad-token"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/connection/"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})

	body := `{"connection_id":"conn-1","rig_handle":"alice","fork_org":"alice-org","fork_db":"wl-commons","upstream":"hop/wl-commons"}`
	resp, err := http.Post(ts.URL+"/api/auth/connect", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 400, got %d: %s", resp.StatusCode, string(b))
	}
}

func TestHandleJoin_NoMetadataReturnsPreconditionFailed(t *testing.T) {
	sessions, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/connection/conn-1":
			resp := nangoConnectionResponse{ConnectionID: "conn-1"}
			resp.Credentials.APIKey = "test-token"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	})

	sessionID, _ := sessions.Create("conn-1")
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/join", strings.NewReader(`{"fork_org":"alice-org","fork_db":"wl-commons","upstream":"hop/wl-commons"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: SignSessionCookie(sessionID, "conn-1", testSecret),
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusPreconditionFailed {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 412, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestHandleConnect_SetMetadataFailureReturns500(t *testing.T) {
	_, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/connection/conn-1":
			resp := nangoConnectionResponse{ConnectionID: "conn-1"}
			resp.Credentials.APIKey = "test-token"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/connection/"):
			http.Error(w, "cannot save", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	})

	body := `{"connection_id":"conn-1","rig_handle":"alice","fork_org":"alice-org","fork_db":"wl-commons","upstream":"hop/wl-commons"}`
	resp, err := http.Post(ts.URL+"/api/auth/connect", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusInternalServerError {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, string(b))
	}
}

func TestHandleJoin_SetMetadataFailureReturns500(t *testing.T) {
	meta := &UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{Upstream: "hop/wl-commons", ForkOrg: "alice-org", ForkDB: "wl-commons", Mode: "pr"},
		},
	}
	sessions, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/connection/conn-1":
			resp := nangoConnectionResponse{ConnectionID: "conn-1"}
			resp.Credentials.APIKey = "test-token"
			b, _ := json.Marshal(meta)
			resp.Metadata = json.RawMessage(b)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/connection/"):
			http.Error(w, "cannot save", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	})

	sessionID, _ := sessions.Create("conn-1")
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/join", strings.NewReader(`{"fork_org":"alice-org","fork_db":"gascity","upstream":"gastownhall/gascity"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: SignSessionCookie(sessionID, "conn-1", testSecret),
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestHandleLeaveWasteland_NoMetadataReturnsPreconditionFailed(t *testing.T) {
	sessions, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/connection/conn-1":
			resp := nangoConnectionResponse{ConnectionID: "conn-1"}
			resp.Credentials.APIKey = "test-token"
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	})

	sessionID, _ := sessions.Create("conn-1")
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/auth/wastelands/hop/wl-commons", nil)
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: SignSessionCookie(sessionID, "conn-1", testSecret),
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusPreconditionFailed {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 412, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestHandleLeaveWasteland_NotAuthenticated(t *testing.T) {
	_, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/auth/wastelands/hop/wl-commons", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 401, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestHandleLeaveWasteland_NoConnectionReturnsPreconditionFailed(t *testing.T) {
	sessions, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	sessionID, _ := sessions.Create("")
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/auth/wastelands/hop/wl-commons", nil)
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: SignSessionID(sessionID, testSecret),
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusPreconditionFailed {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 412, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestHandleLeaveWasteland_UpstreamNotFound(t *testing.T) {
	meta := &UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{Upstream: "hop/wl-commons", ForkOrg: "alice-org", ForkDB: "wl-commons", Mode: "wild-west"},
			{Upstream: "gastownhall/gascity", ForkOrg: "alice-org", ForkDB: "gascity", Mode: "pr"},
		},
	}
	sessions, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/connection/conn-1":
			resp := nangoConnectionResponse{ConnectionID: "conn-1"}
			resp.Credentials.APIKey = "test-token"
			b, _ := json.Marshal(meta)
			resp.Metadata = json.RawMessage(b)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/connection/"):
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	})

	sessionID, _ := sessions.Create("conn-1")
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/auth/wastelands/missing/repo", nil)
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: SignSessionCookie(sessionID, "conn-1", testSecret),
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 404, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestHandleJoin_MetadataReadFailureReturns500(t *testing.T) {
	sessions, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	sessionID, _ := sessions.Create("conn-1")
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/join", strings.NewReader(`{"fork_org":"alice-org","fork_db":"gascity","upstream":"gastownhall/gascity"}`))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: SignSessionCookie(sessionID, "conn-1", testSecret),
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestHandleLeaveWasteland_MetadataReadFailureReturns500(t *testing.T) {
	sessions, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})

	sessionID, _ := sessions.Create("conn-1")
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/auth/wastelands/hop/wl-commons", nil)
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: SignSessionCookie(sessionID, "conn-1", testSecret),
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, string(body))
	}
}

func TestHandleLeaveWasteland_SaveMetadataFailureReturns500(t *testing.T) {
	meta := &UserMetadata{
		RigHandle: "alice",
		Wastelands: []WastelandConfig{
			{Upstream: "hop/wl-commons", ForkOrg: "alice-org", ForkDB: "wl-commons", Mode: "wild-west"},
			{Upstream: "gastownhall/gascity", ForkOrg: "alice-org", ForkDB: "gascity", Mode: "pr"},
		},
	}
	sessions, ts := setupHostedServerWithCustomNango(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/connection/conn-1":
			resp := nangoConnectionResponse{ConnectionID: "conn-1"}
			resp.Credentials.APIKey = "test-token"
			b, _ := json.Marshal(meta)
			resp.Metadata = json.RawMessage(b)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(resp)
		case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/connection/"):
			http.Error(w, "cannot save", http.StatusInternalServerError)
		default:
			http.NotFound(w, r)
		}
	})

	sessionID, _ := sessions.Create("conn-1")
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/auth/wastelands/hop/wl-commons", nil)
	req.AddCookie(&http.Cookie{
		Name:  cookieName,
		Value: SignSessionCookie(sessionID, "conn-1", testSecret),
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if resp.StatusCode != http.StatusInternalServerError {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 500, got %d: %s", resp.StatusCode, string(body))
	}
}
