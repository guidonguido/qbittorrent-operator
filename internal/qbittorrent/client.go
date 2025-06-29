package qbittorrent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Client is a client for the qbittorrent API
type Client struct {
	baseURL    string
	httpClient *http.Client
	sessionID  string // SID obtained from login
}

// Struct representing a torrent object returned by the qbittorrent API
// from /api/v2/torrents/info
// the struct maps only the fields we need
type TorrentInfo struct {
	AddedOn     int64  `json:"added_on"`
	AmountLeft  int64  `json:"amount_left"`
	ContentPath string `json:"content_path"`
	Hash        string `json:"hash"`
	MagnetURI   string `json:"magnet_uri"`
	Name        string `json:"name"`
	Size        int64  `json:"size"`
	State       string `json:"state"`
	TotalSize   int64  `json:"total_size"`
	TimeActive  int64  `json:"time_active"`
}

// NewClient creates a new qbittorrent client
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

// Authenticate with qbittorrent and store the session ID
func (c *Client) Login(ctx context.Context, username, password string) error {
	logger := log.FromContext(ctx).WithName("qbittorrent-client")
	loginURL := c.baseURL + "/api/v2/auth/login"

	logger.Info("Logging in to qbittorrent",
		"URL", loginURL,
		"username", username,
	)
	loginData := url.Values{}
	loginData.Set("username", username)
	loginData.Set("password", password)

	resp, err := c.httpClient.PostForm(loginURL, loginData)
	if err != nil {
		logger.Error(err, "Failed to login to qbittorrent")
		return fmt.Errorf("failed to login to qbittorrent: %w", err)
	}
	defer resp.Body.Close()

	// check for non-200 status code
	if resp.StatusCode != http.StatusOK {
		logger.Error(nil, "Failed to login to qbittorrent",
			"status", resp.StatusCode)
		return fmt.Errorf("failed to login to qbittorrent. Status: %s", resp.Status)
	}

	// Get the session ID from the response
	for _, cookie := range resp.Cookies() {
		if cookie.Name == "SID" {
			c.sessionID = cookie.Value
			break
		}
	}

	if c.sessionID == "" {
		logger.Error(nil, "Failed to get session ID from qbittorrent response")
		return fmt.Errorf("failed to get session ID from qbittorrent response")
	}

	logger.V(1).Info("Successfully logged in to qbittorrent",
		"sessionID", c.sessionID,
		"username", username,
	)

	return nil
}

// Retrieve Torrents info list
func (c *Client) GetTorrentsInfo(ctx context.Context) ([]TorrentInfo, error) {
	logger := log.FromContext(ctx).WithName("qbittorrent-client")
	torrentsInfoURL := c.baseURL + "/api/v2/torrents/info"

	logger.V(1).Info("Getting torrents info list",
		"URL", torrentsInfoURL,
	)

	// Add the session ID to the request
	req, err := http.NewRequest("GET", torrentsInfoURL, nil)
	if err != nil {
		logger.Error(err, "Failed to create request")
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.AddCookie(&http.Cookie{
		Name:  "SID",
		Value: c.sessionID,
	})

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error(err, "Failed to get torrents info list")
		return nil, fmt.Errorf("failed to get torrents info list: %w", err)
	}
	defer resp.Body.Close()

	// check for non-200 status code
	if resp.StatusCode != http.StatusOK {
		logger.Error(nil, "Failed to get torrents info list",
			"status", resp.StatusCode)

		// check if the error is 401 Unauthorized
		if resp.StatusCode == http.StatusUnauthorized {
			logger.Error(nil, "Unauthorized access to qbittorrent",
				"status", resp.StatusCode)
			return nil, fmt.Errorf("unauthorized access to qbittorrent")
		}

		return nil, fmt.Errorf("failed to get torrents info list. Status: %s", resp.Status)
	}

	// Parse the response body
	var torrentsInfo []TorrentInfo
	err = json.NewDecoder(resp.Body).Decode(&torrentsInfo)
	if err != nil {
		logger.Error(err, "Failed to parse torrents info list")
		return nil, fmt.Errorf("failed to parse torrents info list: %w", err)
	}

	logger.V(1).Info("Successfully got torrents info list",
		"count", len(torrentsInfo),
	)

	return torrentsInfo, nil
}

