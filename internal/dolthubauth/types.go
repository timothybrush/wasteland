package dolthubauth

import "time"

// WastelandConfig is the persisted Wasteland-specific per-upstream metadata.
type WastelandConfig struct {
	Upstream string `json:"upstream"`
	ForkOrg  string `json:"fork_org"`
	ForkDB   string `json:"fork_db"`
	Mode     string `json:"mode"`
	Signing  bool   `json:"signing"`
}

// UserMetadata is the Wasteland-specific metadata attached to a connection.
type UserMetadata struct {
	RigHandle  string            `json:"rig_handle"`
	Wastelands []WastelandConfig `json:"wastelands"`
}

// MergedWith returns a copy of the metadata with update values applied.
func (m UserMetadata) MergedWith(update UserMetadata) UserMetadata {
	merged := UserMetadata{
		RigHandle:  update.RigHandle,
		Wastelands: append([]WastelandConfig(nil), m.Wastelands...),
	}
	if merged.RigHandle == "" {
		merged.RigHandle = m.RigHandle
	}
	for _, wl := range update.Wastelands {
		merged.UpsertWasteland(wl)
	}
	return merged
}

// FindWasteland returns the matching Wasteland entry for the given upstream.
func (m *UserMetadata) FindWasteland(upstream string) *WastelandConfig {
	for i := range m.Wastelands {
		if m.Wastelands[i].Upstream == upstream {
			return &m.Wastelands[i]
		}
	}
	return nil
}

// UpsertWasteland adds or replaces a Wasteland entry by upstream.
func (m *UserMetadata) UpsertWasteland(wl WastelandConfig) {
	for i := range m.Wastelands {
		if m.Wastelands[i].Upstream == wl.Upstream {
			m.Wastelands[i] = wl
			return
		}
	}
	m.Wastelands = append(m.Wastelands, wl)
}

// RemoveWasteland removes the Wasteland entry for the given upstream.
func (m *UserMetadata) RemoveWasteland(upstream string) bool {
	for i := range m.Wastelands {
		if m.Wastelands[i].Upstream == upstream {
			m.Wastelands = append(m.Wastelands[:i], m.Wastelands[i+1:]...)
			return true
		}
	}
	return false
}

// ConnectionStatus describes the lifecycle state of a connection.
type ConnectionStatus string

const (
	// StatusActive means the connection is usable.
	StatusActive ConnectionStatus = "active"
	// StatusInvalid means the stored credential failed validation.
	StatusInvalid ConnectionStatus = "invalid"
	// StatusDegraded means the connection is usable but has a recent issue.
	StatusDegraded ConnectionStatus = "degraded"
)

// ValidationErrorCode classifies a credential validation failure.
type ValidationErrorCode string

const (
	// ValidationInvalidKey means DoltHub rejected the credential.
	ValidationInvalidKey ValidationErrorCode = "invalid_key"
	// ValidationExpiredKey means the credential has expired.
	ValidationExpiredKey ValidationErrorCode = "expired_key"
	// ValidationRevokedKey means the credential was revoked.
	ValidationRevokedKey ValidationErrorCode = "revoked_key"
	// ValidationUpstreamUnreachable means the upstream could not be reached.
	ValidationUpstreamUnreachable ValidationErrorCode = "upstream_unreachable"
	// ValidationRateLimited means the upstream asked us to back off.
	ValidationRateLimited ValidationErrorCode = "rate_limited"
	// ValidationKMSUnavailable means the encryption backend was unavailable.
	ValidationKMSUnavailable ValidationErrorCode = "kms_unavailable"
	// ValidationProxyUnauthorized means DoltHub rejected the proxy request.
	ValidationProxyUnauthorized ValidationErrorCode = "proxy_unauthorized"
)

// Connection is the stored, non-secret view of a DoltHub connection.
type Connection struct {
	ConnectionID            string              `json:"connection_id"`
	TenantID                string              `json:"tenant_id"`
	Environment             string              `json:"environment"`
	SubjectID               string              `json:"subject_id"`
	Metadata                UserMetadata        `json:"metadata"`
	Status                  ConnectionStatus    `json:"status"`
	CredentialVersion       int                 `json:"credential_version"`
	RecordVersion           int                 `json:"record_version"`
	LastValidatedAt         *time.Time          `json:"last_validated_at,omitempty"`
	LastValidationErrorCode ValidationErrorCode `json:"last_validation_error_code,omitempty"`
	LastProxyErrorAt        *time.Time          `json:"last_proxy_error_at,omitempty"`
	CreatedAt               time.Time           `json:"created_at"`
	UpdatedAt               time.Time           `json:"updated_at"`
}

