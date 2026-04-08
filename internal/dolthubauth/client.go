package dolthubauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gastownhall/wasteland/internal/observability"
)

type ClientConfig struct {
	BaseURL      string
	TenantID     string
	Environment  string
	KeyID        string
	SharedSecret string
	Now          func() time.Time
	HTTPClient   *http.Client
}

type Client struct {
	baseURL      string
	tenantID     string
	environment  string
	keyID        string
	sharedSecret string
	now          func() time.Time
	httpClient   *http.Client
}

func NewClient(cfg ClientConfig) *Client {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:      strings.TrimRight(cfg.BaseURL, "/"),
		tenantID:     cfg.TenantID,
		environment:  cfg.Environment,
		keyID:        cfg.KeyID,
		sharedSecret: cfg.SharedSecret,
		now:          now,
		httpClient:   observability.WrapClient(httpClient),
	}
}

func (c *Client) BaseURL() string {
	return c.baseURL
}

type RequestError struct {
	Status      int
	ErrorCode   string
	UserMessage string
	Retryable   bool
}

func (e *RequestError) Error() string {
	if e.UserMessage != "" {
		return e.UserMessage
	}
	if e.ErrorCode != "" {
		return e.ErrorCode
	}
	return fmt.Sprintf("auth service returned HTTP %d", e.Status)
}

