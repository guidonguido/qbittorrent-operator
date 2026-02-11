package qbittorrent

import (
	"fmt"
	"strings"
)

func GetTorrentHash(magnetURI string) (string, error) {
	// Find the btih: prefix
	btihIndex := strings.Index(magnetURI, "btih:")
	if btihIndex == -1 {
		return "", fmt.Errorf("'btih:' not found")
	}

	// Start after "btih:"
	hashStart := btihIndex + 5
	if hashStart >= len(magnetURI) {
		return "", fmt.Errorf("no hash after 'btih:'")
	}

	// Hash ends at the next '&' or end of string
	hashEnd := strings.Index(magnetURI[hashStart:], "&")
	if hashEnd == -1 {
		return magnetURI[hashStart:], nil
	}

	return magnetURI[hashStart : hashStart+hashEnd], nil
}
