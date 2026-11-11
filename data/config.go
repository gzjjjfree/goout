package data

import (
	"encoding/json"
	"os"
	"path/filepath"
)

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
	return &Config{
		V2RayConfigPath: "/data/local/tmp/config.json",
		SocksPort:       10808,
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

