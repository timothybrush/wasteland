package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gastownhall/wasteland/internal/observability"
	"go.opentelemetry.io/otel"
)

var nangoTracer = otel.Tracer("github.com/gastownhall/wasteland/internal/hosted/nango")

// NangoConfig holds configuration for the Nango integration.
type NangoConfig struct {
	BaseURL       string // default "https://api.nango.dev"
	SecretKey     string // server-side only
	IntegrationID string // default "dolthub"
}

// WastelandConfig describes a single joined wasteland in Nango metadata.
type WastelandConfig struct {
	Upstream string `json:"upstream"`
	ForkOrg  string `json:"fork_org"`
	ForkDB   string `json:"fork_db"`
	Mode     string `json:"mode"`    // "wild-west" or "pr"
	Signing  bool   `json:"signing"` // GPG-signed dolt commits
}

// UserMetadata is the persistent user config stored as Nango connection metadata.
type UserMetadata struct {
	RigHandle  string            `json:"rig_handle"`
	Wastelands []WastelandConfig `json:"wastelands"`
}

// FindWasteland returns the config for the given upstream, or nil if not found.
func (m *UserMetadata) FindWasteland(upstream string) *WastelandConfig {
	for i := range m.Wastelands {
		if m.Wastelands[i].Upstream == upstream {
			return &m.Wastelands[i]
		}
	}
	return nil
}

// UpsertWasteland adds or updates a wasteland entry.
func (m *UserMetadata) UpsertWasteland(wl WastelandConfig) {
	for i := range m.Wastelands {
		if m.Wastelands[i].Upstream == wl.Upstream {
			m.Wastelands[i] = wl
			return
		}
	}
	m.Wastelands = append(m.Wastelands, wl)
}

// RemoveWasteland removes the wasteland with the given upstream.
// Returns false if the upstream was not found.
func (m *UserMetadata) RemoveWasteland(upstream string) bool {
	for i := range m.Wastelands {
		if m.Wastelands[i].Upstream == upstream {
			m.Wastelands = append(m.Wastelands[:i], m.Wastelands[i+1:]...)
			return true
		}
	}
	return false
}

// UserConfig is the legacy flat metadata format. Kept for backward compatibility
// parsing only — new writes always use UserMetadata.
type UserConfig struct {
	RigHandle string `json:"rig_handle"`
	ForkOrg   string `json:"fork_org"`
	ForkDB    string `json:"fork_db"`
	Upstream  string `json:"upstream"`
	Mode      string `json:"mode"`
	Signing   bool   `json:"signing"`
}

// parseMetadata reads Nango metadata JSON and returns a UserMetadata.
// It handles both the new multi-wasteland format and the legacy flat format.
func parseMetadata(raw json.RawMessage) *UserMetadata {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}

	// Try new format first (has "wastelands" array).
	var meta UserMetadata
	if err := json.Unmarshal(raw, &meta); err == nil && len(meta.Wastelands) > 0 {
		return &meta
	}

	// Try legacy flat format (has top-level "upstream" field).
	var legacy UserConfig
	if err := json.Unmarshal(raw, &legacy); err == nil && legacy.Upstream != "" {
		return &UserMetadata{
			RigHandle: legacy.RigHandle,
			Wastelands: []WastelandConfig{
				{
					Upstream: legacy.Upstream,
					ForkOrg:  legacy.ForkOrg,
					ForkDB:   legacy.ForkDB,
					Mode:     legacy.Mode,
					Signing:  legacy.Signing,
				},
			},
		}
	}

	return nil
}

// NangoClient talks to the Nango REST API.
type NangoClient struct {
	baseURL       string
	secretKey     string
	integrationID string
	client        *http.Client
}

// NewNangoClient creates a NangoClient from the given config.
func NewNangoClient(cfg NangoConfig) *NangoClient {
	base := cfg.BaseURL
	if base == "" {
		base = "https://api.nango.dev"
	}
	integrationID := cfg.IntegrationID
	if integrationID == "" {
		integrationID = "dolthub"
	}
	return &NangoClient{
		baseURL:       base,
		secretKey:     cfg.SecretKey,
		integrationID: integrationID,
		client: &http.Client{
			Timeout:   30 * time.Second,
			Transport: observability.NewTransport(nil),
		},
	}
}

// IntegrationID returns the configured Nango integration ID.
func (n *NangoClient) IntegrationID() string { return n.integrationID }

// BaseURL returns the Nango API base URL.
func (n *NangoClient) BaseURL() string { return n.baseURL }

// SecretKey returns the Nango server-side secret key.
func (n *NangoClient) SecretKey() string { return n.secretKey }

// nangoConnectionResponse is the JSON shape returned by GET /connection/{id}.
type nangoConnectionResponse struct {
	ConnectionID string `json:"connection_id"`
	Credentials  struct {
		APIKey string `json:"apiKey"`
	} `json:"credentials"`
	Metadata json.RawMessage `json:"metadata"`
}

// GetConnection fetches the stored token and metadata for a Nango connection.
func (n *NangoClient) GetConnection(connectionID string) (string, *UserMetadata, error) {
	return n.GetConnectionContext(context.Background(), connectionID)
}

