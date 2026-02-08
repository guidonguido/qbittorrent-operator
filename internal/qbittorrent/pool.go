package qbittorrent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

// ClientPool manages a pool of qBittorrent clients keyed by TCC namespace/name.
type ClientPool struct {
	mu      sync.RWMutex
	clients map[string]*poolEntry
	ttl     time.Duration
}

type poolEntry struct {
	client   *Client
	credHash string
	lastUsed time.Time
}

// NewClientPool creates a new pool that evicts entries not used within the given TTL.
func NewClientPool(ttl time.Duration) *ClientPool {
	return &ClientPool{
		clients: make(map[string]*poolEntry),
		ttl:     ttl,
	}
}

// GetOrCreate returns an existing client or creates a new one.
// If credentials have changed (detected via hash), the old client is replaced.
func (p *ClientPool) GetOrCreate(ctx context.Context, key, url, username, password string) (*Client, error) {
	credHash := hashCredentials(url, username, password)

	p.mu.RLock()
	entry, exists := p.clients[key]
	p.mu.RUnlock()

	if exists && entry.credHash == credHash {
		p.mu.Lock()
		entry.lastUsed = time.Now()
		p.mu.Unlock()
		return entry.client, nil
	}

	// Create new client and login
	client := NewClient(url)
	if err := client.Login(ctx, username, password); err != nil {
		return nil, fmt.Errorf("failed to login for %s: %w", key, err)
	}

	p.mu.Lock()
	p.clients[key] = &poolEntry{
		client:   client,
		credHash: credHash,
		lastUsed: time.Now(),
	}
	p.mu.Unlock()

	return client, nil
}

// Remove evicts a specific client from the pool.
func (p *ClientPool) Remove(key string) {
	p.mu.Lock()
	delete(p.clients, key)
	p.mu.Unlock()
}

// Cleanup evicts entries that have not been used within the TTL.
func (p *ClientPool) Cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for key, entry := range p.clients {
		if now.Sub(entry.lastUsed) > p.ttl {
			delete(p.clients, key)
		}
	}
}

func hashCredentials(url, username, password string) string {
	h := sha256.Sum256([]byte(url + "|" + username + "|" + password))
	return fmt.Sprintf("%x", h)
}
