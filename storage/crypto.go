package storage

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// Encrypt encrypts plaintext using AES-GCM with the provided key.
// The key must be 16, 24, or 32 bytes for AES-128, AES-192, or AES-256.
// Returns base64-encoded ciphertext.
func Encrypt(plaintext []byte, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Seal appends the encrypted data to nonce, so we get nonce + ciphertext + tag
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// Decrypt decrypts base64-encoded ciphertext using AES-GCM with the provided key.
func Decrypt(encoded string, key []byte) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

// DeriveKey creates a 32-byte key from a passphrase using simple padding/truncation.
// For production, consider using a proper KDF like Argon2 or PBKDF2.
func DeriveKey(passphrase string) []byte {
	key := make([]byte, 32)
	copy(key, []byte(passphrase))
	return key
}
