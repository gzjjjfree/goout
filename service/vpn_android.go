//go:build android

package service

import (
	"log"

	"github.com/gzjjjfree/goout/jni"
)

// StartVPN initializes Android VpnService TUN interface and tun2socks packet forwarding.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	log.Printf("[Android-VPN] Starting VpnService TUN tunnel: IP=%s, Gateway=%s, SOCKS=%s", v.tunIP, v.gateway, socksAddr)

	simulatedFD := -1 // In actual Android runtime, obtained from VpnService.Builder().establish().getFd()
	jni.StartTun2Socks(simulatedFD, v.tunIP, v.gateway, socksAddr)

	v.active = true
	return nil
}

// StopVPN stops Android VpnService TUN tunnel.
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[Android-VPN] Stopping Android VpnService TUN tunnel")
	v.active = false
	return nil
}
