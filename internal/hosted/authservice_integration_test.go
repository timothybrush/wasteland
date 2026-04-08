package hosted

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gastownhall/wasteland/internal/api"
	"github.com/gastownhall/wasteland/internal/dolthubauth"
	"github.com/gastownhall/wasteland/internal/sdk"
)

type fakeProxyForkRegistrar struct {
	mu            sync.Mutex
	callCount     int
	lastUpstream  string
	lastForkOrg   string
	lastForkDB    string
	lastRigHandle string
}

func (f *fakeProxyForkRegistrar) EnsureForkAndRegister(_ *http.Client, upstream, forkOrg, forkDB, rigHandle, _, _ string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	f.lastUpstream = upstream
	f.lastForkOrg = forkOrg
	f.lastForkDB = forkDB
	f.lastRigHandle = rigHandle
	return ""
}

type fakeAuthService struct {
	t *testing.T

	mu                 sync.Mutex
	connectTokenCalls  int
	redeemCalls        int
	getConnectionCalls int
	lastCreateRequest  dolthubauth.CreateConnectTokenRequest
	lastCreateSubject  string
	lastGetSubject     string
	connection         *dolthubauth.ConnectionResponse
}

func (f *fakeAuthService) currentConnection(subjectID string) dolthubauth.ConnectionResponse {
	if f.connection != nil {
		conn := *f.connection
		conn.SubjectID = subjectID
		if conn.ConnectionID == "" {
			conn.ConnectionID = "conn-1"
		}
		return conn
	}
	return dolthubauth.ConnectionResponse{
		ConnectionID:  "conn-1",
		SubjectID:     subjectID,
		RigHandle:     "alice",
		Wastelands:    []dolthubauth.WastelandConfig{{Upstream: "hop/wl-commons", ForkOrg: "alice", ForkDB: "wl-commons", Mode: "pr", Signing: true}},
		Status:        dolthubauth.StatusActive,
		RecordVersion: 1,
		CreatedAt:     time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC),
		UpdatedAt:     time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC),
	}
}

func (f *fakeAuthService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/v1/connect-tokens":
		var req dolthubauth.CreateConnectTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			f.t.Fatalf("decode connect token request: %v", err)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Wasteland-HMAC ") {
			f.t.Fatalf("missing service auth header: %q", got)
		}

		f.mu.Lock()
		f.connectTokenCalls++
		f.lastCreateRequest = req
		f.lastCreateSubject = r.Header.Get("X-Auth-Subject-Id")
		f.mu.Unlock()

		writeJSON(w, http.StatusOK, dolthubauth.CreateConnectTokenResponse{
			ConnectToken: "connect-token-1",
			RedeemSecret: "redeem-secret-1",
			Metadata:     req.Metadata,
			ExpiresAt:    time.Date(2026, 4, 8, 12, 5, 0, 0, time.UTC),
		})
		return

	case r.Method == http.MethodPost && r.URL.Path == "/v1/connect-tokens/redeem":
		var req dolthubauth.RedeemConnectTokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			f.t.Fatalf("decode redeem request: %v", err)
		}
		if req.ConnectToken != "connect-token-1" || req.RedeemSecret != "redeem-secret-1" {
			f.t.Fatalf("unexpected redeem payload: %+v", req)
		}
		if req.APIKey != "api-key-123" {
			f.t.Fatalf("api key = %q", req.APIKey)
		}

		f.mu.Lock()
		f.redeemCalls++
		f.mu.Unlock()

		writeJSON(w, http.StatusOK, dolthubauth.RedeemConnectTokenResponse{
			ConnectionID: "conn-1",
			Status:       dolthubauth.StatusActive,
		})
		return

	case r.Method == http.MethodGet && r.URL.Path == "/v1/connections/conn-1":
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Wasteland-HMAC ") {
			f.t.Fatalf("missing service auth header on get connection: %q", got)
		}
		subjectID := r.Header.Get("X-Auth-Subject-Id")
		if subjectID == "" {
			f.t.Fatal("missing X-Auth-Subject-Id on get connection")
		}

		f.mu.Lock()
		f.getConnectionCalls++
		f.lastGetSubject = subjectID
		f.mu.Unlock()

		writeJSON(w, http.StatusOK, f.currentConnection(subjectID))
		return
	}

	http.Error(w, "not found", http.StatusNotFound)
}

