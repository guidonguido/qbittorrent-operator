package configinit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// overrideDefaultPaths temporarily overrides the package-level path constants
// for testing, restoring them when the test completes.
func overrideDefaultPaths(t *testing.T, credDir, configDir string) {
	t.Helper()
	origCred := defaultCredentialsPath
	origConfig := defaultConfigPath
	defaultCredentialsPath = credDir
	defaultConfigPath = configDir
	t.Cleanup(func() {
		defaultCredentialsPath = origCred
		defaultConfigPath = origConfig
	})
}

func setupCredentials(t *testing.T, dir, username, password string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "username"), []byte(username), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "password"), []byte(password), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRun_FirstBoot(t *testing.T) {
	credDir := t.TempDir()
	configDir := t.TempDir()
	overrideDefaultPaths(t, credDir, configDir)
	setupCredentials(t, credDir, "admin", "secretpass")

	if err := Run(); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	configFile := filepath.Join(configDir, "qBittorrent", "qBittorrent.conf")
	if _, err := os.Stat(configFile); os.IsNotExist(err) {
		t.Fatal("expected config file to be created")
	}
}

func TestRun_ExistingConfig(t *testing.T) {
	credDir := t.TempDir()
	configDir := t.TempDir()
	overrideDefaultPaths(t, credDir, configDir)
	setupCredentials(t, credDir, "admin", "secretpass")

	// Pre-create config file
	qbtDir := filepath.Join(configDir, "qBittorrent")
	if err := os.MkdirAll(qbtDir, 0755); err != nil {
		t.Fatal(err)
	}
	existingContent := "[Preferences]\nWebUI\\Username=olduser\n"
	if err := os.WriteFile(filepath.Join(qbtDir, "qBittorrent.conf"), []byte(existingContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := Run(); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	// Verify the file was NOT overwritten
	content, err := os.ReadFile(filepath.Join(qbtDir, "qBittorrent.conf"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != existingContent {
		t.Errorf("config file was overwritten: got %q, want %q", string(content), existingContent)
	}
}

func TestRun_MissingCredentials(t *testing.T) {
	credDir := t.TempDir() // empty directory
	configDir := t.TempDir()
	overrideDefaultPaths(t, credDir, configDir)

	err := Run()
	if err == nil {
		t.Fatal("expected error for missing credentials")
	}
	if !strings.Contains(err.Error(), "failed to read username") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRun_MissingPassword(t *testing.T) {
	credDir := t.TempDir()
	configDir := t.TempDir()
	overrideDefaultPaths(t, credDir, configDir)
	// Only write username, not password
	if err := os.WriteFile(filepath.Join(credDir, "username"), []byte("admin"), 0644); err != nil {
		t.Fatal(err)
	}

	err := Run()
	if err == nil {
		t.Fatal("expected error for missing password")
	}
	if !strings.Contains(err.Error(), "failed to read password") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRun_ConfigContent(t *testing.T) {
	credDir := t.TempDir()
	configDir := t.TempDir()
	overrideDefaultPaths(t, credDir, configDir)
	setupCredentials(t, credDir, "admin", "testpass123")

	if err := Run(); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	configFile := filepath.Join(configDir, "qBittorrent", "qBittorrent.conf")
	content, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}

	contentStr := string(content)
	if !strings.Contains(contentStr, "[Preferences]") {
		t.Error("config missing [Preferences] section")
	}
	if !strings.Contains(contentStr, "WebUI\\Username=admin") {
		t.Error("config missing WebUI\\Username")
	}
	if !strings.Contains(contentStr, "WebUI\\Password_PBKDF2=\"@ByteArray(") {
		t.Error("config missing WebUI\\Password_PBKDF2 with @ByteArray format")
	}
}
