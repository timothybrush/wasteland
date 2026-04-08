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

func (m *UserMetadata) FindWasteland(upstream string) *WastelandConfig {
	for i := range m.Wastelands {
		if m.Wastelands[i].Upstream == upstream {
			return &m.Wastelands[i]
		}
	}
	return nil
}

func (m *UserMetadata) UpsertWasteland(wl WastelandConfig) {
	for i := range m.Wastelands {
		if m.Wastelands[i].Upstream == wl.Upstream {
			m.Wastelands[i] = wl
			return
		}
	}
	m.Wastelands = append(m.Wastelands, wl)
}

func (m *UserMetadata) RemoveWasteland(upstream string) bool {
	for i := range m.Wastelands {
		if m.Wastelands[i].Upstream == upstream {
			m.Wastelands = append(m.Wastelands[:i], m.Wastelands[i+1:]...)
			return true
		}
	}
	return false
}

type ConnectionStatus string

const (
	StatusActive   ConnectionStatus = "active"
	StatusInvalid  ConnectionStatus = "invalid"
	StatusDegraded ConnectionStatus = "degraded"
)

type ValidationErrorCode string

const (
	ValidationInvalidKey          ValidationErrorCode = "invalid_key"
	ValidationExpiredKey          ValidationErrorCode = "expired_key"
	ValidationRevokedKey          ValidationErrorCode = "revoked_key"
	ValidationUpstreamUnreachable ValidationErrorCode = "upstream_unreachable"
	ValidationRateLimited         ValidationErrorCode = "rate_limited"
	ValidationKMSUnavailable      ValidationErrorCode = "kms_unavailable"
	ValidationProxyUnauthorized   ValidationErrorCode = "proxy_unauthorized"
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

type CreateConnectTokenRequest struct {
	SubjectID  string       `json:"subject_id"`
	Metadata   UserMetadata `json:"metadata"`
	TTLSeconds int          `json:"ttl_seconds,omitempty"`
}

type CreateConnectTokenResponse struct {
	ConnectToken string       `json:"connect_token"`
	RedeemSecret string       `json:"redeem_secret"`
	Metadata     UserMetadata `json:"metadata"`
	ExpiresAt    time.Time    `json:"expires_at"`
}

type RedeemConnectTokenRequest struct {
	ConnectToken string       `json:"connect_token"`
	RedeemSecret string       `json:"redeem_secret"`
	APIKey       string       `json:"api_key"`
	Metadata     UserMetadata `json:"metadata"`
}

type RedeemConnectTokenResponse struct {
	ConnectionID    string           `json:"connection_id"`
	Status          ConnectionStatus `json:"status"`
	LastValidatedAt *time.Time       `json:"last_validated_at,omitempty"`
}

type ErrorResponse struct {
	ErrorCode   string `json:"error_code,omitempty"`
	Error       string `json:"error,omitempty"`
	UserMessage string `json:"user_message,omitempty"`
	Retryable   bool   `json:"retryable,omitempty"`
	RequestID   string `json:"request_id,omitempty"`
}

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

type WastelandUpsertRequest struct {
	RecordVersion int             `json:"record_version"`
	Wasteland     WastelandConfig `json:"wasteland"`
}

type WastelandSettingsPatchRequest struct {
	RecordVersion int    `json:"record_version"`
	Mode          string `json:"mode"`
	Signing       bool   `json:"signing"`
}

type RigHandlePatchRequest struct {
	RecordVersion int    `json:"record_version"`
	RigHandle     string `json:"rig_handle"`
}
