package data

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigSaveAndLoad(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "goout-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	cfg.ServerAddress = "test.proxy.com"
	cfg.ServerPort = 9999

	if err := SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.ServerAddress != "test.proxy.com" {
		t.Errorf("expected ServerAddress 'test.proxy.com', got '%s'", loaded.ServerAddress)
	}

	if loaded.ServerPort != 9999 {
		t.Errorf("expected ServerPort 9999, got %d", loaded.ServerPort)
	}
}
