//go:build darwin

package service

import (
	"fmt"
	"log"
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

	isHttp, host, port := CheckHttpInbound(socksAddr)

	services := getActiveNetworkServices()
	successCount := 0
	var lastErr error

	for _, svc := range services {
		var err error
		if isHttp {
			if err = exec.Command("networksetup", "-setwebproxy", svc, host, port).Run(); err != nil {
				lastErr = err
				log.Printf("[macOS-SystemProxy] Failed to set web proxy on %s: %v", svc, err)
			}
			if err = exec.Command("networksetup", "-setwebproxystate", svc, "on").Run(); err != nil {
				lastErr = err
				log.Printf("[macOS-SystemProxy] Failed to set web proxy state on for %s: %v", svc, err)
			}
			if err = exec.Command("networksetup", "-setsecurewebproxy", svc, host, port).Run(); err != nil {
				lastErr = err
				log.Printf("[macOS-SystemProxy] Failed to set secure web proxy on %s: %v", svc, err)
			}
			if err = exec.Command("networksetup", "-setsecurewebproxystate", svc, "on").Run(); err != nil {
				lastErr = err
				log.Printf("[macOS-SystemProxy] Failed to set secure web proxy state on for %s: %v", svc, err)
			}
			log.Printf("[macOS-SystemProxy] Configured Web/SecureWeb proxy on %s -> %s:%s", svc, host, port)
		} else {
			if err = exec.Command("networksetup", "-setsocksfirewallproxy", svc, host, port).Run(); err != nil {
				lastErr = err
				log.Printf("[macOS-SystemProxy] Failed to set socks firewall proxy on %s: %v", svc, err)
			}
			if err = exec.Command("networksetup", "-setsocksfirewallproxystate", svc, "on").Run(); err != nil {
				lastErr = err
				log.Printf("[macOS-SystemProxy] Failed to set socks firewall proxy state on for %s: %v", svc, err)
			}
			log.Printf("[macOS-SystemProxy] Configured SOCKS firewall proxy on %s -> %s:%s", svc, host, port)
		}
		if err == nil {
			successCount++
		}
	}

	if len(services) > 0 && successCount == 0 && lastErr != nil {
		log.Printf("[macOS-SystemProxy] Failed to configure system proxy on any network service: %v", lastErr)
		return fmt.Errorf("failed to configure macOS system proxy: %w", lastErr)
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
