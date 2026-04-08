package dolthubauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gastownhall/wasteland/internal/observability"
)

const publicProbeURL = "https://www.dolthub.com/api/v1alpha1/hop/wl-commons/main?q=SELECT%201"

const (
	defaultConnectTokenTTL = 5 * time.Minute
	maxConnectTokenTTL     = 5 * time.Minute
	validationProbeTimeout = 15 * time.Second
)

// LocalMasterKey is the software-backed encryption dependency used by the
// current auth-service implementation.
type LocalMasterKey struct {
	key string
}

// NewLocalMasterKey constructs a local master-key dependency.
func NewLocalMasterKey(raw string) (*LocalMasterKey, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("DOLTHUB_AUTH_MASTER_KEY is required")
	}
	return &LocalMasterKey{key: raw}, nil
}

// Check verifies the local master key is present.
func (k *LocalMasterKey) Check(context.Context) error {
	if strings.TrimSpace(k.key) == "" {
		return fmt.Errorf("local master key is not configured")
	}
	return nil
}

// Dependencies are the external runtime dependencies required by the auth
// service.
type Dependencies struct {
	Store              AuthStore
	KeyManager         CredentialCipher
	ValidateCredential func(context.Context, string) (ValidationErrorCode, error)
	ProxyHTTPClient    *http.Client
	Now                func() time.Time
	Version            string
}

// Server exposes health endpoints plus the auth-service API.
type Server struct {
	cfg                Config
	deps               Dependencies
	handler            http.Handler
	allowlist          map[string]struct{}
	validateCredential func(context.Context, string) (ValidationErrorCode, error)
	proxyHTTPClient    *http.Client
}

type statusResponse struct {
	Service     string            `json:"service"`
	Status      string            `json:"status"`
	Environment string            `json:"environment,omitempty"`
	TenantID    string            `json:"tenant_id,omitempty"`
	Version     string            `json:"version,omitempty"`
	Time        string            `json:"time,omitempty"`
	Checks      map[string]string `json:"checks,omitempty"`
	Error       string            `json:"error,omitempty"`
}

// NewServer constructs the standalone auth-service HTTP server.
func NewServer(cfg Config, deps Dependencies) (*Server, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if deps.Store == nil {
		return nil, fmt.Errorf("auth-service store is required")
	}
	if deps.KeyManager == nil {
		return nil, fmt.Errorf("auth-service key manager is required")
	}
	if deps.Now == nil {
		deps.Now = time.Now
	}
	if deps.ValidateCredential == nil {
		deps.ValidateCredential = validateDoltHubAPIKey
	}
	if deps.ProxyHTTPClient == nil {
		deps.ProxyHTTPClient = observability.WrapClient(&http.Client{Timeout: 60 * time.Second})
	}

	s := &Server{
		cfg:                cfg,
		deps:               deps,
		allowlist:          make(map[string]struct{}, len(cfg.AllowedOrigins)),
		validateCredential: deps.ValidateCredential,
		proxyHTTPClient:    deps.ProxyHTTPClient,
	}
	for _, origin := range cfg.AllowedOrigins {
		s.allowlist[origin] = struct{}{}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.handleRoot)
	mux.HandleFunc("GET /livez", s.handleLiveness)
	mux.HandleFunc("GET /readyz", s.handleReadiness)
	mux.HandleFunc("POST /v1/connect-tokens", s.handleCreateConnectToken)
	mux.HandleFunc("POST /v1/connect-tokens/redeem", s.handleRedeemConnectToken)
	mux.HandleFunc("GET /v1/connections/{connection_id}", s.handleGetConnection)
	mux.HandleFunc("GET /v1/subjects/{subject_id}/connection", s.handleNotImplemented("GET /v1/subjects/{subject_id}/connection"))
	mux.HandleFunc("PATCH /v1/connections/{connection_id}/rig-handle", s.handlePatchRigHandle)
	mux.HandleFunc("PUT /v1/connections/{connection_id}/wastelands/{upstream...}", s.handleUpsertWasteland)
	mux.HandleFunc("DELETE /v1/connections/{connection_id}/wastelands/{upstream...}", s.handleDeleteWasteland)
	mux.HandleFunc("PATCH /v1/connections/{connection_id}/wasteland-settings/{upstream...}", s.handlePatchWastelandSettings)
	mux.HandleFunc("POST /v1/proxy/graphql", s.handleProxyGraphQL)
	mux.HandleFunc("GET /v1/proxy/api/{path...}", s.handleProxyAPI)
	mux.HandleFunc("POST /v1/proxy/api/{path...}", s.handleProxyAPI)
	mux.HandleFunc("PATCH /v1/proxy/api/{path...}", s.handleProxyAPI)
	mux.HandleFunc("PUT /v1/proxy/api/{path...}", s.handleProxyAPI)
	mux.HandleFunc("DELETE /v1/proxy/api/{path...}", s.handleProxyAPI)
	s.handler = s.withCORS(mux)

	return s, nil
}

