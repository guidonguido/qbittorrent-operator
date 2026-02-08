package qbittorrent

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
)

const (
	pbkdf2Iterations = 100000
	pbkdf2KeyLength  = 64
	pbkdf2SaltLength = 16
)

// HashPassword hashes a password using PBKDF2-HMAC-SHA512 in qBittorrent's format.
// Returns a string like: @ByteArray(BASE64_SALT:BASE64_HASH)
func HashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}
	return hashPasswordWithSalt(password, salt)
}

// hashPasswordWithSalt is the deterministic core used by HashPassword and tests.
func hashPasswordWithSalt(password string, salt []byte) (string, error) {
	hash, err := pbkdf2.Key(sha512.New, password, salt, pbkdf2Iterations, pbkdf2KeyLength)
	if err != nil {
		return "", fmt.Errorf("failed to derive key: %w", err)
	}
	return fmt.Sprintf("@ByteArray(%s:%s)",
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(hash),
	), nil
}