func (c *Client) CreateConnectToken(ctx context.Context, subjectID string, metadata UserMetadata, ttl time.Duration) (*CreateConnectTokenResponse, error) {
	body, err := json.Marshal(CreateConnectTokenRequest{
		SubjectID:  subjectID,
		Metadata:   metadata,
		TTLSeconds: int(ttl / time.Second),
	})
	if err != nil {
		return nil, fmt.Errorf("marshal connect token request: %w", err)
	}
	var resp CreateConnectTokenResponse
	if err := c.doServiceJSON(ctx, http.MethodPost, "/v1/connect-tokens", body, subjectID, "", &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetConnection(ctx context.Context, subjectID, connectionID string) (*ConnectionResponse, error) {
	var resp ConnectionResponse
	path := fmt.Sprintf("/v1/connections/%s", connectionID)
	if err := c.doServiceJSON(ctx, http.MethodGet, path, nil, subjectID, connectionID, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) PatchRigHandle(ctx context.Context, subjectID, connectionID, rigHandle string, recordVersion int) (*ConnectionResponse, error) {
	body, err := json.Marshal(RigHandlePatchRequest{
		RecordVersion: recordVersion,
		RigHandle:     rigHandle,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal rig-handle patch: %w", err)
	}
	var resp ConnectionResponse
	path := fmt.Sprintf("/v1/connections/%s/rig-handle", connectionID)
	if err := c.doServiceJSON(ctx, http.MethodPatch, path, body, subjectID, connectionID, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpsertWasteland(ctx context.Context, subjectID, connectionID string, recordVersion int, wasteland WastelandConfig) (*ConnectionResponse, error) {
	body, err := json.Marshal(WastelandUpsertRequest{
		RecordVersion: recordVersion,
		Wasteland:     wasteland,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal wasteland upsert: %w", err)
	}
	var resp ConnectionResponse
	path := fmt.Sprintf("/v1/connections/%s/wastelands/%s", connectionID, wasteland.Upstream)
	if err := c.doServiceJSON(ctx, http.MethodPut, path, body, subjectID, connectionID, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteWasteland(ctx context.Context, subjectID, connectionID, upstream string, recordVersion int) (*ConnectionResponse, error) {
	body, err := json.Marshal(struct {
		RecordVersion int `json:"record_version"`
	}{RecordVersion: recordVersion})
	if err != nil {
		return nil, fmt.Errorf("marshal wasteland delete: %w", err)
	}
	var resp ConnectionResponse
	path := fmt.Sprintf("/v1/connections/%s/wastelands/%s", connectionID, upstream)
	if err := c.doServiceJSON(ctx, http.MethodDelete, path, body, subjectID, connectionID, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) PatchWastelandSettings(
	ctx context.Context,
	subjectID, connectionID, upstream string,
	recordVersion int,
	mode string,
	signing bool,
) (*ConnectionResponse, error) {
	body, err := json.Marshal(WastelandSettingsPatchRequest{
		RecordVersion: recordVersion,
		Mode:          mode,
		Signing:       signing,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal settings patch: %w", err)
	}
	var resp ConnectionResponse
	path := fmt.Sprintf("/v1/connections/%s/wasteland-settings/%s", connectionID, upstream)
	if err := c.doServiceJSON(ctx, http.MethodPatch, path, body, subjectID, connectionID, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) NewProxyHTTPClient(subjectID, connectionID string) *http.Client {
	return observability.WrapClient(&http.Client{
		Transport: &ProxyTransport{
			baseURL:      c.baseURL,
			tenantID:     c.tenantID,
			environment:  c.environment,
			keyID:        c.keyID,
			sharedSecret: c.sharedSecret,
			now:          c.now,
			subjectID:    subjectID,
			connectionID: connectionID,
			inner:        http.DefaultTransport,
		},
		Timeout: 60 * time.Second,
	})
}

type ProxyTransport struct {
	baseURL      string
	tenantID     string
	environment  string
	keyID        string
	sharedSecret string
	now          func() time.Time
	subjectID    string
	connectionID string
	inner        http.RoundTripper
	nonceFn      func(int) (string, error)
}

func (t *ProxyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, err := readAndResetBody(req)
	if err != nil {
		return nil, err
	}
	targetURL, ok := proxyTargetURL(t.baseURL, req.URL.String())
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProxyTarget, req.URL.String())
	}

	proxyReq, err := http.NewRequestWithContext(req.Context(), req.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	copyProxyHeaders(proxyReq.Header, req.Header)
	proxyReq.Header.Del("Authorization")
	proxyReq.Header.Del("Host")
	if err := t.sign(proxyReq, body); err != nil {
		return nil, err
	}
	return t.transport().RoundTrip(proxyReq)
}

func (t *ProxyTransport) transport() http.RoundTripper {
	if t.inner != nil {
		return t.inner
	}
	return http.DefaultTransport
}

func (t *ProxyTransport) sign(req *http.Request, body []byte) error {
	nonceFn := t.nonceFn
	if nonceFn == nil {
		nonceFn = randomHex
	}
	nonce, err := nonceFn(16)
	if err != nil {
		return fmt.Errorf("generate proxy nonce: %w", err)
	}
	timestamp, signature := signServiceRequest(
		t.sharedSecret,
		t.keyID,
		t.now().UTC(),
		nonce,
		req.Method,
		req.URL.RequestURI(),
		body,
		t.tenantID,
		t.environment,
		t.subjectID,
		t.connectionID,
	)
	req.Header.Set(headerAuthorization, serviceAuthPrefix+t.keyID+":"+signature)
	req.Header.Set(headerServiceTimestamp, timestamp)
	req.Header.Set(headerServiceNonce, nonce)
	req.Header.Set(headerServiceBodySHA, bodySHA256(body))
	req.Header.Set(headerAuthTenantID, t.tenantID)
	req.Header.Set(headerAuthEnvironment, t.environment)
	req.Header.Set(headerAuthSubjectID, t.subjectID)
	req.Header.Set(headerAuthConnectionID, t.connectionID)
	return nil
}

func (c *Client) doServiceJSON(
	ctx context.Context,
	method, path string,
	body []byte,
	subjectID, connectionID string,
	out any,
) error {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create auth-service request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	nonce, err := randomHex(16)
	if err != nil {
		return err
	}
	timestamp, signature := signServiceRequest(
		c.sharedSecret,
		c.keyID,
		c.now().UTC(),
		nonce,
		method,
		req.URL.RequestURI(),
		body,
		c.tenantID,
		c.environment,
		subjectID,
		connectionID,
	)
	req.Header.Set(headerAuthorization, serviceAuthPrefix+c.keyID+":"+signature)
	req.Header.Set(headerServiceTimestamp, timestamp)
	req.Header.Set(headerServiceNonce, nonce)
	req.Header.Set(headerServiceBodySHA, bodySHA256(body))
	req.Header.Set(headerAuthTenantID, c.tenantID)
	req.Header.Set(headerAuthEnvironment, c.environment)
	req.Header.Set(headerAuthSubjectID, subjectID)
	if connectionID != "" {
		req.Header.Set(headerAuthConnectionID, connectionID)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("auth-service request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read auth-service response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var failure ErrorResponse
		_ = json.Unmarshal(respBody, &failure)
		return &RequestError{
			Status:      resp.StatusCode,
			ErrorCode:   failure.ErrorCode,
			UserMessage: failure.UserMessage,
			Retryable:   failure.Retryable,
		}
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("decode auth-service response: %w", err)
	}
	return nil
}

func proxyTargetURL(baseURL, rawURL string) (string, bool) {
	switch {
	case strings.HasPrefix(rawURL, "https://www.dolthub.com/api/v1alpha1/"):
		suffix := strings.TrimPrefix(rawURL, "https://www.dolthub.com/api/v1alpha1")
		return baseURL + "/v1/proxy/api" + suffix, true
	case rawURL == "https://www.dolthub.com/graphql":
		return baseURL + "/v1/proxy/graphql", true
	default:
		return "", false
	}
}

func readAndResetBody(req *http.Request) ([]byte, error) {
	if req.Body == nil {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("read request body: %w", err)
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func copyProxyHeaders(dst, src http.Header) {
	for key, values := range src {
		switch http.CanonicalHeaderKey(key) {
		case "Authorization",
			"Connection",
			"Proxy-Connection",
			"Keep-Alive",
			"Te",
			"Trailer",
			"Transfer-Encoding",
			"Upgrade",
			"Host",
			http.CanonicalHeaderKey(headerServiceTimestamp),
			http.CanonicalHeaderKey(headerServiceNonce),
			http.CanonicalHeaderKey(headerServiceBodySHA),
			http.CanonicalHeaderKey(headerAuthTenantID),
			http.CanonicalHeaderKey(headerAuthEnvironment),
			http.CanonicalHeaderKey(headerAuthSubjectID),
			http.CanonicalHeaderKey(headerAuthConnectionID):
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}
