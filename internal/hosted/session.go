// Package hosted provides multi-tenant hosted mode with external credential delegation.
package hosted

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// UserSession represents an authenticated browser session.
type UserSession struct {
	ID             string
	SubjectID      string
	ConnectionID   string // External auth-service connection ID (set after DoltHub connect)
	ActiveUpstream string
	CreatedAt      time.Time
}

// SessionStore is a thread-safe in-memory session store.
type SessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*UserSession
}

// NewSessionStore creates a new empty SessionStore with periodic cleanup.
func NewSessionStore() *SessionStore {
	s := &SessionStore{sessions: make(map[string]*UserSession)}
	go s.cleanup()
	return s
}

// cleanup periodically removes expired sessions to prevent unbounded memory growth.
func (s *SessionStore) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		for id, sess := range s.sessions {
			if time.Since(sess.CreatedAt) > sessionTTL {
				delete(s.sessions, id)
			}
		}
		s.mu.Unlock()
	}
}

// Create creates a new session with the given connection ID.
func (s *SessionStore) Create(connectionID string) (string, error) {
	return s.CreateWithSubject(connectionID, "")
}

// CreateWithSubject creates a new session with the given connection and subject.
func (s *SessionStore) CreateWithSubject(connectionID, subjectID string) (string, error) {
	id, err := generateSessionID()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[id] = &UserSession{
		ID:           id,
		SubjectID:    subjectID,
		ConnectionID: connectionID,
		CreatedAt:    time.Now(),
	}
	return id, nil
}

const (
	sessionTTL       = 24 * time.Hour
	subjectCookieTTL = 365 * 24 * time.Hour
)

// Get retrieves a session by ID. Expired sessions (>24h) are lazily evicted.
func (s *SessionStore) Get(id string) (*UserSession, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[id]
	if !ok {
		return nil, false
	}
	if time.Since(sess.CreatedAt) > sessionTTL {
		delete(s.sessions, id)
		return nil, false
	}
	return sess, true
}

// Delete removes a session by ID.
func (s *SessionStore) Delete(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

// RememberActiveUpstream updates the last selected upstream for a session.
func (s *SessionStore) RememberActiveUpstream(id, upstream string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		sess.ActiveUpstream = upstream
	}
}

// ActiveUpstream returns the remembered upstream for a session.
func (s *SessionStore) ActiveUpstream(id string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sess, ok := s.sessions[id]; ok {
		return sess.ActiveUpstream
	}
	return ""
}

// Restore re-creates a session from cookie data after a server restart.
// The original creation time is unknown, so the session gets a reduced TTL
// (half the normal sessionTTL) to limit how much a restart can extend a session.
func (s *SessionStore) Restore(sessionID, connectionID string) {
	s.RestoreWithSubject(sessionID, connectionID, "")
}

// RestoreWithSubject re-creates a session from cookie data with subject context.
func (s *SessionStore) RestoreWithSubject(sessionID, connectionID, subjectID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[sessionID] = &UserSession{
		ID:           sessionID,
		SubjectID:    subjectID,
		ConnectionID: connectionID,
		CreatedAt:    time.Now().Add(-sessionTTL / 2),
	}
}

func generateSessionID() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("crypto/rand failed: %w", err)
	}
	return hex.EncodeToString(b), nil
}

const (
	cookieName        = "wl_session"
	subjectCookieName = "wl_subject"
)

// SignSessionID signs a session ID with the given secret using HMAC-SHA256.
func SignSessionID(id, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	sig := hex.EncodeToString(mac.Sum(nil))
	return id + "." + sig
}

// VerifySessionID verifies a signed session cookie value. Returns the session ID if valid.
func VerifySessionID(signed, secret string) (string, bool) {
	// Find the last dot separator.
	dot := -1
	for i := len(signed) - 1; i >= 0; i-- {
		if signed[i] == '.' {
			dot = i
			break
		}
	}
	if dot < 0 || dot == 0 || dot == len(signed)-1 {
		return "", false
	}
	id := signed[:dot]
	sig := signed[dot+1:]

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(id))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", false
	}
	return id, true
}

// SignSessionCookie signs a session cookie containing both sessionID and connectionID.
// Format: sessionID.connectionID.HMAC(sessionID.connectionID, secret)
func SignSessionCookie(sessionID, connectionID, secret string) string {
	payload := sessionID + "." + connectionID
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	sig := hex.EncodeToString(mac.Sum(nil))
	return payload + "." + sig
}

// VerifySessionCookie verifies a signed session cookie in the new 3-segment format.
// Returns (sessionID, connectionID, ok).
// The signature is always a 64-char hex HMAC-SHA256. We split from the right
// to handle connectionIDs that contain dots.
func VerifySessionCookie(signed, secret string) (string, string, bool) {
	// The signature is the last 64 hex characters after the last dot.
	lastDot := strings.LastIndex(signed, ".")
	if lastDot < 1 || lastDot == len(signed)-1 {
		return "", "", false
	}
	payload := signed[:lastDot]
	sig := signed[lastDot+1:]

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(expected)) {
		return "", "", false
	}

	// Split payload into sessionID.connectionID at the first dot.
	firstDot := strings.Index(payload, ".")
	if firstDot < 1 || firstDot == len(payload)-1 {
		return "", "", false
	}
	return payload[:firstDot], payload[firstDot+1:], true
}

// SetSessionCookie sets the wl_session cookie on the response.
func SetSessionCookie(w http.ResponseWriter, sessionID, connectionID, secret string) {
	signed := SignSessionCookie(sessionID, connectionID, secret)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    signed,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
	})
}

// ClearSessionCookie clears the wl_session cookie.
func ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})
}

// ReadSessionCookie reads and verifies the session cookie from the request.
// Supports both old format (sessionID.sig → empty connectionID) and new format
// (sessionID.connectionID.sig).
func ReadSessionCookie(r *http.Request, secret string) (string, string, bool) {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return "", "", false
	}

	// Try new 3-segment format first.
	if sessionID, connectionID, ok := VerifySessionCookie(c.Value, secret); ok {
		return sessionID, connectionID, true
	}

	// Fall back to old 2-segment format (connectionID will be empty).
	if sessionID, ok := VerifySessionID(c.Value, secret); ok {
		return sessionID, "", true
	}

	return "", "", false
}

// SignSubjectID signs a stable hosted subject identifier.
func SignSubjectID(subjectID, secret string) string {
	return SignSessionID(subjectID, secret)
}

// VerifySubjectID verifies a stable hosted subject identifier cookie.
func VerifySubjectID(signed, secret string) (string, bool) {
	return VerifySessionID(signed, secret)
}

// SetSubjectCookie sets the stable hosted subject identifier cookie.
func SetSubjectCookie(w http.ResponseWriter, subjectID, secret string) {
	expiresAt := time.Now().Add(subjectCookieTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     subjectCookieName,
		Value:    SignSubjectID(subjectID, secret),
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(subjectCookieTTL / time.Second),
		Expires:  expiresAt,
	})
}

// ReadSubjectCookie reads and verifies the stable hosted subject cookie.
func ReadSubjectCookie(r *http.Request, secret string) (string, bool) {
	c, err := r.Cookie(subjectCookieName)
	if err != nil {
		return "", false
	}
	return VerifySubjectID(c.Value, secret)
}
