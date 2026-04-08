package hosted

import (
	"context"
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gastownhall/wasteland/internal/api"
	"github.com/gastownhall/wasteland/internal/dolthubauth"
	"github.com/gastownhall/wasteland/internal/sdk"
)

// AuthServiceServer hosts the browser/API boundary while delegating
// credential custody and authenticated proxying to the standalone auth service.
type AuthServiceServer struct {
	resolver      *AuthServiceWorkspaceResolver
	sessions      *SessionStore
	auth          *dolthubauth.Client
	sessionSecret string
	subjectSecret string
	forkRegistrar ProxyForkRegistrar
	environment   string
}

// NewAuthServiceServer constructs a hosted server wired to the standalone
// DoltHub auth service.
func NewAuthServiceServer(
	resolver *AuthServiceWorkspaceResolver,
	sessions *SessionStore,
	auth *dolthubauth.Client,
	sessionSecret,
	subjectSecret,
	environment string,
) *AuthServiceServer {
	if subjectSecret == "" {
		subjectSecret = sessionSecret
	}
	return &AuthServiceServer{
		resolver:      resolver,
		sessions:      sessions,
		auth:          auth,
		sessionSecret: sessionSecret,
		subjectSecret: subjectSecret,
		forkRegistrar: &DoltHubProxyForkRegistrar{},
		environment:   environment,
	}
}

// Handler returns the HTTP handler for the hosted app when it is backed by the
// standalone DoltHub auth service.
func (s *AuthServiceServer) Handler(apiServer *api.Server, assets fs.FS) http.Handler {
	mux := http.NewServeMux()

	authRL := api.RateLimit(api.NewRateLimiter(10, 10, time.Minute))
	generalRL := api.RateLimit(api.NewRateLimiter(120, 120, time.Minute))

	mux.HandleFunc("GET /healthz", healthHandler())
	mux.Handle("POST /api/auth/connect", authRL(http.HandlerFunc(s.handleConnect)))
	mux.Handle("GET /api/auth/status", authRL(http.HandlerFunc(s.handleAuthStatus)))
	mux.Handle("GET /api/bootstrap", generalRL(http.HandlerFunc(s.handleBootstrap)))
	mux.Handle("POST /api/auth/logout", authRL(http.HandlerFunc(s.handleLogout)))
	mux.Handle("POST /api/auth/connect-session", authRL(http.HandlerFunc(s.handleConnectSession)))
	mux.Handle("POST /api/auth/join", authRL(http.HandlerFunc(s.handleJoin)))
	mux.Handle("DELETE /api/auth/wastelands/{upstream...}", authRL(http.HandlerFunc(s.handleLeaveWasteland)))

	mux.HandleFunc("GET /api/runtime-config", apiServer.ServeHTTP)
	mux.HandleFunc("POST /api/telemetry/v1/traces", apiServer.ServeHTTP)
	mux.HandleFunc("OPTIONS /api/telemetry/v1/traces", apiServer.ServeHTTP)
	mux.HandleFunc("GET /api/scoreboard", apiServer.ScoreboardHandler())
	mux.HandleFunc("OPTIONS /api/scoreboard", apiServer.ScoreboardHandler())
	mux.Handle("/", generalRL(s.AuthMiddleware(api.SPAHandler(apiServer, assets))))

	return mux
}

type beginConnectRequest struct {
	RigHandle   string `json:"rig_handle"`
	ForkOrg     string `json:"fork_org"`
	ForkDB      string `json:"fork_db"`
	Upstream    string `json:"upstream"`
	Mode        string `json:"mode"`
	Signing     bool   `json:"signing"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

type beginConnectResponse struct {
	AuthServiceBaseURL string    `json:"auth_service_base_url"`
	ConnectToken       string    `json:"connect_token"`
	RedeemSecret       string    `json:"redeem_secret"`
	ExpiresAt          time.Time `json:"expires_at"`
}

type finalizeConnectRequest struct {
	ConnectionID string `json:"connection_id"`
	Upstream     string `json:"upstream,omitempty"`
	DisplayName  string `json:"display_name"`
	Email        string `json:"email"`
}

type authServiceJoinRequest struct {
	ForkOrg     string `json:"fork_org"`
	ForkDB      string `json:"fork_db"`
	Upstream    string `json:"upstream"`
	Mode        string `json:"mode"`
	Signing     *bool  `json:"signing,omitempty"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
}

