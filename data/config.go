package data

import (
	_ "embed"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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

func getWritableDir(subPath ...string) string {
	candidates := []string{}

	if cache, err := os.UserCacheDir(); err == nil && cache != "" && cache != "/" {
		if strings.HasPrefix(cache, "/data") {
			pkgDir := filepath.Dir(cache)
			candidates = append(candidates, filepath.Join(append([]string{pkgDir, "files"}, subPath...)...))
		}
		candidates = append(candidates, filepath.Join(append([]string{cache}, subPath...)...))
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/" {
		if strings.HasPrefix(home, "/data") {
			pkgDir := home
			if filepath.Base(home) == "cache" {
				pkgDir = filepath.Dir(home)
			}
			candidates = append(candidates, filepath.Join(append([]string{pkgDir, "files"}, subPath...)...))
		} else {
			candidates = append(candidates, filepath.Join(append([]string{home, ".goout"}, subPath...)...))
		}
	}

	if tmp := os.TempDir(); tmp != "" && tmp != "/" {
		candidates = append(candidates, filepath.Join(append([]string{tmp, "goout"}, subPath...)...))
	}

	candidates = append(candidates, filepath.Join(append([]string{"goout"}, subPath...)...))

	for _, dir := range candidates {
		if err := os.MkdirAll(dir, 0700); err == nil {
			testFile := filepath.Join(dir, ".write_test")
			if err := os.WriteFile(testFile, []byte("test"), 0600); err == nil {
				_ = os.Remove(testFile)
				return dir
			}
		}
	}

	fallback := filepath.Join(append([]string{os.TempDir(), "goout"}, subPath...)...)
	_ = os.MkdirAll(fallback, 0700)
	return fallback
}

func DefaultConfig() *Config {
	configDir := getWritableDir()
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