// Handler returns the auth-service HTTP handler.
func (s *Server) Handler() http.Handler {
	return s.handler
}

func (s *Server) handleRoot(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{
		Service:     "dolthub-auth",
		Status:      "ok",
		Environment: s.cfg.Environment,
		TenantID:    s.cfg.TenantID,
		Version:     s.deps.Version,
		Time:        s.deps.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleLiveness(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, statusResponse{
		Service: "dolthub-auth",
		Status:  "ok",
		Time:    s.deps.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleReadiness(w http.ResponseWriter, r *http.Request) {
	checks := map[string]string{
		"postgres":    "ok",
		"key_manager": "ok",
	}
	var failures []string
	if err := s.deps.Store.Check(r.Context()); err != nil {
		checks["postgres"] = err.Error()
		failures = append(failures, fmt.Sprintf("postgres: %v", err))
	}
	if err := s.deps.KeyManager.Check(r.Context()); err != nil {
		checks["key_manager"] = err.Error()
		failures = append(failures, fmt.Sprintf("key_manager: %v", err))
	}
	if len(failures) > 0 {
		writeJSON(w, http.StatusServiceUnavailable, statusResponse{
			Service: "dolthub-auth",
			Status:  "not_ready",
			Checks:  checks,
			Error:   strings.Join(failures, "; "),
			Time:    s.deps.Now().UTC().Format(time.RFC3339),
		})
		return
	}
	writeJSON(w, http.StatusOK, statusResponse{
		Service: "dolthub-auth",
		Status:  "ready",
		Checks:  checks,
		Time:    s.deps.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) handleCreateConnectToken(w http.ResponseWriter, r *http.Request) {
	scope, body, ok := s.requireServiceAuth(w, r, true, false)
	if !ok {
		return
	}
	var req CreateConnectTokenRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_json",
			UserMessage: "invalid JSON payload",
		})
		return
	}
	if scope.SubjectID == "" || req.SubjectID == "" || scope.SubjectID != req.SubjectID {
		s.writeError(w, http.StatusUnauthorized, ErrorResponse{
			ErrorCode:   "subject_mismatch",
			UserMessage: "service-auth subject does not match the requested subject",
		})
		return
	}
	if err := validateMetadata(req.Metadata); err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_metadata",
			UserMessage: err.Error(),
		})
		return
	}

	connectToken, err := randomHex(32)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, ErrorResponse{
			ErrorCode:   "random_generation_failed",
			UserMessage: "could not mint connect token",
			Retryable:   true,
		})
		return
	}
	redeemSecret, err := randomHex(32)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, ErrorResponse{
			ErrorCode:   "random_generation_failed",
			UserMessage: "could not mint redeem secret",
			Retryable:   true,
		})
		return
	}

	ttl := defaultConnectTokenTTL
	if req.TTLSeconds > 0 {
		ttl = time.Duration(req.TTLSeconds) * time.Second
		if ttl > maxConnectTokenTTL {
			ttl = maxConnectTokenTTL
		}
	}
	now := s.deps.Now().UTC()
	if err := s.deps.Store.CreateConnectToken(
		r.Context(),
		macSHA256(s.cfg.TokenPepper, []byte(connectToken)),
		macSHA256(s.cfg.RedeemPepper, []byte(redeemSecret)),
		req.SubjectID,
		req.Metadata,
		now.Add(ttl),
		now,
	); err != nil {
		s.writeError(w, http.StatusInternalServerError, ErrorResponse{
			ErrorCode:   "store_write_failed",
			UserMessage: "could not persist connect token",
			Retryable:   true,
		})
		return
	}

	writeJSON(w, http.StatusOK, CreateConnectTokenResponse{
		ConnectToken: connectToken,
		RedeemSecret: redeemSecret,
		Metadata:     req.Metadata,
		ExpiresAt:    now.Add(ttl),
	})
}

