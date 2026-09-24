package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

var (
	ErrInvalidCiphertext = errors.New("invalid or tampered ciphertext")
	ErrTokenExpired      = errors.New("token has expired")
)

type Encryptor struct {
	aead cipher.AEAD
}

func NewEncryptor(secret []byte) (*Encryptor, error) {
	if len(secret) == 0 {
		return nil, errors.New("secret key cannot be empty")
	}

	// Derive a 32-byte key using SHA-256 to ensure exactly 32 bytes for AES-256
	key := sha256.Sum256(secret)

	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher block: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	return &Encryptor{aead: aead}, nil
}

func (e *Encryptor) EncryptJSON(v any) (string, error) {
	plaintext, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("failed to marshal json: %w", err)
	}

	nonce := make([]byte, e.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal appends ciphertext and authentication tag to nonce
	sealed := e.aead.Seal(nonce, nonce, plaintext, nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (e *Encryptor) DecryptJSON(token string, dest any) error {
	data, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return fmt.Errorf("%w: invalid base64 encoding", ErrInvalidCiphertext)
	}

	nonceSize := e.aead.NonceSize()
	if len(data) < nonceSize {
		return fmt.Errorf("%w: payload too short", ErrInvalidCiphertext)
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := e.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("%w: decryption or authentication failed", ErrInvalidCiphertext)
	}

	if err := json.Unmarshal(plaintext, dest); err != nil {
		return fmt.Errorf("failed to unmarshal payload: %w", err)
	}

	return nil
}

func GenerateSecureRandomString(byteLength int) (string, error) {
	b := make([]byte, byteLength)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", fmt.Errorf("failed to generate random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func ConstantTimeCompare(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
