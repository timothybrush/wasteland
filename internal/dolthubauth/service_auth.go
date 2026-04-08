package dolthubauth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	headerAuthorization    = "Authorization"
	headerServiceTimestamp = "X-Service-Timestamp"
	headerServiceNonce     = "X-Service-Nonce"
	headerServiceBodySHA   = "X-Service-Body-SHA256"
	headerAuthTenantID     = "X-Auth-Tenant-Id"
	headerAuthEnvironment  = "X-Auth-Environment"
	headerAuthSubjectID    = "X-Auth-Subject-Id"
	headerAuthConnectionID = "X-Auth-Connection-Id"
	headerRequestID        = "X-Request-Id"

	serviceAuthPrefix = "Wasteland-HMAC "
	maxClockSkew      = 5 * time.Minute
)

type ServiceScope struct {
	KeyID        string
	TenantID     string
	Environment  string
	SubjectID    string
	ConnectionID string
	RequestID    string
}

type NonceRecorder interface {
	UseServiceNonce(context.Context, string, string, time.Time, time.Time) error
}

type serviceKeyLookup interface {
	lookupServiceSecret(string) (string, bool)
}

func serviceAuthBaseString(
	keyID, timestamp, nonce, method, requestURI, bodyHash, tenantID, environment, subjectID, connectionID string,
) string {
	return strings.Join([]string{
		keyID,
		timestamp,
		nonce,
		method,
		requestURI,
		bodyHash,
		tenantID,
		environment,
		subjectID,
		connectionID,
	}, "\n")
}

func signServiceRequest(
	secret, keyID string,
	at time.Time,
	nonce, method, requestURI string,
	body []byte,
	tenantID, environment, subjectID, connectionID string,
) (timestamp string, signature string) {
	timestamp = at.UTC().Format(time.RFC3339)
	base := serviceAuthBaseString(
		keyID,
		timestamp,
		nonce,
		method,
		requestURI,
		bodySHA256(body),
		tenantID,
		environment,
		subjectID,
		connectionID,
	)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base))
	return timestamp, hex.EncodeToString(mac.Sum(nil))
}

func parseServiceAuthorization(raw string) (string, string, bool) {
	if !strings.HasPrefix(raw, serviceAuthPrefix) {
		return "", "", false
	}
	rest := strings.TrimPrefix(raw, serviceAuthPrefix)
	keyID, signature, ok := strings.Cut(rest, ":")
	if !ok || keyID == "" || signature == "" {
		return "", "", false
	}
	return keyID, signature, true
}

func verifyServiceRequest(
	ctx context.Context,
	keys serviceKeyLookup,
	nonces NonceRecorder,
	now time.Time,
	r *http.Request,
	body []byte,
	expectedTenantID, expectedEnvironment string,
) (ServiceScope, error) {
	keyID, signature, ok := parseServiceAuthorization(r.Header.Get(headerAuthorization))
	if !ok {
		return ServiceScope{}, ErrServiceUnauthorized
	}

	secret, ok := keys.lookupServiceSecret(keyID)
	if !ok {
		return ServiceScope{}, ErrServiceUnauthorized
	}

	timestampRaw := strings.TrimSpace(r.Header.Get(headerServiceTimestamp))
	timestamp, err := time.Parse(time.RFC3339, timestampRaw)
	if err != nil {
		return ServiceScope{}, ErrServiceUnauthorized
	}
	if delta := now.Sub(timestamp); delta > maxClockSkew || delta < -maxClockSkew {
		return ServiceScope{}, ErrServiceUnauthorized
	}

	nonce := strings.TrimSpace(r.Header.Get(headerServiceNonce))
	if nonce == "" {
		return ServiceScope{}, ErrServiceUnauthorized
	}
	if got := bodySHA256(body); got != strings.TrimSpace(r.Header.Get(headerServiceBodySHA)) {
		return ServiceScope{}, ErrServiceUnauthorized
	}

	scope := ServiceScope{
		KeyID:        keyID,
		TenantID:     strings.TrimSpace(r.Header.Get(headerAuthTenantID)),
		Environment:  strings.TrimSpace(r.Header.Get(headerAuthEnvironment)),
		SubjectID:    strings.TrimSpace(r.Header.Get(headerAuthSubjectID)),
		ConnectionID: strings.TrimSpace(r.Header.Get(headerAuthConnectionID)),
		RequestID:    strings.TrimSpace(r.Header.Get(headerRequestID)),
	}
	if scope.TenantID != expectedTenantID || scope.Environment != expectedEnvironment {
		return ServiceScope{}, ErrServiceUnauthorized
	}

	base := serviceAuthBaseString(
		keyID,
		timestampRaw,
		nonce,
		r.Method,
		r.URL.RequestURI(),
		bodySHA256(body),
		scope.TenantID,
		scope.Environment,
		scope.SubjectID,
		scope.ConnectionID,
	)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(base))
	expectedSig := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expectedSig)) {
		return ServiceScope{}, ErrServiceUnauthorized
	}
	if err := nonces.UseServiceNonce(ctx, keyID, nonce, now, timestamp.Add(maxClockSkew)); err != nil {
		if errors.Is(err, ErrConflict) {
			return ServiceScope{}, ErrServiceReplay
		}
		return ServiceScope{}, err
	}
	return scope, nil
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
