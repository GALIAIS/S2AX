package repository

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const maxPreviousEncryptionKeys = 4

// AESEncryptor implements SecretEncryptor using AES-256-GCM
type AESEncryptor struct {
	currentKey     []byte
	decryptionKeys [][]byte
}

// NewAESEncryptor creates a new AES encryptor
func NewAESEncryptor(cfg *config.Config) (service.SecretEncryptor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config")
	}

	currentKey, err := decodeAES256Key(cfg.Totp.EncryptionKey, "totp encryption key")
	if err != nil {
		return nil, err
	}
	previousKeys, err := decodePreviousEncryptionKeys(cfg.Totp.PreviousEncryptionKeys, currentKey)
	if err != nil {
		return nil, err
	}

	decryptionKeys := make([][]byte, 0, 1+len(previousKeys))
	decryptionKeys = append(decryptionKeys, currentKey)
	decryptionKeys = append(decryptionKeys, previousKeys...)
	return &AESEncryptor{
		currentKey:     currentKey,
		decryptionKeys: decryptionKeys,
	}, nil
}

// Encrypt encrypts plaintext using AES-256-GCM
// Output format: base64(nonce + ciphertext + tag)
func (e *AESEncryptor) Encrypt(plaintext string) (string, error) {
	if e == nil || len(e.currentKey) == 0 {
		return "", fmt.Errorf("encryptor is not initialized")
	}
	block, err := aes.NewCipher(e.currentKey)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	// Generate a random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate nonce: %w", err)
	}

	// Encrypt the plaintext
	// Seal appends the ciphertext and tag to the nonce
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)

	// Encode as base64
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts ciphertext using AES-256-GCM
func (e *AESEncryptor) Decrypt(ciphertext string) (string, error) {
	if e == nil || len(e.decryptionKeys) == 0 {
		return "", fmt.Errorf("encryptor is not initialized")
	}
	// Decode from base64
	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("decode base64: %w", err)
	}

	nonceSize, err := gcmNonceSize(e.decryptionKeys[0])
	if err != nil {
		return "", err
	}
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	// Extract nonce and ciphertext
	nonce, ciphertextData := data[:nonceSize], data[nonceSize:]

	// Try the active key first, then the bounded historical keyring. Encryption
	// always uses currentKey, so old keys are read-only compatibility material.
	for _, key := range e.decryptionKeys {
		block, cipherErr := aes.NewCipher(key)
		if cipherErr != nil {
			return "", fmt.Errorf("create cipher: %w", cipherErr)
		}
		gcm, gcmErr := cipher.NewGCM(block)
		if gcmErr != nil {
			return "", fmt.Errorf("create gcm: %w", gcmErr)
		}
		plaintext, openErr := gcm.Open(nil, nonce, ciphertextData, nil)
		if openErr == nil {
			return string(plaintext), nil
		}
	}

	return "", fmt.Errorf("decrypt: authentication failed with configured keyring")
}

func decodeAES256Key(value, label string) ([]byte, error) {
	key, err := hex.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %w", label, err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("%s must be 32 bytes (64 hex chars), got %d bytes", label, len(key))
	}
	return key, nil
}

func decodePreviousEncryptionKeys(values []string, currentKey []byte) ([][]byte, error) {
	if len(values) > maxPreviousEncryptionKeys {
		return nil, fmt.Errorf("totp previous encryption keys must contain at most %d keys", maxPreviousEncryptionKeys)
	}
	seen := map[string]struct{}{hex.EncodeToString(currentKey): {}}
	keys := make([][]byte, 0, len(values))
	for index, value := range values {
		key, err := decodeAES256Key(value, fmt.Sprintf("totp previous encryption key #%d", index+1))
		if err != nil {
			return nil, err
		}
		fingerprint := hex.EncodeToString(key)
		if _, exists := seen[fingerprint]; exists {
			return nil, fmt.Errorf("totp encryption keyring contains a duplicate key at previous index %d", index)
		}
		seen[fingerprint] = struct{}{}
		keys = append(keys, key)
	}
	return keys, nil
}

func gcmNonceSize(key []byte) (int, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return 0, fmt.Errorf("create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return 0, fmt.Errorf("create gcm: %w", err)
	}
	return gcm.NonceSize(), nil
}
