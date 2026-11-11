//go:build !android

package jni

import "log"

// StartTun2Socks stub for non-Android platforms (e.g. Linux/macOS/Windows host testing).
func StartTun2Socks(fd int, tunAddr, gateway, socksServer string) {
	log.Printf("[JNI-Stub] StartTun2Socks (stub): fd=%d, tun=%s, gateway=%s, socks=%s", fd, tunAddr, gateway, socksServer)
}