// CreateConnectTokenRequest is the browser-facing request for a connect token.
type CreateConnectTokenRequest struct {
	SubjectID  string       `json:"subject_id"`
	Metadata   UserMetadata `json:"metadata"`
	TTLSeconds int          `json:"ttl_seconds,omitempty"`
}

// CreateConnectTokenResponse returns the opaque token pair and approved metadata.
type CreateConnectTokenResponse struct {
	ConnectToken string       `json:"connect_token"`
	RedeemSecret string       `json:"redeem_secret"`
	Metadata     UserMetadata `json:"metadata"`
	ExpiresAt    time.Time    `json:"expires_at"`
}

// RedeemConnectTokenRequest submits the browser-issued API key for storage.
type RedeemConnectTokenRequest struct {
	ConnectToken string       `json:"connect_token"`
	RedeemSecret string       `json:"redeem_secret"`
	APIKey       string       `json:"api_key"`
	Metadata     UserMetadata `json:"metadata"`
}

// RedeemConnectTokenResponse reports the new connection identity and status.
type RedeemConnectTokenResponse struct {
	ConnectionID    string           `json:"connection_id"`
	Status          ConnectionStatus `json:"status"`
	LastValidatedAt *time.Time       `json:"last_validated_at,omitempty"`
}

// ErrorResponse is the standard JSON error envelope returned by the service.
type ErrorResponse struct {
	ErrorCode   string `json:"error_code,omitempty"`
	Error       string `json:"error,omitempty"`
	UserMessage string `json:"user_message,omitempty"`
	Retryable   bool   `json:"retryable,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
}

// ConnectionResponse is the non-secret connection view returned to Wasteland.
type ConnectionResponse struct {
	ConnectionID            string              `json:"connection_id"`
	SubjectID               string              `json:"subject_id"`
	RigHandle               string              `json:"rig_handle"`
	Wastelands              []WastelandConfig   `json:"wastelands"`
	Status                  ConnectionStatus    `json:"status"`
	LastValidatedAt         *time.Time          `json:"last_validated_at,omitempty"`
	LastValidationErrorCode ValidationErrorCode `json:"last_validation_error_code,omitempty"`
	CreatedAt               time.Time           `json:"created_at"`
	UpdatedAt               time.Time           `json:"updated_at"`
	RecordVersion           int                 `json:"record_version"`
}

// NewConnectionResponse converts an internal connection into its public view.
func NewConnectionResponse(conn *Connection) ConnectionResponse {
	return ConnectionResponse{
		ConnectionID:            conn.ConnectionID,
		SubjectID:               conn.SubjectID,
		RigHandle:               conn.Metadata.RigHandle,
		Wastelands:              append([]WastelandConfig(nil), conn.Metadata.Wastelands...),
		Status:                  conn.Status,
		LastValidatedAt:         conn.LastValidatedAt,
		LastValidationErrorCode: conn.LastValidationErrorCode,
		CreatedAt:               conn.CreatedAt,
		UpdatedAt:               conn.UpdatedAt,
		RecordVersion:           conn.RecordVersion,
	}
}

// WastelandUpsertRequest adds or replaces one Wasteland entry.
type WastelandUpsertRequest struct {
	RecordVersion int             `json:"record_version"`
	Wasteland     WastelandConfig `json:"wasteland"`
}

// WastelandSettingsPatchRequest updates mutable Wasteland settings.
type WastelandSettingsPatchRequest struct {
	RecordVersion int    `json:"record_version"`
	Mode          string `json:"mode"`
	Signing       bool   `json:"signing"`
}

// RigHandlePatchRequest updates the stored rig handle.
type RigHandlePatchRequest struct {
	RecordVersion int    `json:"record_version"`
	RigHandle     string `json:"rig_handle"`
}