func (s *AuthServiceServer) ensureSubjectID(w http.ResponseWriter, r *http.Request) (string, error) {
	if subjectID, ok := ReadSubjectCookie(r, s.subjectSecret); ok {
		return subjectID, nil
	}
	subjectID, err := generateSessionID()
	if err != nil {
		return "", err
	}
	SetSubjectCookie(w, subjectID, s.subjectSecret)
	return subjectID, nil
}

func (s *AuthServiceServer) restoreSession(r *http.Request) (string, *UserSession, bool) {
	sessionID, connectionID, ok := ReadSessionCookie(r, s.sessionSecret)
	if !ok {
		return "", nil, false
	}
	subjectID, ok := ReadSubjectCookie(r, s.subjectSecret)
	if !ok {
		return "", nil, false
	}

	session, ok := s.sessions.Get(sessionID)
	if ok {
		if session.SubjectID == "" || session.SubjectID != subjectID {
			return "", nil, false
		}
		return sessionID, session, true
	}
	if connectionID == "" {
		return "", nil, false
	}
	if _, err := s.auth.GetConnection(r.Context(), subjectID, connectionID); err != nil {
		return "", nil, false
	}
	s.sessions.RestoreWithSubject(sessionID, connectionID, subjectID)
	session, ok = s.sessions.Get(sessionID)
	if !ok {
		return "", nil, false
	}
	return sessionID, session, true
}

func (s *AuthServiceServer) handleConnectSession(w http.ResponseWriter, r *http.Request) {
	var req beginConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.RigHandle == "" || req.ForkOrg == "" || req.ForkDB == "" || req.Upstream == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "rig_handle, fork_org, fork_db, and upstream are required"})
		return
	}
	if err := validateConnectFields(req.RigHandle, req.ForkOrg, req.ForkDB, req.Upstream); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	subjectID, err := s.ensureSubjectID(w, r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to establish browser subject"})
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = "pr"
	}
	connectToken, err := s.auth.CreateConnectToken(r.Context(), subjectID, dolthubauth.UserMetadata{
		RigHandle: req.RigHandle,
		Wastelands: []dolthubauth.WastelandConfig{{
			Upstream: req.Upstream,
			ForkOrg:  req.ForkOrg,
			ForkDB:   req.ForkDB,
			Mode:     mode,
			Signing:  req.Signing,
		}},
	}, 5*time.Minute)
	if err != nil {
		slog.Error("auth-service: failed to create connect token", "error", err, "subject_id", subjectID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create connect token"})
		return
	}

	writeJSON(w, http.StatusOK, beginConnectResponse{
		AuthServiceBaseURL: s.auth.BaseURL(),
		ConnectToken:       connectToken.ConnectToken,
		RedeemSecret:       connectToken.RedeemSecret,
		ExpiresAt:          connectToken.ExpiresAt,
	})
}

func (s *AuthServiceServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	subjectID, ok := ReadSubjectCookie(r, s.subjectSecret)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "browser subject missing"})
		return
	}

	var req finalizeConnectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.ConnectionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "connection_id is required"})
		return
	}

	conn, err := s.auth.GetConnection(r.Context(), subjectID, req.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "connection not found for subject"})
		return
	}
	if len(conn.Wastelands) == 0 {
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": "connection has no wastelands"})
		return
	}
	selected := conn.Wastelands[0]
	if req.Upstream != "" {
		found := false
		for _, wl := range conn.Wastelands {
			if wl.Upstream == req.Upstream {
				selected = wl
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "requested upstream is not part of the connection"})
			return
		}
	}

	var setupWarning string
	if s.forkRegistrar != nil {
		displayName := req.DisplayName
		if displayName == "" {
			displayName = conn.RigHandle
		}
		setupWarning = s.forkRegistrar.EnsureForkAndRegister(
			s.auth.NewProxyHTTPClient(subjectID, req.ConnectionID),
			selected.Upstream,
			selected.ForkOrg,
			selected.ForkDB,
			conn.RigHandle,
			displayName,
			req.Email,
		)
	}

	sessionID, err := s.sessions.CreateWithSubject(req.ConnectionID, subjectID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}
	s.resolver.InvalidateConnection(req.ConnectionID)
	SetSessionCookie(w, sessionID, req.ConnectionID, s.sessionSecret)
	s.sessions.RememberActiveUpstream(sessionID, selected.Upstream)
	if session, ok := s.sessions.Get(sessionID); ok {
		s.resolver.WarmSession(session, conn)
	}

	resp := map[string]string{"status": "connected"}
	if setupWarning != "" {
		resp["setup_warning"] = setupWarning
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *AuthServiceServer) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	sessionID, session, ok := s.restoreSession(r)
	if !ok {
		writeJSON(w, http.StatusOK, authStatusResponse{Environment: s.environment})
		return
	}
	if session.ConnectionID == "" {
		writeJSON(w, http.StatusOK, authStatusResponse{Authenticated: true, Environment: s.environment})
		return
	}
	conn, err := s.auth.GetConnection(r.Context(), session.SubjectID, session.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusOK, authStatusResponse{Authenticated: true, Connected: false, Environment: s.environment})
		return
	}
	_ = sessionID
	writeJSON(w, http.StatusOK, authStatusResponse{
		Authenticated: true,
		Connected:     true,
		RigHandle:     conn.RigHandle,
		Wastelands:    authWastelandsToHosted(conn.Wastelands),
		Environment:   s.environment,
	})
}

