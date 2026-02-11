package configinit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/guidonguido/qbittorrent-operator/internal/qbittorrent"
)

var (
	defaultCredentialsPath = "/credentials"
	defaultConfigPath      = "/config"
)

// Read credentials mounted to defaultCredentialsPath and write qBittorrent.conf
func Run() error {

	// Up to qBittorrent 5.1.4, the config file is expected at /config/qBittorrent/qBittorrent.conf
	configFile := filepath.Join(defaultConfigPath, "qBittorrent", "qBittorrent.conf")

	// Skip config-init if the config file already exists
	if _, err := os.Stat(configFile); err == nil {
		fmt.Println("config-init: qBittorrent.conf already exists, skipping")
		return nil
	}

	// TorrentServer pods mount credentials from secret at /credentials/username and /credentials/password
	usernameBytes, err := os.ReadFile(filepath.Join(defaultCredentialsPath, "username"))
	if err != nil {
		return fmt.Errorf("failed to read username: %w", err)
	}
	passwordBytes, err := os.ReadFile(filepath.Join(defaultCredentialsPath, "password"))
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}

	username := strings.TrimSpace(string(usernameBytes))
	password := strings.TrimSpace(string(passwordBytes))

	// qBittorrent expects the password to be hashed using PBKDF2
	hashedPassword, err := qbittorrent.HashPassword(password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	configDir := filepath.Join(defaultConfigPath, "qBittorrent")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	content := fmt.Sprintf("[Preferences]\nWebUI\\Username=%s\nWebUI\\Password_PBKDF2=\"%s\"\n",
		username, hashedPassword)

	// Only owner can write the created file
	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Printf("config-init: wrote %s/qBittorrent/qBittorrent.conf with pre-seeded credentials\n", configDir)
	return nil
}
