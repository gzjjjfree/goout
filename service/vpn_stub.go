//go:build !windows && !android && !darwin

package service

import (
	"fmt"
	"log"

	"github.com/gzjjjfree/goout/jni"
)

// StartVPN initiates cross-platform TUN simulation/stub for non-Windows/non-Android OS.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	log.Printf("[Stub-VPN] Starting cross-platform TUN simulation: IP=%s, Gateway=%s, SOCKS=%s", v.tunIP, v.gateway, socksAddr)
	if err := jni.StartTun2Socks(-1, v.tunIP, v.gateway, socksAddr); err != nil {
		log.Printf("[Stub-VPN] Failed to start tun2socks: %v", err)
		return fmt.Errorf("start tun2socks: %w", err)
	}

	v.active = true
	return nil
}

// StopVPN stops cross-platform TUN simulation.
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[Stub-VPN] Stopping cross-platform TUN simulation")
	jni.StopTun2Socks()
	v.active = false
	return nil
}
