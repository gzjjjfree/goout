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

//go:embed geosite.dat
var embeddedGeosite []byte

// EnsureGeositeDat 确保 geosite.dat 在所有可能的可写存储目录、工作目录及可执行目录下可用，并设置 v2ray.location.asset 环境变量
func EnsureGeositeDat() {
	if len(embeddedGeosite) == 0 {
		return
	}

	dirs := []string{}
	writable := getWritableDir()
	if writable != "" {
		dirs = append(dirs, writable)
	}
	if wd, err := os.Getwd(); err == nil && wd != "" {
		dirs = append(dirs, wd)
	}
	if exe, err := os.Executable(); err == nil && exe != "" {
		dirs = append(dirs, filepath.Dir(exe))
	}

	var primaryDir string
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0755); err != nil {
			continue
		}
		p := filepath.Join(dir, "geosite.dat")
		if err := os.WriteFile(p, embeddedGeosite, 0644); err == nil {
			if primaryDir == "" {
				primaryDir = dir
			}
		}
	}

	// 同时在当前相对路径写入一份
	_ = os.WriteFile("geosite.dat", embeddedGeosite, 0644)

	if primaryDir != "" {
		_ = os.Setenv("v2ray.location.asset", primaryDir)
		_ = os.Setenv("V2RAY_LOCATION_ASSET", primaryDir)
	} else if writable != "" {
		_ = os.Setenv("v2ray.location.asset", writable)
		_ = os.Setenv("V2RAY_LOCATION_ASSET", writable)
	}
}

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

	if cache, err := os.UserCacheDir(); err == nil && cache != "" && filepath.IsAbs(cache) {
		if strings.HasPrefix(cache, "/data") {
			pkgDir := filepath.Dir(cache)
			if filepath.IsAbs(pkgDir) {
				candidates = append(candidates, filepath.Join(append([]string{pkgDir, "files"}, subPath...)...))
			}
		}
		candidates = append(candidates, filepath.Join(append([]string{cache}, subPath...)...))
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" && filepath.IsAbs(home) {
		if strings.HasPrefix(home, "/data") {
			pkgDir := home
			if filepath.Base(home) == "cache" {
				pkgDir = filepath.Dir(home)
			}
			if filepath.IsAbs(pkgDir) {
				candidates = append(candidates, filepath.Join(append([]string{pkgDir, "files"}, subPath...)...))
			}
		} else {
			candidates = append(candidates, filepath.Join(append([]string{home, ".goout"}, subPath...)...))
		}
	}

	if tmp := os.TempDir(); tmp != "" && filepath.IsAbs(tmp) {
		candidates = append(candidates, filepath.Join(append([]string{tmp, "goout"}, subPath...)...))
	}

	for _, dir := range candidates {
		if dir == "" || dir == "/" || !filepath.IsAbs(dir) {
			continue
		}
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

func GetDefaultConfigBytes() []byte {
	return defaultConfigFile
}