func TestAuthServiceHostedEndToEnd(t *testing.T) {
	authStub := &fakeAuthService{t: t}
	authTS := httptest.NewServer(authStub)
	defer authTS.Close()

	authClient := dolthubauth.NewClient(dolthubauth.ClientConfig{
		BaseURL:      authTS.URL,
		TenantID:     "tenant-1",
		Environment:  "staging",
		KeyID:        "kid-1",
		SharedSecret: "shared-secret",
		Now: func() time.Time {
			return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
		},
	})

	sessions := NewSessionStore()
	resolver := NewAuthServiceWorkspaceResolver(authClient, sessions)
	defer resolver.Stop()

	hostedServer := NewAuthServiceServer(resolver, sessions, authClient, "session-secret", "subject-secret", "staging")
	forkRegistrar := &fakeProxyForkRegistrar{}
	hostedServer.forkRegistrar = forkRegistrar

	apiServer := api.NewHostedWorkspace(NewClientFunc(), NewWorkspaceFunc())
	ts := httptest.NewServer(hostedServer.Handler(apiServer, emptyFS{}))
	defer ts.Close()

	beginBody, err := json.Marshal(beginConnectRequest{
		RigHandle: "alice",
		ForkOrg:   "alice",
		ForkDB:    "wl-commons",
		Upstream:  "hop/wl-commons",
		Mode:      "pr",
		Signing:   true,
	})
	if err != nil {
		t.Fatalf("marshal begin connect: %v", err)
	}
	beginResp, err := http.Post(ts.URL+"/api/auth/connect-session", "application/json", bytes.NewReader(beginBody))
	if err != nil {
		t.Fatalf("POST /api/auth/connect-session: %v", err)
	}
	defer func() { _ = beginResp.Body.Close() }()
	if beginResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(beginResp.Body)
		t.Fatalf("connect-session failed: %d %s", beginResp.StatusCode, string(body))
	}

	var begin beginConnectResponse
	if err := json.NewDecoder(beginResp.Body).Decode(&begin); err != nil {
		t.Fatalf("decode begin connect response: %v", err)
	}
	if begin.AuthServiceBaseURL != authTS.URL {
		t.Fatalf("auth service base URL = %q", begin.AuthServiceBaseURL)
	}

	var subjectCookie *http.Cookie
	for _, c := range beginResp.Cookies() {
		if c.Name == subjectCookieName {
			subjectCookie = c
			break
		}
	}
	if subjectCookie == nil {
		t.Fatal("expected subject cookie from connect-session")
	}
	subjectID, ok := VerifySubjectID(subjectCookie.Value, "subject-secret")
	if !ok || subjectID == "" {
		t.Fatalf("invalid subject cookie: %+v", subjectCookie)
	}

	redeemPayload, err := json.Marshal(dolthubauth.RedeemConnectTokenRequest{
		ConnectToken: begin.ConnectToken,
		RedeemSecret: begin.RedeemSecret,
		APIKey:       "api-key-123",
		Metadata: dolthubauth.UserMetadata{
			RigHandle: "alice",
			Wastelands: []dolthubauth.WastelandConfig{{
				Upstream: "hop/wl-commons",
				ForkOrg:  "alice",
				ForkDB:   "wl-commons",
				Mode:     "pr",
				Signing:  true,
			}},
		},
	})
	if err != nil {
		t.Fatalf("marshal redeem payload: %v", err)
	}
	redeemResp, err := http.Post(begin.AuthServiceBaseURL+"/v1/connect-tokens/redeem", "application/json", bytes.NewReader(redeemPayload))
	if err != nil {
		t.Fatalf("POST redeem: %v", err)
	}
	defer func() { _ = redeemResp.Body.Close() }()
	if redeemResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(redeemResp.Body)
		t.Fatalf("redeem failed: %d %s", redeemResp.StatusCode, string(body))
	}

	finalizeReq, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/connect", strings.NewReader(`{"connection_id":"conn-1","upstream":"hop/wl-commons"}`))
	if err != nil {
		t.Fatalf("create finalize request: %v", err)
	}
	finalizeReq.Header.Set("Content-Type", "application/json")
	finalizeReq.AddCookie(subjectCookie)

	finalizeResp, err := http.DefaultClient.Do(finalizeReq)
	if err != nil {
		t.Fatalf("POST /api/auth/connect: %v", err)
	}
	defer func() { _ = finalizeResp.Body.Close() }()
	if finalizeResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(finalizeResp.Body)
		t.Fatalf("connect failed: %d %s", finalizeResp.StatusCode, string(body))
	}

	var sessionCookie *http.Cookie
	for _, c := range finalizeResp.Cookies() {
		if c.Name == cookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie from finalize connect")
	}

	sessionID, connectionID, ok := VerifySessionCookie(sessionCookie.Value, "session-secret")
	if !ok {
		t.Fatalf("invalid session cookie: %+v", sessionCookie)
	}
	if connectionID != "conn-1" {
		t.Fatalf("session connectionID = %q", connectionID)
	}

	configReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/config", nil)
	if err != nil {
		t.Fatalf("create config request: %v", err)
	}
	configReq.AddCookie(subjectCookie)
	configReq.AddCookie(sessionCookie)

	configResp, err := http.DefaultClient.Do(configReq)
	if err != nil {
		t.Fatalf("GET /api/config: %v", err)
	}
	defer func() { _ = configResp.Body.Close() }()
	if configResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(configResp.Body)
		t.Fatalf("config failed: %d %s", configResp.StatusCode, string(body))
	}

	var cfg api.ConfigResponse
	if err := json.NewDecoder(configResp.Body).Decode(&cfg); err != nil {
		t.Fatalf("decode config response: %v", err)
	}
	if cfg.RigHandle != "alice" {
		t.Fatalf("rig_handle = %q", cfg.RigHandle)
	}
	if !cfg.Hosted || !cfg.Connected {
		t.Fatalf("hosted config flags = %+v", cfg)
	}
	if len(cfg.Upstreams) != 1 || cfg.Upstreams[0].Upstream != "hop/wl-commons" {
		t.Fatalf("config upstreams = %+v", cfg.Upstreams)
	}

	sessions.Delete(sessionID)

	restoreReq, err := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/status", nil)
	if err != nil {
		t.Fatalf("create auth status request: %v", err)
	}
	restoreReq.AddCookie(subjectCookie)
	restoreReq.AddCookie(sessionCookie)

	restoreResp, err := http.DefaultClient.Do(restoreReq)
	if err != nil {
		t.Fatalf("GET /api/auth/status: %v", err)
	}
	defer func() { _ = restoreResp.Body.Close() }()
	if restoreResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(restoreResp.Body)
		t.Fatalf("auth status failed: %d %s", restoreResp.StatusCode, string(body))
	}

	var status authStatusResponse
	if err := json.NewDecoder(restoreResp.Body).Decode(&status); err != nil {
		t.Fatalf("decode auth status: %v", err)
	}
	if !status.Authenticated || !status.Connected || status.RigHandle != "alice" {
		t.Fatalf("auth status = %+v", status)
	}

	authStub.mu.Lock()
	defer authStub.mu.Unlock()
	if authStub.connectTokenCalls != 1 {
		t.Fatalf("connect token calls = %d", authStub.connectTokenCalls)
	}
	if authStub.redeemCalls != 1 {
		t.Fatalf("redeem calls = %d", authStub.redeemCalls)
	}
	if authStub.getConnectionCalls < 3 {
		t.Fatalf("get connection calls = %d, want at least 3", authStub.getConnectionCalls)
	}
	if authStub.lastCreateSubject != subjectID {
		t.Fatalf("create subject = %q, want %q", authStub.lastCreateSubject, subjectID)
	}
	if authStub.lastGetSubject != subjectID {
		t.Fatalf("get connection subject = %q, want %q", authStub.lastGetSubject, subjectID)
	}
	if authStub.lastCreateRequest.Metadata.RigHandle != "alice" {
		t.Fatalf("connect token metadata = %+v", authStub.lastCreateRequest.Metadata)
	}
	if forkRegistrar.callCount != 1 {
		t.Fatalf("fork registrar calls = %d", forkRegistrar.callCount)
	}
	if forkRegistrar.lastUpstream != "hop/wl-commons" || forkRegistrar.lastForkOrg != "alice" || forkRegistrar.lastRigHandle != "alice" {
		t.Fatalf("fork registrar args = %+v", forkRegistrar)
	}
}