func (s *Server) handleRedeemConnectToken(w http.ResponseWriter, r *http.Request) {
	var req RedeemConnectTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_json",
			UserMessage: "invalid JSON payload",
		})
		return
	}
	if req.ConnectToken == "" || req.RedeemSecret == "" || req.APIKey == "" {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "missing_fields",
			UserMessage: "connect_token, redeem_secret, and api_key are required",
		})
		return
	}
	if err := validateMetadata(req.Metadata); err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_metadata",
			UserMessage: err.Error(),
		})
		return
	}

	conn, err := s.deps.Store.RedeemConnectToken(r.Context(), RedeemInput{
		TenantID:           s.cfg.TenantID,
		Environment:        s.cfg.Environment,
		ConnectTokenMAC:    macSHA256(s.cfg.TokenPepper, []byte(req.ConnectToken)),
		RedeemSecretMAC:    macSHA256(s.cfg.RedeemPepper, []byte(req.RedeemSecret)),
		Metadata:           req.Metadata,
		APIKey:             req.APIKey,
		Now:                s.deps.Now().UTC(),
		Cipher:             s.deps.KeyManager,
		ValidateCredential: s.validateCredential,
	})
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidConnectToken):
			s.writeError(w, http.StatusUnauthorized, ErrorResponse{
				ErrorCode:   "invalid_connect_token",
				UserMessage: "The connect token is invalid or has already been used.",
			})
		case errors.Is(err, ErrExpiredConnectToken):
			s.writeError(w, http.StatusUnauthorized, ErrorResponse{
				ErrorCode:   "expired_connect_token",
				UserMessage: "The connect token has expired. Start the connect flow again.",
			})
		case errors.Is(err, ErrMetadataMismatch):
			s.writeError(w, http.StatusUnauthorized, ErrorResponse{
				ErrorCode:   "metadata_mismatch",
				UserMessage: "The submitted metadata did not match the approved connect-token payload.",
			})
		case errors.Is(err, ErrValidationFailed):
			var validationErr *ValidationError
			if errors.As(err, &validationErr) {
				s.writeValidationError(w, validationErr)
				return
			}
			s.writeError(w, http.StatusBadRequest, ErrorResponse{
				ErrorCode:   string(ValidationInvalidKey),
				UserMessage: "DoltHub rejected the API key. Verify you created an API token with the required permissions.",
			})
		default:
			s.writeError(w, http.StatusInternalServerError, ErrorResponse{
				ErrorCode:   "redeem_failed",
				UserMessage: "The auth service could not store the DoltHub credential.",
				Retryable:   true,
			})
		}
		return
	}

	writeJSON(w, http.StatusOK, RedeemConnectTokenResponse{
		ConnectionID:    conn.ConnectionID,
		Status:          conn.Status,
		LastValidatedAt: conn.LastValidatedAt,
	})
}

func (s *Server) handleGetConnection(w http.ResponseWriter, r *http.Request) {
	scope, _, ok := s.requireServiceAuth(w, r, true, true)
	if !ok {
		return
	}
	connectionID := r.PathValue("connection_id")
	if connectionID == "" || connectionID != scope.ConnectionID {
		s.writeError(w, http.StatusUnauthorized, ErrorResponse{
			ErrorCode:   "connection_mismatch",
			UserMessage: "service-auth connection_id did not match the requested connection",
		})
		return
	}

	conn, err := s.deps.Store.GetConnection(r.Context(), scope.TenantID, scope.Environment, scope.SubjectID, connectionID)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, NewConnectionResponse(conn))
}

