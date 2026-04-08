package dolthubauth

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strings"
)

const localEncryptionBackend = "local-master-key"

const kmsEnvelopeEncryptionBackend = "gcp-kms-envelope"

// CredentialCipher encrypts and decrypts stored DoltHub API keys.
type CredentialCipher interface {
	ReadinessChecker
	Encrypt(context.Context, []byte) ([]byte, string, string, error)
	Decrypt(context.Context, []byte, string, string) ([]byte, error)
}

var newGCPKMSEnvelopeCipher = NewGCPKMSEnvelopeCipher

// MultiCipher routes decrypt operations by stored backend while always
// encrypting with the configured primary backend.
type MultiCipher struct {
	primary  CredentialCipher
	handlers map[string]CredentialCipher
}

// NewMultiCipher constructs a primary cipher with optional decrypt fallbacks.
func NewMultiCipher(primary CredentialCipher, fallbacks map[string]CredentialCipher) (*MultiCipher, error) {
	if primary == nil {
		return nil, errors.New("primary credential cipher is required")
	}
	handlers := map[string]CredentialCipher{}
	for backend, cipher := range fallbacks {
		if cipher == nil {
			continue
		}
		handlers[backend] = cipher
	}
	switch typed := primary.(type) {
	case *GCPKMSEnvelopeCipher:
		handlers[kmsEnvelopeEncryptionBackend] = typed
	case *LocalMasterKey:
		handlers[localEncryptionBackend] = typed
	}
	return &MultiCipher{primary: primary, handlers: handlers}, nil
}

// Check verifies the primary cipher and any decrypt fallbacks are ready.
func (m *MultiCipher) Check(ctx context.Context) error {
	if err := m.primary.Check(ctx); err != nil {
		return err
	}
	for _, cipher := range m.handlers {
		if cipher == m.primary {
			continue
		}
		if err := cipher.Check(ctx); err != nil {
			return err
		}
	}
	return nil
}

// Encrypt delegates to the configured primary backend.
func (m *MultiCipher) Encrypt(ctx context.Context, plaintext []byte) ([]byte, string, string, error) {
	return m.primary.Encrypt(ctx, plaintext)
}

// Decrypt dispatches to the backend stored with the ciphertext.
func (m *MultiCipher) Decrypt(ctx context.Context, ciphertext []byte, keyVersion, backend string) ([]byte, error) {
	cipher, ok := m.handlers[backend]
	if !ok {
		return nil, fmt.Errorf("unsupported encryption backend %q", backend)
	}
	return cipher.Decrypt(ctx, ciphertext, keyVersion, backend)
}

// Close closes any underlying cipher clients that expose io.Closer.
func (m *MultiCipher) Close() error {
	var closeErr error
	if closer, ok := m.primary.(io.Closer); ok {
		closeErr = closer.Close()
	}
	for _, cipher := range m.handlers {
		if cipher == m.primary {
			continue
		}
		closer, ok := cipher.(io.Closer)
		if !ok {
			continue
		}
		if err := closer.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	return closeErr
}

// NewCredentialCipher builds the configured primary encryption backend plus any
// compatible decrypt fallbacks needed for rollout.
func NewCredentialCipher(ctx context.Context, cfg Config) (CredentialCipher, error) {
	switch cfg.effectiveEncryptionBackend() {
	case localEncryptionBackend:
		return NewLocalMasterKey(cfg.MasterKey)
	case kmsEnvelopeEncryptionBackend:
		primary, err := newGCPKMSEnvelopeCipher(ctx, cfg.KMSKeyName, cfg.GCPCredentialsJSON)
		if err != nil {
			return nil, err
		}
		fallbacks := map[string]CredentialCipher{
			kmsEnvelopeEncryptionBackend: primary,
		}
		if strings.TrimSpace(cfg.MasterKey) != "" {
			legacy, err := NewLocalMasterKey(cfg.MasterKey)
			if err != nil {
				_ = primary.Close()
				return nil, err
			}
			fallbacks[localEncryptionBackend] = legacy
		}
		multi, err := NewMultiCipher(primary, fallbacks)
		if err != nil {
			_ = primary.Close()
			return nil, err
		}
		return multi, nil
	default:
		return nil, fmt.Errorf("unsupported encryption backend %q", cfg.EncryptionBackend)
	}
}

// Encrypt seals plaintext using AES-GCM with a key derived from the configured
// local master key. The ciphertext layout is nonce || ciphertext.
func (k *LocalMasterKey) Encrypt(_ context.Context, plaintext []byte) ([]byte, string, string, error) {
	block, err := aes.NewCipher(k.derivedKey())
	if err != nil {
		return nil, "", "", fmt.Errorf("construct cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, "", "", fmt.Errorf("construct gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, "", "", fmt.Errorf("generate nonce: %w", err)
	}

	sealed := gcm.Seal(nil, nonce, plaintext, nil)
	ciphertext := append(append([]byte(nil), nonce...), sealed...)
	return ciphertext, localEncryptionBackend, localEncryptionBackend, nil
}

// Decrypt opens ciphertext produced by Encrypt.
func (k *LocalMasterKey) Decrypt(_ context.Context, ciphertext []byte, keyVersion, backend string) ([]byte, error) {
	if backend != localEncryptionBackend {
		return nil, fmt.Errorf("unsupported encryption backend %q", backend)
	}
	if keyVersion != localEncryptionBackend {
		return nil, fmt.Errorf("unsupported key version %q", keyVersion)
	}

	block, err := aes.NewCipher(k.derivedKey())
	if err != nil {
		return nil, fmt.Errorf("construct cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("construct gcm: %w", err)
	}
	if len(ciphertext) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}

	nonce := ciphertext[:gcm.NonceSize()]
	body := ciphertext[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, body, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt credential: %w", err)
	}
	return plaintext, nil
}

func (k *LocalMasterKey) derivedKey() []byte {
	sum := sha256.Sum256([]byte(k.key))
	return sum[:]
}

func bodySHA256(body []byte) string {
	sum := sha256.Sum256(body)
	return fmt.Sprintf("%x", sum[:])
}

func macSHA256(secret string, parts ...[]byte) []byte {
	mac := hmac.New(sha256.New, []byte(secret))
	for _, part := range parts {
		mac.Write(part)
	}
	return mac.Sum(nil)
}
