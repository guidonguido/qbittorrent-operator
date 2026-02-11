package qbittorrent

import (
	"strings"
	"testing"
)

func TestHashPassword_Format(t *testing.T) {
	result, err := HashPassword("testpassword")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	if !strings.HasPrefix(result, "@ByteArray(") {
		t.Errorf("expected prefix @ByteArray(, got %s", result)
	}
	if !strings.HasSuffix(result, ")") {
		t.Errorf("expected suffix ), got %s", result)
	}

	// Extract inner content and verify it has salt:hash format
	inner := result[len("@ByteArray(") : len(result)-1]
	parts := strings.SplitN(inner, ":", 2)
	if len(parts) != 2 {
		t.Fatalf("expected salt:hash format, got %s", inner)
	}
	if parts[0] == "" || parts[1] == "" {
		t.Errorf("salt or hash is empty: salt=%q, hash=%q", parts[0], parts[1])
	}
}

func TestHashPassword_Uniqueness(t *testing.T) {
	result1, err := HashPassword("samepassword")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	result2, err := HashPassword("samepassword")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	if result1 == result2 {
		t.Error("expected different hashes due to random salt, got identical results")
	}
}

func TestHashPassword_DifferentPasswords(t *testing.T) {
	result1, err := HashPassword("password1")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}
	result2, err := HashPassword("password2")
	if err != nil {
		t.Fatalf("HashPassword returned error: %v", err)
	}

	if result1 == result2 {
		t.Error("expected different hashes for different passwords")
	}
}
