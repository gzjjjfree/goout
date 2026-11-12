package data

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
)

//go:embed goout.json
var defaultConfigFile []byte

type Config struct {
	V2RayConfigPath string   `json:"v2ray_config_path"`
	SocksPort       int      `json:"socks_port"`
	TunIP           string   `json:"tun_ip"`
	ServerAddress   string   `json:"server_address"`
	ServerPort      int      `json:"server_port"`
	Subscriptions   []string `json:"subscriptions"`
	RoutingBypass   []string `json:"routing_bypass"`
}

func DefaultConfig() *Config {
	homeDir, err := os.UserHomeDir()
	if err != nil || homeDir == "" {
		homeDir = os.TempDir()
	}
	var configDir string
	if filepath.Base(homeDir) == "cache" {
		configDir = filepath.Join(filepath.Dir(homeDir), "files")
	} else {
		configDir = filepath.Join(homeDir, "files")
		if _, err := os.Stat(configDir); os.IsNotExist(err) {
			configDir = filepath.Join(homeDir, "goout")
		}
	}
	_ = os.MkdirAll(configDir, 0755)
	configPath := filepath.Join(configDir, "goout.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		_ = os.WriteFile(configPath, defaultConfigFile, 0644)
	}

	return &Config{
		V2RayConfigPath: configPath,
		SocksPort:       10818,
		TunIP:           "10.0.0.2",
		ServerAddress:   "127.0.0.1",
		ServerPort:      10809,
		Subscriptions:   []string{},
		RoutingBypass:   []string{"10.0.0.0/8", "192.168.0.0/16"},
	}
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultConfig(), err
	}
	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func SaveConfig(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
