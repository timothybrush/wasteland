package dolthubauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeServiceKeys map[string]string

func (f fakeServiceKeys) lookupServiceSecret(keyID string) (string, bool) {
	secret, ok := f[keyID]
	return secret, ok
}

func TestVerifyServiceRequest_AcceptsEquivalentQueryEncodings(t *testing.T) {
	now := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	clientReq := httptest.NewRequest(
		http.MethodGet,
		"/v1/proxy/api/hop/wl-commons/main?q=SELECT+id%2C+status%2C+COALESCE%28claimed_by%2C+%27%27%29+as+claimed_by+FROM+wanted",
		nil,
	)
	serverReq := httptest.NewRequest(
		http.MethodGet,
		"/v1/proxy/api/hop/wl-commons/main?q=SELECT+id,+status,+COALESCE(claimed_by,+'')+as+claimed_by+FROM+wanted",
		nil,
	)

	nonce := "nonce-1"
	timestamp, signature := signServiceRequest(
		"current-secret",
		"current-key",
		now,
		nonce,
		http.MethodGet,
		serviceAuthRequestTarget(clientReq.URL),
		nil,
		"tenant-dev",
		"dev",
		"subject-1",
		"conn-1",
	)
	serverReq.Header.Set(headerAuthorization, serviceAuthPrefix+"current-key:"+signature)
	serverReq.Header.Set(headerServiceTimestamp, timestamp)
	serverReq.Header.Set(headerServiceNonce, nonce)
	serverReq.Header.Set(headerServiceBodySHA, bodySHA256(nil))
	serverReq.Header.Set(headerAuthTenantID, "tenant-dev")
	serverReq.Header.Set(headerAuthEnvironment, "dev")
	serverReq.Header.Set(headerAuthSubjectID, "subject-1")
	serverReq.Header.Set(headerAuthConnectionID, "conn-1")

	scope, err := verifyServiceRequest(
		context.Background(),
		fakeServiceKeys{"current-key": "current-secret"},
		fakeStore{},
		now,
		serverReq,
		nil,
		"tenant-dev",
		"dev",
	)
	if err != nil {
		t.Fatalf("verifyServiceRequest() error = %v", err)
	}
	if scope.SubjectID != "subject-1" || scope.ConnectionID != "conn-1" {
		t.Fatalf("scope = %+v", scope)
	}
}
