package qbittorrent

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClient(t *testing.T) {
	RegisterFailHandler(Fail)

	client := NewClient("http://localhost:8080")

	if client.baseURL != "http://localhost:8080" {
		t.Errorf("Expected baseURL to be http://localhost:8080, got %s", client.baseURL)
	}

	if client.sessionID != "" {
		t.Errorf("Expected sessionID to be empty, got %s", client.sessionID)
	}

	if client.httpClient == nil {
		t.Errorf("Expected httpClient to be non-nil")
	}
}

func TestNewClient_TrimsTrailingSlash(t *testing.T) {
	client := NewClient("http://localhost:8080/")

	if client.baseURL != "http://localhost:8080" {
		t.Errorf("Expected trailing slash to be trimmed, got '%s'", client.baseURL)
	}
}