func TestAuthServiceRestoreSessionRejectsSubjectMismatchForHotSession(t *testing.T) {
	authStub := &fakeAuthService{t: t}
	authTS := httptest.NewServer(authStub)
	defer authTS.Close()

	authClient := dolthubauth.NewClient(dolthubauth.ClientConfig{
		BaseURL:      authTS.URL,
		TenantID:     "tenant-1",
		Environment:  "staging",
		KeyID:        "kid-1",
		SharedSecret: "shared-secret",
		Now: func() time.Time {
			return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
		},
	})

	sessions := NewSessionStore()
	resolver := NewAuthServiceWorkspaceResolver(authClient, sessions)
	defer resolver.Stop()

	hostedServer := NewAuthServiceServer(resolver, sessions, authClient, "session-secret", "subject-secret", "staging")
	apiServer := api.NewHostedWorkspace(NewClientFunc(), NewWorkspaceFunc())
	ts := httptest.NewServer(hostedServer.Handler(apiServer, emptyFS{}))
	defer ts.Close()

	subjectID := "subject-a"
	sessionID, err := sessions.CreateWithSubject("conn-1", subjectID)
	if err != nil {
		t.Fatalf("CreateWithSubject() error = %v", err)
	}

	subjectCookie := &http.Cookie{Name: subjectCookieName, Value: SignSubjectID("subject-b", "subject-secret")}
	sessionCookie := &http.Cookie{Name: cookieName, Value: SignSessionCookie(sessionID, "conn-1", "session-secret")}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/api/auth/status", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.AddCookie(subjectCookie)
	req.AddCookie(sessionCookie)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/auth/status: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("auth status failed: %d %s", resp.StatusCode, string(body))
	}

	var status authStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode auth status: %v", err)
	}
	if status.Authenticated || status.Connected {
		t.Fatalf("expected anonymous status for subject mismatch, got %+v", status)
	}
}

