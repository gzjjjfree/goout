// data/config_test.go
package data

import (
	"encoding/json"
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

	cfg, err := DefaultConfig()
	if err != nil {
		t.Fatalf("failed to create default config: %v", err)
	}
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

func TestMergeConfigTemplate(t *testing.T) {
	baseBytes := GetDefaultConfigBytes()
	userJSON := []byte(`{
		"inbounds": [
			{
				"tag": "gz",
				"port": 10819,
				"listen": "127.0.0.1",
				"protocol": "socks"
			}
		]
	}`)

	merged, err := MergeConfigTemplate(baseBytes, userJSON)
	if err != nil {
		t.Fatalf("failed to merge config template: %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(merged, &parsed); err != nil {
		t.Fatalf("failed to parse merged json: %v", err)
	}

	inbounds, ok := parsed["inbounds"].([]interface{})
	if !ok || len(inbounds) == 0 {
		t.Fatalf("inbounds missing or invalid")
	}

	ib := inbounds[0].(map[string]interface{})
	if ib["port"] != float64(10819) {
		t.Errorf("expected port 10819, got %v", ib["port"])
	}
	if ib["sniffing"] == nil {
		t.Errorf("expected sniffing field to be preserved from template")
	}
	if ib["settings"] == nil {
		t.Errorf("expected settings field to be preserved from template")
	}
}
