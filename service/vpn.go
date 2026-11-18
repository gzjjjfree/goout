package service

import (
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
