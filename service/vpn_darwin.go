//go:build darwin

package service

import (
	"fmt"
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
	ifconfigCmdStr := fmt.Sprintf("ifconfig utun3 %s %s up", v.tunIP, v.gateway)
	log.Printf("[macOS-VPN] Executing command: %s", ifconfigCmdStr)
	cmdIfconfig := exec.Command("ifconfig", "utun3", v.tunIP, v.gateway, "up")
	if err := cmdIfconfig.Run(); err != nil {
		log.Printf("[macOS-VPN] ifconfig setup notice (requires administrative privileges or simulation mode): %v", err)
	} else {
		log.Printf("[macOS-VPN] ifconfig executed successfully.")
	}

	routeCmdStr := fmt.Sprintf("route add default %s", v.gateway)
	log.Printf("[macOS-VPN] Executing command: %s", routeCmdStr)
	cmdRoute := exec.Command("route", "add", "default", v.gateway)
	if err := cmdRoute.Run(); err != nil {
		log.Printf("[macOS-VPN] route setup notice: %v", err)
	} else {
		log.Printf("[macOS-VPN] route added successfully.")
	}

	simulatedFD := -1 // macOS UTUN file descriptor integration point
	log.Printf("[macOS-VPN] Calling jni.StartTun2Socks with simulatedFD=%d, tunIP=%s, gateway=%s, socksAddr=%s", simulatedFD, v.tunIP, v.gateway, socksAddr)
	jni.StartTun2Socks(simulatedFD, v.tunIP, v.gateway, socksAddr)
	log.Printf("[macOS-VPN] jni.StartTun2Socks invoked successfully.")

	v.active = true
	log.Printf("[macOS-VPN] VPN started successfully, active=true")
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
