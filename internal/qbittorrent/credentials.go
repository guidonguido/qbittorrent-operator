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

// Hash password in PBKDF2-HMAC-SHA512; used by qBittorrent for storing pw
func HashPassword(password string) (string, error) {
	salt := make([]byte, pbkdf2SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("failed to generate salt: %w", err)
	}

	hash, err := pbkdf2.Key(sha512.New, password, salt, pbkdf2Iterations, pbkdf2KeyLength)
	if err != nil {
		return "", fmt.Errorf("failed to derive key: %w", err)
	}

	return fmt.Sprintf("@ByteArray(%s:%s)",
		base64.StdEncoding.EncodeToString(salt),
		base64.StdEncoding.EncodeToString(hash),
	), nil
}
