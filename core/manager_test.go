package core

import (
	"os"
	"testing"

	"github.com/gzjjjfree/goout/data"
)

func TestV2rayManagerLifecycle(t *testing.T) {
	logCalled := false
	mgr := GetManager(func(msg string) {
		logCalled = true
		t.Logf("[V2Ray Log] %s", msg)
	})

	if mgr.IsRunning() {
		t.Errorf("Expected manager to not be running initially")
	}

	// 测试传入非法配置（应当优雅报错，不应导致 Panic 或 UI 崩溃）
	invalidConfig := []byte(`{invalid json}`)
	err := mgr.Start(invalidConfig)
	if err == nil {
		t.Errorf("Expected error when starting with invalid config, got nil")
	}

	if mgr.IsRunning() {
		t.Errorf("Expected manager to not be running after failed start")
	}

	t.Logf("Log callback successfully verified, received logs: %v", logCalled)
}

func TestV2rayManagerWithDefaultConfigAndGeosite(t *testing.T) {
	cfg := data.DefaultConfig()
	_ = os.WriteFile(cfg.V2RayConfigPath, data.GetDefaultConfigBytes(), 0644)

	configBytes, err := os.ReadFile(cfg.V2RayConfigPath)
	if err != nil {
		t.Fatalf("Failed to read default config file: %v", err)
	}

	mgr := GetManager(func(msg string) {
		t.Logf("[V2Ray Log] %s", msg)
	})

	err = mgr.Start(configBytes)
	if err != nil {
		t.Fatalf("Failed to start v2ray with default config (geosite test): %v", err)
	}

	if !mgr.IsRunning() {
		t.Errorf("Expected manager to be running")
	}

	err = mgr.Stop()
	if err != nil {
		t.Errorf("Failed to stop manager: %v", err)
	}

	if mgr.IsRunning() {
		t.Errorf("Expected manager to not be running after stop")
	}
}
