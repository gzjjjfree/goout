package service

import (
	"sync"

	"github.com/gzjjjfree/goout/jni"
)

// VPNServiceManager handles Android VpnService lifecycle and routing configuration.
type VPNServiceManager struct {
	mu        sync.Mutex
	active    bool
	tunIP     string
	gateway   string
	dnsServer string
}

func NewVPNServiceManager(tunIP, gateway string) *VPNServiceManager {
	return &VPNServiceManager{
		tunIP:     tunIP,
		gateway:   gateway,
		dnsServer: "8.8.8.8",
	}
}

func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	// On Android, VpnService establishes a TUN interface and returns a file descriptor (fd).
	// Here we simulate or interface with Android system VpnService builder via JNI / platform API.
	// For cross-platform compilation/testing, we fallback or handle fd appropriately.
	simulatedFD := -1 // In actual Android environment, obtain fd from VpnService.Builder()

	// Call tun2socks JNI integration
	jni.StartTun2Socks(simulatedFD, v.tunIP, v.gateway, socksAddr)

	v.active = true
	return nil
}

func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	// Stop tun2socks / cleanup TUN interface
	v.active = false
	return nil
}

func (v *VPNServiceManager) IsRunning() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.active
}


