//go:build android

package jni

/*
#cgo LDFLAGS: -L. -ltun2socks
#include <stdlib.h>

// Example declaration for C-based tun2socks hook
// extern void run_tun2socks(int fd, const char* tun_addr, const char* gateway, const char* socks_server);
*/
import "C"
import (
	"log"
	"unsafe"
)

// StartTun2Socks initiates the tun2socks native bridge.
func StartTun2Socks(fd int, tunAddr, gateway, socksServer string) {
	log.Printf("[JNI] StartTun2Socks called: fd=%d, tun=%s, gateway=%s, socks=%s", fd, tunAddr, gateway, socksServer)

	cTunAddr := C.CString(tunAddr)
	defer C.free(unsafe.Pointer(cTunAddr))

	cGateway := C.CString(gateway)
	defer C.free(unsafe.Pointer(cGateway))

	cSocksServer := C.CString(socksServer)
	defer C.free(unsafe.Pointer(cSocksServer))

	// If C library is present and linked, call C function:
	// C.run_tun2socks(C.int(fd), cTunAddr, cGateway, cSocksServer)
}