func (s *AuthServiceServer) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	ctx, span := hostedTracer.Start(r.Context(), "hosted.auth_service_bootstrap")
	defer span.End()
	r = r.WithContext(ctx)

	w.Header().Set("Cache-Control", "no-store")
	resp := api.BootstrapResponse{
		Hosted:      true,
		Environment: s.environment,
	}

	sessionID, session, ok := s.restoreSession(r)
	if !ok {
		writeJSON(w, http.StatusOK, resp)
		return
	}
	resp.Authenticated = true
	if session.ConnectionID == "" {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	conn, err := s.auth.GetConnection(r.Context(), session.SubjectID, session.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusOK, resp)
		return
	}

	resp.Connected = true
	resp.RigHandle = conn.RigHandle
	resp.Wastelands = make([]api.WastelandConfigJSON, len(conn.Wastelands))
	for i, wl := range conn.Wastelands {
		resp.Wastelands[i] = api.WastelandConfigJSON{
			Upstream: wl.Upstream,
			ForkOrg:  wl.ForkOrg,
			ForkDB:   wl.ForkDB,
			Mode:     wl.Mode,
			Signing:  wl.Signing,
		}
	}
	hostedWastelands := authWastelandsToHosted(conn.Wastelands)
	resp.ActiveUpstream, resp.Mode = activeBootstrapUpstream(
		r.Header.Get("X-Wasteland"),
		s.sessions.ActiveUpstream(sessionID),
		hostedWastelands,
	)
	if resp.ActiveUpstream != "" {
		s.sessions.RememberActiveUpstream(sessionID, resp.ActiveUpstream)
		s.resolver.WarmSession(session, conn)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *AuthServiceServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	sessionID, _, ok := ReadSessionCookie(r, s.sessionSecret)
	if ok {
		s.sessions.Delete(sessionID)
	}
	ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "logged out"})
}

func (s *AuthServiceServer) handleJoin(w http.ResponseWriter, r *http.Request) {
	sessionID, session, ok := s.restoreSession(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	if session.ConnectionID == "" {
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": "DoltHub not connected"})
		return
	}

	var req authServiceJoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
		return
	}
	if req.ForkOrg == "" || req.ForkDB == "" || req.Upstream == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "fork_org, fork_db, and upstream are required"})
		return
	}
	if err := validateJoinFields(req.ForkOrg, req.ForkDB, req.Upstream); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	mode := req.Mode
	if mode == "" {
		mode = "pr"
	}
	signing := true
	if req.Signing != nil {
		signing = *req.Signing
	}
	current, err := s.auth.GetConnection(r.Context(), session.SubjectID, session.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "connection not found"})
		return
	}
	updated, err := s.auth.UpsertWasteland(
		r.Context(),
		session.SubjectID,
		session.ConnectionID,
		current.RecordVersion,
		dolthubauth.WastelandConfig{
			Upstream: req.Upstream,
			ForkOrg:  req.ForkOrg,
			ForkDB:   req.ForkDB,
			Mode:     mode,
			Signing:  signing,
		},
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to save metadata"})
		return
	}

	var setupWarning string
	if s.forkRegistrar != nil {
		setupWarning = s.forkRegistrar.EnsureForkAndRegister(
			s.auth.NewProxyHTTPClient(session.SubjectID, session.ConnectionID),
			req.Upstream,
			req.ForkOrg,
			req.ForkDB,
			updated.RigHandle,
			req.DisplayName,
			req.Email,
		)
	}

	s.resolver.InvalidateConnection(session.ConnectionID)
	s.sessions.RememberActiveUpstream(sessionID, req.Upstream)
	resp := map[string]string{"status": "joined"}
	if setupWarning != "" {
		resp["setup_warning"] = setupWarning
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *AuthServiceServer) handleLeaveWasteland(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.restoreSession(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "not authenticated"})
		return
	}
	if session.ConnectionID == "" {
		writeJSON(w, http.StatusPreconditionFailed, map[string]string{"error": "DoltHub not connected"})
		return
	}

	upstream := r.PathValue("upstream")
	if upstream == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "upstream is required"})
		return
	}
	current, err := s.auth.GetConnection(r.Context(), session.SubjectID, session.ConnectionID)
	if err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "connection not found"})
		return
	}
	if _, err := s.auth.DeleteWasteland(r.Context(), session.SubjectID, session.ConnectionID, upstream, current.RecordVersion); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to remove wasteland"})
		return
	}

	s.resolver.InvalidateConnection(session.ConnectionID)
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