func (s *Server) handlePatchRigHandle(w http.ResponseWriter, r *http.Request) {
	scope, body, ok := s.requireServiceAuth(w, r, true, true)
	if !ok {
		return
	}
	connectionID := r.PathValue("connection_id")
	if connectionID == "" || connectionID != scope.ConnectionID {
		s.writeError(w, http.StatusUnauthorized, ErrorResponse{
			ErrorCode:   "connection_mismatch",
			UserMessage: "service-auth connection_id did not match the requested connection",
		})
		return
	}
	var req RigHandlePatchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_json",
			UserMessage: "invalid JSON payload",
		})
		return
	}
	if err := validateSlug("rig_handle", req.RigHandle); err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_metadata",
			UserMessage: err.Error(),
		})
		return
	}
	conn, err := s.deps.Store.PatchRigHandle(
		r.Context(),
		scope.TenantID,
		scope.Environment,
		scope.SubjectID,
		connectionID,
		req.RigHandle,
		req.RecordVersion,
		s.deps.Now().UTC(),
	)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, NewConnectionResponse(conn))
}

func (s *Server) handleUpsertWasteland(w http.ResponseWriter, r *http.Request) {
	scope, body, ok := s.requireServiceAuth(w, r, true, true)
	if !ok {
		return
	}
	connectionID := r.PathValue("connection_id")
	upstream := r.PathValue("upstream")
	if connectionID == "" || connectionID != scope.ConnectionID {
		s.writeError(w, http.StatusUnauthorized, ErrorResponse{
			ErrorCode:   "connection_mismatch",
			UserMessage: "service-auth connection_id did not match the requested connection",
		})
		return
	}
	var req WastelandUpsertRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_json",
			UserMessage: "invalid JSON payload",
		})
		return
	}
	if upstream != req.Wasteland.Upstream {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "upstream_mismatch",
			UserMessage: "path upstream did not match the request payload",
		})
		return
	}
	req.Wasteland.Mode = normalizeMode(req.Wasteland.Mode)
	if err := validateWastelandConfig(req.Wasteland); err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_metadata",
			UserMessage: err.Error(),
		})
		return
	}
	conn, err := s.deps.Store.UpsertWasteland(
		r.Context(),
		scope.TenantID,
		scope.Environment,
		scope.SubjectID,
		connectionID,
		req.Wasteland,
		req.RecordVersion,
		s.deps.Now().UTC(),
	)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, NewConnectionResponse(conn))
}