// Get a torrent info from qbittorrent
func (c *Client) GetTorrentInfo(ctx context.Context, hash string) (*TorrentInfo, error) {
	logger := log.FromContext(ctx).WithName("qbittorrent-client")

	torrentsInfo, err := c.GetTorrentsInfo(ctx)
	if err != nil {
		logger.Error(err, "Failed to get torrents info list")
		return nil, fmt.Errorf("failed to get torrents info list: %w", err)
	}

	for _, torrent := range torrentsInfo {
		if torrent.Hash == hash {
			return &torrent, nil
		}
	}

	logger.V(1).Info("Torrent not found", "hash", hash)
	return nil, nil
}

// Add a torrent to qbittorrent
func (c *Client) AddTorrent(ctx context.Context, magnetURI string) error {
	logger := log.FromContext(ctx).WithName("qbittorrent-client")
	torrentsAddURL := c.baseURL + "/api/v2/torrents/add"

	logger.Info("Adding torrent to qbittorrent",
		"URL", torrentsAddURL,
		"magnetURI", magnetURI,
	)

	// Buffer to store the multi-part form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	writer.WriteField("urls", magnetURI)
	urlFields, err := writer.CreateFormField("urls")
	if err != nil {
		logger.Error(err, "Failed to create form field")
		return fmt.Errorf("failed to create form field: %w", err)
	}

	_, err = urlFields.Write([]byte(magnetURI))

	// Close the writer to finalize the form data
	writer.Close()

	req, err := http.NewRequest("POST", torrentsAddURL, body)
	if err != nil {
		logger.Error(err, "Failed to create request")
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.AddCookie(&http.Cookie{
		Name:  "SID",
		Value: c.sessionID,
	})

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error(err, "Failed to add torrent")
		return fmt.Errorf("failed to add torrent: %w", err)
	}
	defer resp.Body.Close()

	// check for non-200 status code
	if resp.StatusCode != http.StatusOK {
		logger.Error(nil, "Failed to add torrent",
			"status", resp.StatusCode)

		// check if the error is 401 Unauthorized
		if resp.StatusCode == http.StatusUnauthorized {
			logger.Error(nil, "Unauthorized access to qbittorrent",
				"status", resp.StatusCode)
			return fmt.Errorf("unauthorized access to qbittorrent")
		}

		return fmt.Errorf("failed to add torrent. Status: %s", resp.Status)
	}

	logger.Info("Successfully added torrent",
		"magnetURI", magnetURI,
	)

	return nil
}

// Delete a torrent from qbittorrent
func (c *Client) DeleteTorrent(ctx context.Context, hash string, deleteFiles bool) error {
	logger := log.FromContext(ctx).WithName("qbittorrent-client")
	torrentsDeleteURL := c.baseURL + "/api/v2/torrents/delete"

	logger.Info("Deleting torrent from qbittorrent",
		"URL", torrentsDeleteURL,
		"hash", hash,
		"deleteFiles", deleteFiles,
	)

	// Prepare URL-encoded form data
	data := url.Values{}
	data.Set("hashes", hash)
	data.Set("deleteFiles", fmt.Sprintf("%t", deleteFiles))

	req, err := http.NewRequest("POST", torrentsDeleteURL, bytes.NewBufferString(data.Encode()))
	if err != nil {
		logger.Error(err, "Failed to create request")
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(&http.Cookie{
		Name:  "SID",
		Value: c.sessionID,
	})

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error(err, "Failed to delete torrent")
		return fmt.Errorf("failed to delete torrent: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Error(nil, "Failed to delete torrent",
			"status", resp.StatusCode)

		if resp.StatusCode == http.StatusUnauthorized {
			logger.Error(nil, "Unauthorized access to qbittorrent",
				"status", resp.StatusCode)
			return fmt.Errorf("unauthorized access to qbittorrent")
		}

		return fmt.Errorf("failed to delete torrent. Status: %s", resp.Status)
	}

	logger.Info("Successfully deleted torrent",
		"hash", hash,
	)
	return nil
}
