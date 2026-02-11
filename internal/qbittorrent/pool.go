package qbittorrent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"time"
)

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

func NewClientPool(ttl time.Duration) *ClientPool {
	return &ClientPool{
		clients: make(map[string]*poolEntry),
		ttl:     ttl,
	}
}

func (p *ClientPool) GetOrCreate(ctx context.Context, url, username, password string) (*Client, error) {
	credHash := hashCredentials(url, username, password)

	p.mu.RLock()
	entry, exists := p.clients[credHash]
	p.mu.RUnlock()

	if exists && entry.credHash == credHash {
		p.mu.Lock()
		entry.lastUsed = time.Now()
		p.mu.Unlock()
		go scheduleRemove(p, credHash)
		return entry.client, nil
	}

	// Create new client and login
	client := NewClient(url)
	if err := client.Login(ctx, username, password); err != nil {
		return nil, fmt.Errorf("failed to login for credentials[%s, %s] url[%s]: %w",
			username, password, url, err)
	}

	p.mu.Lock()
	p.clients[credHash] = &poolEntry{
		client:   client,
		credHash: credHash,
		lastUsed: time.Now(),
	}
	p.mu.Unlock()

	go scheduleRemove(p, credHash)
	return client, nil
}

func (p *ClientPool) Remove(credHash string) {
	p.mu.Lock()
	delete(p.clients, credHash)
	p.mu.Unlock()
}

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

func scheduleRemove(pool *ClientPool, credHash string) {
	time.AfterFunc(pool.ttl, func() {
		pool.mu.RLock()
		entry, exists := pool.clients[credHash]
		pool.mu.RUnlock()

		if !exists {
			return
		}

		if time.Since(entry.lastUsed) > pool.ttl {
			pool.Remove(credHash)
		}
	})
}

func hashCredentials(url, username, password string) string {
	h := sha256.Sum256([]byte(url + "|" + username + "|" + password))
	return fmt.Sprintf("%x", h)
}