func (s *Server) handleDeleteWasteland(w http.ResponseWriter, r *http.Request) {
	scope, body, ok := s.requireServiceAuth(w, r, true, true)
	if !ok {
		return
	}
	connectionID := r.PathValue("connection_id")
	upstream := r.PathValue("upstream")
	if connectionID == "" || connectionID != scope.ConnectionID {
		s.writeError(w, http.StatusUnauthorized, ErrorResponse{
			ErrorCode:   "connection_mismatch",
			UserMessage: "service-auth connection_id did not match the requested connection",
		})
		return
	}
	var req struct {
		RecordVersion int `json:"record_version"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_json",
			UserMessage: "invalid JSON payload",
		})
		return
	}
	conn, err := s.deps.Store.DeleteWasteland(
		r.Context(),
		scope.TenantID,
		scope.Environment,
		scope.SubjectID,
		connectionID,
		upstream,
		req.RecordVersion,
		s.deps.Now().UTC(),
	)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, NewConnectionResponse(conn))
}

func (s *Server) handlePatchWastelandSettings(w http.ResponseWriter, r *http.Request) {
	scope, body, ok := s.requireServiceAuth(w, r, true, true)
	if !ok {
		return
	}
	connectionID := r.PathValue("connection_id")
	upstream := r.PathValue("upstream")
	if connectionID == "" || connectionID != scope.ConnectionID {
		s.writeError(w, http.StatusUnauthorized, ErrorResponse{
			ErrorCode:   "connection_mismatch",
			UserMessage: "service-auth connection_id did not match the requested connection",
		})
		return
	}
	var req WastelandSettingsPatchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_json",
			UserMessage: "invalid JSON payload",
		})
		return
	}
	req.Mode = normalizeMode(req.Mode)
	if err := validateMode(req.Mode); err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_metadata",
			UserMessage: err.Error(),
		})
		return
	}
	conn, err := s.deps.Store.PatchWastelandSettings(
		r.Context(),
		scope.TenantID,
		scope.Environment,
		scope.SubjectID,
		connectionID,
		upstream,
		req.Mode,
		req.Signing,
		req.RecordVersion,
		s.deps.Now().UTC(),
	)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, NewConnectionResponse(conn))
}

func (s *Server) handleProxyGraphQL(w http.ResponseWriter, r *http.Request) {
	s.handleProxy(w, r, "https://www.dolthub.com/graphql")
}

func (s *Server) handleProxyAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.PathValue("path"), "/")
	target := "https://www.dolthub.com/api/v1alpha1"
	if path != "" {
		target += "/" + path
	}
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	s.handleProxy(w, r, target)
}

func (s *Server) handleProxy(w http.ResponseWriter, r *http.Request, target string) {
	scope, body, ok := s.requireServiceAuth(w, r, true, true)
	if !ok {
		return
	}
	conn, apiKey, err := s.deps.Store.GetConnectionCredential(
		r.Context(),
		scope.TenantID,
		scope.Environment,
		scope.SubjectID,
		scope.ConnectionID,
		s.deps.KeyManager,
	)
	if err != nil {
		s.writeStoreError(w, err)
		return
	}
	_ = conn

	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, bytes.NewReader(body))
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, ErrorResponse{
			ErrorCode:   "proxy_request_failed",
			UserMessage: "could not construct DoltHub proxy request",
			Retryable:   true,
		})
		return
	}
	copyProxyHeaders(req.Header, r.Header)
	req.Header.Set("Authorization", "token "+apiKey)

	resp, err := s.proxyHTTPClient.Do(req)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, ErrorResponse{
			ErrorCode:   "upstream_unreachable",
			UserMessage: "DoltHub is temporarily unreachable",
			Retryable:   true,
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	copyResponseHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) handleNotImplemented(route string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		s.writeError(w, http.StatusNotImplemented, ErrorResponse{
			ErrorCode:   "not_implemented",
			UserMessage: fmt.Sprintf("%s is not implemented yet.", route),
		})
	}
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}
		if _, ok := s.allowlist[origin]; !ok {
			s.writeError(w, http.StatusForbidden, ErrorResponse{
				ErrorCode:   "origin_not_allowed",
				UserMessage: "The request origin is not allowlisted for this auth-service deployment.",
			})
			return
		}

		headers := w.Header()
		headers.Set("Access-Control-Allow-Origin", origin)
		headers.Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
		headers.Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Service-Timestamp, X-Service-Nonce, X-Service-Body-SHA256, X-Auth-Tenant-Id, X-Auth-Environment, X-Auth-Subject-Id, X-Auth-Connection-Id, X-Request-Id")
		headers.Add("Vary", "Origin")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requireServiceAuth(w http.ResponseWriter, r *http.Request, requireSubject, requireConnection bool) (ServiceScope, []byte, bool) {
	body, err := readAndResetBody(r)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_body",
			UserMessage: "could not read request body",
		})
		return ServiceScope{}, nil, false
	}
	scope, err := verifyServiceRequest(
		r.Context(),
		s,
		s.deps.Store,
		s.deps.Now().UTC(),
		r,
		body,
		s.cfg.TenantID,
		s.cfg.Environment,
	)
	if err != nil {
		code := http.StatusUnauthorized
		errorCode := "service_unauthorized"
		if errors.Is(err, ErrServiceReplay) {
			errorCode = "service_replay"
		}
		s.writeError(w, code, ErrorResponse{
			ErrorCode:   errorCode,
			UserMessage: "service authentication failed",
		})
		return ServiceScope{}, nil, false
	}
	if requireSubject && scope.SubjectID == "" {
		s.writeError(w, http.StatusUnauthorized, ErrorResponse{
			ErrorCode:   "missing_subject_id",
			UserMessage: "service-auth subject_id is required",
		})
		return ServiceScope{}, nil, false
	}
	if requireConnection && scope.ConnectionID == "" {
		s.writeError(w, http.StatusUnauthorized, ErrorResponse{
			ErrorCode:   "missing_connection_id",
			UserMessage: "service-auth connection_id is required",
		})
		return ServiceScope{}, nil, false
	}
	return scope, body, true
}

func (s *Server) lookupServiceSecret(keyID string) (string, bool) {
	switch keyID {
	case s.cfg.CurrentKeyID:
		return s.cfg.CurrentSharedSecret, true
	case s.cfg.NextKeyID:
		if s.cfg.NextSharedSecret != "" {
			return s.cfg.NextSharedSecret, true
		}
	}
	return "", false
}

func (s *Server) writeStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		s.writeError(w, http.StatusNotFound, ErrorResponse{
			ErrorCode:   "not_found",
			UserMessage: "connection not found",
		})
	case errors.Is(err, ErrConflict):
		s.writeError(w, http.StatusConflict, ErrorResponse{
			ErrorCode:   "version_conflict",
			UserMessage: "connection metadata changed; reload and retry the mutation",
		})
	case errors.Is(err, ErrInvalidMetadata):
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   "invalid_metadata",
			UserMessage: strings.TrimPrefix(err.Error(), ErrInvalidMetadata.Error()+": "),
		})
	case errors.Is(err, ErrLastWasteland):
		s.writeError(w, http.StatusConflict, ErrorResponse{
			ErrorCode:   "last_wasteland",
			UserMessage: "cannot remove the last wasteland from a connection",
		})
	case errors.Is(err, ErrWastelandNotFound):
		s.writeError(w, http.StatusNotFound, ErrorResponse{
			ErrorCode:   "wasteland_not_found",
			UserMessage: "wasteland not found on this connection",
		})
	default:
		s.writeError(w, http.StatusInternalServerError, ErrorResponse{
			ErrorCode:   "store_failed",
			UserMessage: "the auth service could not complete the request",
			Retryable:   true,
		})
	}
}

func (s *Server) writeError(w http.ResponseWriter, status int, resp ErrorResponse) {
	w.Header().Set("X-Wasteland-Auth-Error-Code", resp.ErrorCode)
	writeJSON(w, status, resp)
}

func (s *Server) writeValidationError(w http.ResponseWriter, validationErr *ValidationError) {
	switch validationErr.Code {
	case ValidationInvalidKey, ValidationExpiredKey, ValidationRevokedKey, "":
		s.writeError(w, http.StatusBadRequest, ErrorResponse{
			ErrorCode:   string(ValidationInvalidKey),
			UserMessage: "DoltHub rejected the API key. Verify you created an API token with the required permissions.",
		})
	case ValidationRateLimited:
		s.writeError(w, http.StatusServiceUnavailable, ErrorResponse{
			ErrorCode:   string(validationErr.Code),
			UserMessage: "DoltHub is rate limiting token validation. Try again in a moment.",
			Retryable:   true,
		})
	case ValidationUpstreamUnreachable, ValidationKMSUnavailable, ValidationProxyUnauthorized:
		s.writeError(w, http.StatusServiceUnavailable, ErrorResponse{
			ErrorCode:   string(validationErr.Code),
			UserMessage: "DoltHub is temporarily unreachable. Try again in a moment.",
			Retryable:   true,
		})
	default:
		s.writeError(w, http.StatusInternalServerError, ErrorResponse{
			ErrorCode:   "redeem_failed",
			UserMessage: "The auth service could not store the DoltHub credential.",
			Retryable:   true,
		})
	}
}

func validateDoltHubAPIKey(ctx context.Context, apiKey string) (ValidationErrorCode, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicProbeURL, nil)
	if err != nil {
		return ValidationUpstreamUnreachable, err
	}
	req.Header.Set("Authorization", "token "+apiKey)

	resp, err := (&http.Client{Timeout: validationProbeTimeout}).Do(req)
	if err != nil {
		return ValidationUpstreamUnreachable, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		return "", nil
	case http.StatusTooManyRequests:
		return ValidationRateLimited, fmt.Errorf("DoltHub rate limited the validation probe")
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusBadRequest:
		if strings.Contains(strings.ToLower(string(body)), "invalid authorization") || resp.StatusCode != http.StatusBadRequest {
			return ValidationInvalidKey, fmt.Errorf("DoltHub rejected the API key")
		}
		return ValidationInvalidKey, fmt.Errorf("DoltHub rejected the API key")
	default:
		return ValidationUpstreamUnreachable, fmt.Errorf("DoltHub returned HTTP %d", resp.StatusCode)
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for key, values := range src {
		switch http.CanonicalHeaderKey(key) {
		case "Connection", "Proxy-Connection", "Keep-Alive", "Te", "Trailer", "Transfer-Encoding", "Upgrade":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
