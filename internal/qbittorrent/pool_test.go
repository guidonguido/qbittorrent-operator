package qbittorrent

import (
	"testing"
	"time"
)

func TestNewClientPool(t *testing.T) {
	pool := NewClientPool(5 * time.Minute)
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
	if pool.ttl != 5*time.Minute {
		t.Errorf("expected TTL 5m, got %v", pool.ttl)
	}
	if len(pool.clients) != 0 {
		t.Errorf("expected empty clients map, got %d entries", len(pool.clients))
	}
}

func TestHashCredentials(t *testing.T) {
	h1 := hashCredentials("http://localhost:8080", "admin", "pass")
	h2 := hashCredentials("http://localhost:8080", "admin", "pass")
	if h1 != h2 {
		t.Errorf("same inputs should produce same hash")
	}

	h3 := hashCredentials("http://localhost:8080", "admin", "different")
	if h1 == h3 {
		t.Errorf("different inputs should produce different hash")
	}

	h4 := hashCredentials("http://other:8080", "admin", "pass")
	if h1 == h4 {
		t.Errorf("different URL should produce different hash")
	}
}

func TestRemove(t *testing.T) {
	pool := NewClientPool(5 * time.Minute)
	// Manually insert an entry
	pool.clients["test/key"] = &poolEntry{
		client:   &Client{},
		credHash: "hash",
		lastUsed: time.Now(),
	}

	if len(pool.clients) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(pool.clients))
	}

	pool.Remove("test/key")
	if len(pool.clients) != 0 {
		t.Errorf("expected 0 entries after remove, got %d", len(pool.clients))
	}

	// Removing non-existent key should not panic
	pool.Remove("nonexistent")
}

func TestCleanup(t *testing.T) {
	pool := NewClientPool(1 * time.Second)

	// Insert an entry that's already expired
	pool.clients["old"] = &poolEntry{
		client:   &Client{},
		credHash: "hash1",
		lastUsed: time.Now().Add(-2 * time.Second),
	}

	// Insert a fresh entry
	pool.clients["new"] = &poolEntry{
		client:   &Client{},
		credHash: "hash2",
		lastUsed: time.Now(),
	}

	pool.Cleanup()

	if len(pool.clients) != 1 {
		t.Errorf("expected 1 entry after cleanup, got %d", len(pool.clients))
	}
	if _, ok := pool.clients["new"]; !ok {
		t.Error("expected 'new' entry to survive cleanup")
	}
	if _, ok := pool.clients["old"]; ok {
		t.Error("expected 'old' entry to be evicted")
	}
}

func TestCleanupAllExpired(t *testing.T) {
	pool := NewClientPool(1 * time.Millisecond)

	pool.clients["a"] = &poolEntry{
		client:   &Client{},
		credHash: "h1",
		lastUsed: time.Now().Add(-1 * time.Second),
	}
	pool.clients["b"] = &poolEntry{
		client:   &Client{},
		credHash: "h2",
		lastUsed: time.Now().Add(-1 * time.Second),
	}

	pool.Cleanup()

	if len(pool.clients) != 0 {
		t.Errorf("expected 0 entries after cleanup, got %d", len(pool.clients))
	}
}
