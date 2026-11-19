package service

import (
	"encoding/json"
	"net"
	"os"
	"sync"
)

// VPNServiceManager handles cross-platform VPN/TUN lifecycle and routing configuration.
type VPNServiceManager struct {
	mu              sync.Mutex
	active          bool
	tunIP           string
	gateway         string
	dnsServer       string
	originalGateway string
	socksHost       string
	proxyHosts      []string
}

func NewVPNServiceManager(tunIP, gateway string) *VPNServiceManager {
	return &VPNServiceManager{
		tunIP:     tunIP,
		gateway:   gateway,
		dnsServer: "8.8.8.8",
	}
}

func (v *VPNServiceManager) IsRunning() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.active
}

func (v *VPNServiceManager) loadProxyHosts() []string {
	hosts := make(map[string]bool)

	// 1. Read data/result.json
	if resultBytes, err := os.ReadFile("data/result.json"); err == nil {
		var list []map[string]interface{}
		if err := json.Unmarshal(resultBytes, &list); err == nil {
			for _, item := range list {
				if addr, ok := item["address"].(string); ok && net.ParseIP(addr) != nil {
					hosts[addr] = true
				}
			}
		}
	}

	// 2. Read goout.json
	if gooutBytes, err := os.ReadFile("goout.json"); err == nil {
		var cfg map[string]interface{}
		if err := json.Unmarshal(gooutBytes, &cfg); err == nil {
			if outbounds, ok := cfg["outbounds"].([]interface{}); ok {
				for _, ob := range outbounds {
					if obMap, ok := ob.(map[string]interface{}); ok {
						if settings, ok := obMap["settings"].(map[string]interface{}); ok {
							if vnext, ok := settings["vnext"].([]interface{}); ok {
								for _, vn := range vnext {
									if vnMap, ok := vn.(map[string]interface{}); ok {
										if addr, ok := vnMap["address"].(string); ok && net.ParseIP(addr) != nil {
											hosts[addr] = true
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	if v.socksHost != "" && net.ParseIP(v.socksHost) != nil {
		hosts[v.socksHost] = true
	}

	var result []string
	for h := range hosts {
		result = append(result, h)
	}
	return result
}