// GetConnectionContext fetches the stored token and metadata for a Nango connection.
func (n *NangoClient) GetConnectionContext(ctx context.Context, connectionID string) (string, *UserMetadata, error) {
	ctx, span := nangoTracer.Start(ctx, "nango.get_connection")
	defer span.End()

	u := fmt.Sprintf("%s/connection/%s?provider_config_key=%s",
		n.baseURL, url.PathEscape(connectionID), url.QueryEscape(n.integrationID))

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		span.RecordError(err)
		return "", nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+n.secretKey)

	resp, err := n.client.Do(req)
	if err != nil {
		span.RecordError(err)
		return "", nil, fmt.Errorf("nango request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if resp.StatusCode != http.StatusOK {
		body, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			body = []byte("(could not read body)")
		}
		span.RecordError(fmt.Errorf("nango returned %d", resp.StatusCode))
		return "", nil, fmt.Errorf("nango returned %d: %s", resp.StatusCode, string(body))
	}

	var connResp nangoConnectionResponse
	if err := json.NewDecoder(resp.Body).Decode(&connResp); err != nil {
		span.RecordError(err)
		return "", nil, fmt.Errorf("decoding nango response: %w", err)
	}

	apiKey := connResp.Credentials.APIKey
	meta := parseMetadata(connResp.Metadata)

	return apiKey, meta, nil
}

// connectSessionRequest is the JSON body for POST /connect/sessions.
type connectSessionAPIRequest struct {
	EndUser             connectSessionEndUser `json:"end_user"`
	AllowedIntegrations []string              `json:"allowed_integrations"`
}

type connectSessionEndUser struct {
	ID string `json:"id"`
}

// connectSessionAPIResponse is the JSON shape returned by POST /connect/sessions.
type connectSessionAPIResponse struct {
	Data struct {
		Token string `json:"token"`
	} `json:"data"`
}

// CreateConnectSession creates a short-lived connect session token for the frontend SDK.
// Retries once on transient errors (timeouts, 5xx).
func (n *NangoClient) CreateConnectSession(endUserID string) (string, error) {
	return n.CreateConnectSessionContext(context.Background(), endUserID)
}

// CreateConnectSessionContext creates a short-lived connect session token with request context.
func (n *NangoClient) CreateConnectSessionContext(ctx context.Context, endUserID string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			time.Sleep(2 * time.Second)
		}
		token, err := n.doCreateConnectSessionContext(ctx, endUserID)
		if err == nil {
			return token, nil
		}
		lastErr = err
		// Only retry on timeouts or 5xx — not on 4xx client errors.
		if !isRetryable(err) {
			return "", err
		}
		slog.Warn("nango connect session failed, retrying", "attempt", attempt+1, "error", err)
	}
	return "", fmt.Errorf("nango is not responding — please try again in a moment: %w", lastErr)
}

func (n *NangoClient) doCreateConnectSessionContext(ctx context.Context, endUserID string) (string, error) {
	ctx, span := nangoTracer.Start(ctx, "nango.create_connect_session")
	defer span.End()

	u := fmt.Sprintf("%s/connect/sessions", n.baseURL)

	body, err := json.Marshal(connectSessionAPIRequest{
		EndUser:             connectSessionEndUser{ID: endUserID},
		AllowedIntegrations: []string{n.integrationID},
	})
	if err != nil {
		return "", fmt.Errorf("marshaling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(body))
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+n.secretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("nango request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if resp.StatusCode >= 500 {
		respBody, _ := io.ReadAll(resp.Body)
		span.RecordError(fmt.Errorf("nango returned %d", resp.StatusCode))
		return "", &retryableError{msg: fmt.Sprintf("nango returned %d: %s", resp.StatusCode, string(respBody))}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			respBody = []byte("(could not read body)")
		}
		span.RecordError(fmt.Errorf("nango returned %d", resp.StatusCode))
		return "", fmt.Errorf("nango returned %d: %s", resp.StatusCode, string(respBody))
	}

	var sessionResp connectSessionAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&sessionResp); err != nil {
		span.RecordError(err)
		return "", fmt.Errorf("decoding nango response: %w", err)
	}

	return sessionResp.Data.Token, nil
}

// retryableError wraps an error that should be retried.
type retryableError struct{ msg string }

func (e *retryableError) Error() string { return e.msg }

// isRetryable returns true for timeout errors and retryableError.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var re *retryableError
	if errors.As(err, &re) {
		return true
	}
	// Timeout errors (context deadline exceeded, client timeout).
	return strings.Contains(err.Error(), "deadline exceeded") ||
		strings.Contains(err.Error(), "Timeout exceeded")
}

// SetMetadata writes/updates the persistent user metadata on the Nango connection.
func (n *NangoClient) SetMetadata(connectionID string, meta *UserMetadata) error {
	return n.SetMetadataContext(context.Background(), connectionID, meta)
}

// SetMetadataContext writes/updates the persistent user metadata with request context.
func (n *NangoClient) SetMetadataContext(ctx context.Context, connectionID string, meta *UserMetadata) error {
	ctx, span := nangoTracer.Start(ctx, "nango.set_metadata")
	defer span.End()

	u := fmt.Sprintf("%s/connection/%s/metadata",
		n.baseURL, url.PathEscape(connectionID))

	body, err := json.Marshal(meta)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("marshaling metadata: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "PATCH", u, bytes.NewReader(body))
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+n.secretKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Provider-Config-Key", n.integrationID)

	resp, err := n.client.Do(req)
	if err != nil {
		span.RecordError(err)
		return fmt.Errorf("nango request failed: %w", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		respBody, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			respBody = []byte("(could not read body)")
		}
		span.RecordError(fmt.Errorf("nango returned %d", resp.StatusCode))
		return fmt.Errorf("nango returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
