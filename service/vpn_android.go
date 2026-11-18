//go:build android

package service

import (
	"log"

	"github.com/gzjjjfree/goout/jni"
)

var (
	androidFD int = -1
)

// StartVPN initializes Android VpnService TUN interface and tun2socks packet forwarding.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	log.Printf("[Android-VPN] Starting VpnService TUN tunnel: IP=%s, Gateway=%s, SOCKS=%s", v.tunIP, v.gateway, socksAddr)

	simulatedFD := androidFD
	log.Printf("[Android-VPN] Calling jni.StartTun2Socks with FD=%d, tunIP=%s, gateway=%s, socksAddr=%s", simulatedFD, v.tunIP, v.gateway, socksAddr)

	jni.StartTun2Socks(simulatedFD, v.tunIP, v.gateway, socksAddr)

	v.active = true
	log.Printf("[Android-VPN] VPN started successfully, active=true")
	return nil
}

// StartVPNWithFD allows Android Java/Kotlin host to pass the real VpnService file descriptor (FD) once established.
//
//export StartVPNWithFD
func StartVPNWithFD(fd int, socksAddr string) {
	log.Printf("[Android-VPN] Received real VpnService FD=%d from Android host", fd)
	androidFD = fd
}

// JNI export for GoOutVpnService.java nativeStartVPNWithFD
//
//export Java_com_gzjjjfree_goout_GoOutVpnService_nativeStartVPNWithFD
func Java_com_gzjjjfree_goout_GoOutVpnService_nativeStartVPNWithFD(env uintptr, thiz uintptr, fd int, socksAddrPtr uintptr) {
	log.Printf("[Android-JNI] nativeStartVPNWithFD called from Android VpnService with FD=%d", fd)
	androidFD = fd
}

// StopVPN stops Android VpnService TUN tunnel.
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[Android-VPN] Stopping Android VpnService TUN tunnel")
	jni.StopTun2Socks()
	androidFD = -1
	v.active = false
	return nil
}
