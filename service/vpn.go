package service

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/gzjjjfree/goout/util"
)

// CheckHttpInbound checks if an HTTP inbound is configured in any of the standard config files,
// returning whether it's HTTP, along with the proxy host and port.
func CheckHttpInbound(socksAddr string) (bool, string, string) {
	host := "127.0.0.1"
	port := "10808"
	if parts := strings.Split(socksAddr, ":"); len(parts) == 2 {
		host = parts[0]
		port = parts[1]
	}

	isHttp := false
	proxyHost := host
	proxyPort := port

	configPaths := []string{"goout.json", "data/goout.json", "config_save.json"}
	for _, path := range configPaths {
		if content, err := os.ReadFile(path); err == nil {
			var raw map[string]interface{}
			if err := json.Unmarshal(content, &raw); err == nil {
				if inbounds, ok := raw["inbounds"].([]interface{}); ok {
					for _, ib := range inbounds {
						if ibMap, ok := ib.(map[string]interface{}); ok {
							if proto, ok := ibMap["protocol"].(string); ok && proto == "http" {
								isHttp = true
								if p := util.ParsePortToInt(ibMap["port"]); p > 0 {
									proxyPort = fmt.Sprintf("%d", p)
								}
								break
							}
						}
					}
				}
			}
		}
	}
	return isHttp, proxyHost, proxyPort
}

// VPNServiceManager handles cross-platform VPN/TUN lifecycle and routing configuration.
type VPNServiceManager struct {
	mu        sync.Mutex
	active    bool
	tunIP     string
	gateway   string
	socksHost string
	onLog     func(string)
}

func NewVPNServiceManager(tunIP, gateway string, onLog func(string)) *VPNServiceManager {
	return &VPNServiceManager{
		tunIP:   tunIP,
		gateway: gateway,
		onLog:   onLog,
	}
}

func (v *VPNServiceManager) SetTunIP(tunIP string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.tunIP = tunIP
}

func (v *VPNServiceManager) IsRunning() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.active
}
