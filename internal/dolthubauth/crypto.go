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
)

const localEncryptionBackend = "local-master-key"

// CredentialCipher encrypts and decrypts stored DoltHub API keys.
type CredentialCipher interface {
	ReadinessChecker
	Encrypt(context.Context, []byte) ([]byte, string, string, error)
	Decrypt(context.Context, []byte, string, string) ([]byte, error)
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