func TestAuthServiceFinalizeConnectRefreshesCachedWorkspace(t *testing.T) {
	authStub := &fakeAuthService{t: t}
	authTS := httptest.NewServer(authStub)
	defer authTS.Close()

	authClient := dolthubauth.NewClient(dolthubauth.ClientConfig{
		BaseURL:      authTS.URL,
		TenantID:     "tenant-1",
		Environment:  "staging",
		KeyID:        "kid-1",
		SharedSecret: "shared-secret",
		Now: func() time.Time {
			return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
		},
	})

	sessions := NewSessionStore()
	resolver := NewAuthServiceWorkspaceResolver(authClient, sessions)
	defer resolver.Stop()
	resolver.cacheWorkspace("conn-1", sdk.NewWorkspace("stale"))

	hostedServer := NewAuthServiceServer(resolver, sessions, authClient, "session-secret", "subject-secret", "staging")
	hostedServer.forkRegistrar = &fakeProxyForkRegistrar{}

	apiServer := api.NewHostedWorkspace(NewClientFunc(), NewWorkspaceFunc())
	ts := httptest.NewServer(hostedServer.Handler(apiServer, emptyFS{}))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/connect", strings.NewReader(`{"connection_id":"conn-1","upstream":"hop/wl-commons"}`))
	if err != nil {
		t.Fatalf("create finalize request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  subjectCookieName,
		Value: SignSubjectID("subject-1", "subject-secret"),
		Path:  "/",
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/auth/connect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("connect failed: %d %s", resp.StatusCode, string(body))
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		cached, ok := resolver.cachedWorkspace("conn-1")
		if ok && cached.RigHandle() == "alice" {
			return
		}
		if time.Now().After(deadline) {
			rigHandle := "<missing>"
			if ok {
				rigHandle = cached.RigHandle()
			}
			t.Fatalf("cached workspace rig_handle = %q, want refreshed cache for conn-1", rigHandle)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestAuthServiceFinalizeConnectUsesRequestedUpstream(t *testing.T) {
	authStub := &fakeAuthService{
		t: t,
		connection: &dolthubauth.ConnectionResponse{
			ConnectionID: "conn-1",
			RigHandle:    "alice",
			Wastelands: []dolthubauth.WastelandConfig{
				{Upstream: "hop/wl-commons", ForkOrg: "alice", ForkDB: "wl-commons", Mode: "pr", Signing: true},
				{Upstream: "hop/wl-alt", ForkOrg: "alice-alt", ForkDB: "wl-alt", Mode: "pr", Signing: true},
			},
			Status:        dolthubauth.StatusActive,
			RecordVersion: 1,
			CreatedAt:     time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC),
			UpdatedAt:     time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC),
		},
	}
	authTS := httptest.NewServer(authStub)
	defer authTS.Close()

	authClient := dolthubauth.NewClient(dolthubauth.ClientConfig{
		BaseURL:      authTS.URL,
		TenantID:     "tenant-1",
		Environment:  "staging",
		KeyID:        "kid-1",
		SharedSecret: "shared-secret",
		Now: func() time.Time {
			return time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
		},
	})

	sessions := NewSessionStore()
	resolver := NewAuthServiceWorkspaceResolver(authClient, sessions)
	defer resolver.Stop()

	hostedServer := NewAuthServiceServer(resolver, sessions, authClient, "session-secret", "subject-secret", "staging")
	forkRegistrar := &fakeProxyForkRegistrar{}
	hostedServer.forkRegistrar = forkRegistrar

	apiServer := api.NewHostedWorkspace(NewClientFunc(), NewWorkspaceFunc())
	ts := httptest.NewServer(hostedServer.Handler(apiServer, emptyFS{}))
	defer ts.Close()

	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/auth/connect", strings.NewReader(`{"connection_id":"conn-1","upstream":"hop/wl-alt"}`))
	if err != nil {
		t.Fatalf("create finalize request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{
		Name:  subjectCookieName,
		Value: SignSubjectID("subject-1", "subject-secret"),
		Path:  "/",
	})

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /api/auth/connect: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("connect failed: %d %s", resp.StatusCode, string(body))
	}

	if forkRegistrar.callCount != 1 {
		t.Fatalf("fork registrar calls = %d", forkRegistrar.callCount)
	}
	if forkRegistrar.lastUpstream != "hop/wl-alt" || forkRegistrar.lastForkOrg != "alice-alt" || forkRegistrar.lastForkDB != "wl-alt" {
		t.Fatalf("fork registrar args = %+v", forkRegistrar)
	}

	var sessionCookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == cookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie from finalize connect")
	}
	sessionID, _, ok := VerifySessionCookie(sessionCookie.Value, "session-secret")
	if !ok {
		t.Fatalf("invalid session cookie: %+v", sessionCookie)
	}
	if got := sessions.ActiveUpstream(sessionID); got != "hop/wl-alt" {
		t.Fatalf("active upstream = %q", got)
	}
}
