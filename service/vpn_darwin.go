//go:build darwin

package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
)

func runCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// getActiveNetworkServices returns a list of network services on macOS
func getActiveNetworkServices() []string {
	out, err := runCommand("networksetup", "-listallnetworkservices")
	if err != nil {
		return []string{"Wi-Fi", "Ethernet"}
	}
	lines := strings.Split(string(out), "\n")
	var services []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "An asterisk") {
			continue
		}
		services = append(services, line)
	}
	if len(services) == 0 {
		services = []string{"Wi-Fi", "Ethernet"}
	}
	return services
}

// StartVPN enables macOS System Proxy pointing to the local proxy address (mirroring Windows System Proxy implementation).
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	log.Printf("[macOS-SystemProxy] Enabling macOS System Proxy: %s", socksAddr)

	isHttp := false
	host := "127.0.0.1"
	port := "10808"
	if parts := strings.Split(socksAddr, ":"); len(parts) == 2 {
		host = parts[0]
		port = parts[1]
	}

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
								if p, ok := ibMap["port"].(float64); ok && p > 0 {
									port = fmt.Sprintf("%d", int(p))
								} else if p, ok := ibMap["port"].(int); ok && p > 0 {
									port = fmt.Sprintf("%d", p)
								}
								break
							}
						}
					}
				}
			}
		}
	}

	services := getActiveNetworkServices()
	for _, svc := range services {
		if isHttp {
			_ = exec.Command("networksetup", "-setwebproxy", svc, host, port).Run()
			_ = exec.Command("networksetup", "-setwebproxystate", svc, "on").Run()
			_ = exec.Command("networksetup", "-setsecurewebproxy", svc, host, port).Run()
			_ = exec.Command("networksetup", "-setsecurewebproxystate", svc, "on").Run()
			log.Printf("[macOS-SystemProxy] Configured Web/SecureWeb proxy on %s -> %s:%s", svc, host, port)
		} else {
			_ = exec.Command("networksetup", "-setsocksfirewallproxy", svc, host, port).Run()
			_ = exec.Command("networksetup", "-setsocksfirewallproxystate", svc, "on").Run()
			log.Printf("[macOS-SystemProxy] Configured SOCKS firewall proxy on %s -> %s:%s", svc, host, port)
		}
	}

	v.socksHost = socksAddr
	v.active = true
	log.Println("[macOS-SystemProxy] macOS System Proxy enabled successfully.")
	return nil
}

// StopVPN disables macOS System Proxy and restores direct connection.
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[macOS-SystemProxy] Disabling macOS System Proxy...")

	services := getActiveNetworkServices()
	for _, svc := range services {
		_ = exec.Command("networksetup", "-setwebproxystate", svc, "off").Run()
		_ = exec.Command("networksetup", "-setsecurewebproxystate", svc, "off").Run()
		_ = exec.Command("networksetup", "-setsocksfirewallproxystate", svc, "off").Run()
	}

	v.active = false
	log.Println("[macOS-SystemProxy] macOS System Proxy disabled successfully.")
	return nil
}
