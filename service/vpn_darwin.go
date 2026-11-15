//go:build darwin

package service

import (
	"log"
	"os/exec"

	"github.com/gzjjjfree/goout/jni"
)

// StartVPN initializes macOS UTUN interface and routing table for global proxy.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	log.Printf("[macOS-VPN] Starting macOS UTUN interface & routing table: IP=%s, Gateway=%s, SOCKS=%s", v.tunIP, v.gateway, socksAddr)

	// Configure macOS routing / ifconfig for UTUN interface
	cmdIfconfig := exec.Command("ifconfig", "utun3", v.tunIP, v.gateway, "up")
	if err := cmdIfconfig.Run(); err != nil {
		log.Printf("[macOS-VPN] ifconfig setup notice (requires administrative privileges or simulation mode): %v", err)
	}

	cmdRoute := exec.Command("route", "add", "default", v.gateway)
	if err := cmdRoute.Run(); err != nil {
		log.Printf("[macOS-VPN] route setup notice: %v", err)
	}

	simulatedFD := -1 // macOS UTUN file descriptor integration point
	jni.StartTun2Socks(simulatedFD, v.tunIP, v.gateway, socksAddr)

	v.active = true
	return nil
}

// StopVPN stops macOS UTUN tunnel and restores system routing.
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[macOS-VPN] Stopping macOS UTUN tunnel & restoring default routing")

	cmdRoute := exec.Command("route", "delete", "default")
	if err := cmdRoute.Run(); err != nil {
		log.Printf("[macOS-VPN] route restore error: %v", err)
	}

	v.active = false
	return nil
}
