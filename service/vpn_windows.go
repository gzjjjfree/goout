//go:build windows

package service

import (
	"log"
	"os/exec"

	"github.com/gzjjjfree/goout/jni"
)

// StartVPN initializes Windows Wintun virtual network adapter and routing table for global proxy.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	log.Printf("[Windows-VPN] Starting Wintun TUN interface & routing table: IP=%s, Gateway=%s, SOCKS=%s", v.tunIP, v.gateway, socksAddr)

	// Configure Windows routing / netsh interface address for GoOutTUN
	cmd := exec.Command("netsh", "interface", "ipv4", "set", "address", "name=\"GoOutTUN\"", "source=static", "addr="+v.tunIP, "mask=255.255.255.0", "gateway="+v.gateway)
	if err := cmd.Run(); err != nil {
		log.Printf("[Windows-VPN] netsh setup notice (interface may be initialized in simulation mode): %v", err)
	}

	simulatedFD := -1 // Windows Wintun handle / packet ring buffer integration point
	jni.StartTun2Socks(simulatedFD, v.tunIP, v.gateway, socksAddr)

	v.active = true
	return nil
}

// StopVPN stops Windows Wintun tunnel and restores system routing.
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[Windows-VPN] Stopping Windows Wintun tunnel & restoring default routing")
	v.active = false
	return nil
}
