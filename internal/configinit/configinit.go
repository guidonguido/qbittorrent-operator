package configinit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/guidonguido/qbittorrent-operator/internal/qbittorrent"
)

const (
	defaultCredentialsPath = "/credentials"
	defaultConfigPath      = "/config"
)

// Run reads credentials from the default mount paths and writes qBittorrent.conf
// if it does not already exist (first-boot only).
func Run() error {
	return RunWithPaths(defaultCredentialsPath, defaultConfigPath)
}

// RunWithPaths is the testable inner function that accepts custom paths.
func RunWithPaths(credentialsPath, configPath string) error {
	configFile := filepath.Join(configPath, "qBittorrent", "qBittorrent.conf")

	// Skip if config already exists (not first boot)
	if _, err := os.Stat(configFile); err == nil {
		fmt.Println("config-init: qBittorrent.conf already exists, skipping")
		return nil
	}

	// Read credentials
	usernameBytes, err := os.ReadFile(filepath.Join(credentialsPath, "username"))
	if err != nil {
		return fmt.Errorf("failed to read username: %w", err)
	}
	passwordBytes, err := os.ReadFile(filepath.Join(credentialsPath, "password"))
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}

	username := strings.TrimSpace(string(usernameBytes))
	password := strings.TrimSpace(string(passwordBytes))

	// Hash the password in qBittorrent's PBKDF2 format
	hashedPassword, err := qbittorrent.HashPassword(password)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	// Create config directory
	configDir := filepath.Join(configPath, "qBittorrent")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Write config file
	content := fmt.Sprintf("[Preferences]\nWebUI\\Username=%s\nWebUI\\Password_PBKDF2=\"%s\"\n",
		username, hashedPassword)

	if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	fmt.Println("config-init: wrote qBittorrent.conf with pre-seeded credentials")
	return nil
}