// AuthMiddleware resolves the active workspace and injects it into the request
// context before authenticated API handlers run.
func (s *AuthServiceServer) AuthMiddleware(next http.Handler) http.Handler {
	isValidUpstream := func(value string) bool {
		org, db, ok := strings.Cut(value, "/")
		if !ok || org == "" || db == "" {
			return false
		}
		return !strings.ContainsAny(org, " \t\n\r") && !strings.ContainsAny(db, " \t\n\r/")
	}
	passOrBlock := func(w http.ResponseWriter, r *http.Request, status int, msg string) {
		if r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		writeJSON(w, status, map[string]string{"error": msg})
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		sessionID, session, ok := s.restoreSession(r)
		if !ok {
			passOrBlock(w, r, http.StatusUnauthorized, "not authenticated")
			return
		}
		if session.ConnectionID == "" {
			passOrBlock(w, r, http.StatusPreconditionFailed, "DoltHub not connected")
			return
		}

		workspace, err := s.resolver.ResolveContext(r.Context(), session)
		if err != nil {
			passOrBlock(w, r, http.StatusUnauthorized, "failed to resolve workspace: "+err.Error())
			return
		}

		upstream := r.Header.Get("X-Wasteland")
		upstreams := workspace.Upstreams()
		if upstream == "" && r.Method == http.MethodGet {
			if remembered := s.sessions.ActiveUpstream(sessionID); remembered != "" {
				upstream = remembered
			} else if len(upstreams) > 0 {
				upstream = upstreams[0].Upstream
			}
		}
		if upstream == "" {
			passOrBlock(w, r, http.StatusBadRequest, "X-Wasteland header required")
			return
		}
		if !isValidUpstream(upstream) {
			passOrBlock(w, r, http.StatusBadRequest, "invalid X-Wasteland format, expected org/db")
			return
		}

		client, err := workspace.Client(upstream)
		if err != nil {
			passOrBlock(w, r, http.StatusBadRequest, "unknown upstream: "+upstream)
			return
		}
		s.sessions.RememberActiveUpstream(sessionID, upstream)
		r.Header.Set("X-Wasteland", upstream)

		if impersonate := r.Header.Get("X-Impersonate"); impersonate != "" && s.environment == "staging" {
			client = client.WithRigHandle(impersonate)
		}

		ctx := api.WithResolvedReadIdentity(r.Context(), api.ResolvedReadIdentity{
			Upstream: upstream,
			Viewer:   workspace.RigHandle(),
		})
		ctx = withConnectionID(ctx, session.ConnectionID)
		ctx = withWorkspaceAndClient(ctx, workspace, client)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func authWastelandsToHosted(input []dolthubauth.WastelandConfig) []WastelandConfig {
	out := make([]WastelandConfig, len(input))
	for i, wl := range input {
		out[i] = WastelandConfig{
			Upstream: wl.Upstream,
			ForkOrg:  wl.ForkOrg,
			ForkDB:   wl.ForkDB,
			Mode:     wl.Mode,
			Signing:  wl.Signing,
		}
	}
	return out
}

func withWorkspaceAndClient(ctx context.Context, workspace *sdk.Workspace, client *sdk.Client) context.Context {
	ctx = context.WithValue(ctx, workspaceContextKey, workspace)
	ctx = context.WithValue(ctx, clientContextKey, client)
	return ctx
}

func withConnectionID(ctx context.Context, connectionID string) context.Context {
	return context.WithValue(ctx, connectionContextKey, connectionID)
}
