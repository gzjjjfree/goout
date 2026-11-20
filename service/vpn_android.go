//go:build android

package service

import (
	"fmt"
	"log"
	"sync"

	"github.com/gzjjjfree/goout/jni"
)

var (
	androidMu        sync.Mutex
	androidFD        int = -1
	protectFunc      func(int) bool
	activeVPN        *VPNServiceManager
	currentSocksAddr string
)

// StartVPN initializes Android VpnService TUN interface and tun2socks packet forwarding.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	androidMu.Lock()
	activeVPN = v
	currentSocksAddr = socksAddr
	simulatedFD := androidFD
	androidMu.Unlock()

	log.Printf("[Android-VPN] Starting VpnService TUN tunnel: IP=%s, Gateway=%s, SOCKS=%s", v.tunIP, v.gateway, socksAddr)
	log.Printf("[Android-VPN] Calling jni.StartTun2Socks with FD=%d, tunIP=%s, gateway=%s, socksAddr=%s", simulatedFD, v.tunIP, v.gateway, socksAddr)

	if err := jni.StartTun2Socks(simulatedFD, v.tunIP, v.gateway, socksAddr); err != nil {
		v.active = false
		log.Printf("[Android-VPN] Failed to start tun2socks: %v", err)
		return fmt.Errorf("start tun2socks: %w", err)
	}

	v.active = true
	log.Printf("[Android-VPN] VPN started successfully, active=true")
	return nil
}

// StartVPNWithFD allows Android Java/Kotlin host to pass the real VpnService file descriptor (FD) once established.
//
//export StartVPNWithFD
func StartVPNWithFD(fd int, socksAddr string) {
	log.Printf("[Android-VPN] Received real VpnService FD=%d from Android host (socks: %s)", fd, socksAddr)
	if socksAddr != "" {
		androidMu.Lock()
		currentSocksAddr = socksAddr
		androidMu.Unlock()
	}
	updateAndroidFD(fd)
}

// JNI export for GoOutVpnService.java nativeStartVPNWithFD
//
//export Java_com_gzjjjfree_goout_GoOutVpnService_nativeStartVPNWithFD
func Java_com_gzjjjfree_goout_GoOutVpnService_nativeStartVPNWithFD(env uintptr, thiz uintptr, fd int, socksAddrPtr uintptr) {
	log.Printf("[Android-JNI] nativeStartVPNWithFD called from Android VpnService with FD=%d", fd)
	updateAndroidFD(fd)
}

func updateAndroidFD(fd int) {
	androidMu.Lock()
	androidFD = fd
	v := activeVPN
	socks := currentSocksAddr
	androidMu.Unlock()

	if v != nil {
		v.mu.Lock()
		defer v.mu.Unlock()
		if v.active {
			log.Printf("[Android-VPN] VPN is active and new FD=%d received. Restarting tun2socks with real FD...", fd)
			jni.StopTun2Socks()
			if socks == "" {
				socks = "127.0.0.1:10808"
			}
			if err := jni.StartTun2Socks(fd, v.tunIP, v.gateway, socks); err != nil {
				log.Printf("[Android-VPN] Failed to restart tun2socks with real FD=%d: %v", fd, err)
				v.active = false
				return
			}
			log.Printf("[Android-VPN] tun2socks restarted successfully with real FD=%d", fd)
		}
	}
}

// RegisterProtectCallback registers a callback function from Java/Kotlin to protect socket fds via VpnService.protect(fd).
func RegisterProtectCallback(fn func(int) bool) {
	protectFunc = fn
}

// ProtectSocket calls the registered protect function to exempt a socket FD from VPN routing.
//export ProtectSocket
func ProtectSocket(fd int) bool {
	if protectFunc != nil {
		success := protectFunc(fd)
		log.Printf("[Android-VPN] ProtectSocket fd=%d, success=%v", fd, success)
		return success
	}
	log.Printf("[Android-VPN] ProtectSocket fd=%d, no protect callback registered", fd)
	return false
}

// JNI export for Java-side socket protection registration if needed
//
//export Java_com_gzjjjfree_goout_GoOutVpnService_nativeRegisterProtect
func Java_com_gzjjjfree_goout_GoOutVpnService_nativeRegisterProtect(env uintptr, thiz uintptr) {
	log.Println("[Android-JNI] nativeRegisterProtect called")
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

	androidMu.Lock()
	androidFD = -1
	activeVPN = nil
	androidMu.Unlock()

	v.active = false
	return nil
}
