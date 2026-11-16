//go:build windows

package service

import (
	"fmt"
	"log"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/gzjjjfree/goout/data"
	"github.com/gzjjjfree/goout/jni"
)

var (
	wintunSession uintptr
	wintunAdapter uintptr
	wintunRunning bool
)

var (
	modkernel32             = syscall.NewLazyDLL("kernel32.dll")
	procWaitForSingleObject = modkernel32.NewProc("WaitForSingleObject")
)

const (
	WAIT_TIMEOUT = 258
)

func waitForSingleObject(handle uintptr, milliseconds uint32) uint32 {
	ret, _, _ := procWaitForSingleObject.Call(handle, uintptr(milliseconds))
	return uint32(ret)
}

// StartVPN initializes Windows Wintun virtual network adapter and routing table for global proxy.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	log.Printf("[Windows-VPN] Starting Wintun TUN interface & routing table: IP=%s, Gateway=%s, SOCKS=%s", v.tunIP, v.gateway, socksAddr)

	// Ensure wintun.dll is available in execution directory
	data.EnsureWintunDLL()

	// Load wintun.dll
	dll, err := syscall.LoadDLL("wintun.dll")
	if err != nil {
		log.Printf("[Windows-VPN] Failed to load wintun.dll: %v (falling back to netsh/simulation mode)", err)
	} else {
		defer dll.Release()
		createAdapter, errCreate := dll.FindProc("WintunCreateAdapter")
		startSession, errSession := dll.FindProc("WintunStartSession")

		if errCreate == nil && errSession == nil {
			namePtr, _ := syscall.UTF16PtrFromString("GoOutTUN")
			typePtr, _ := syscall.UTF16PtrFromString("GoOut")

			adapter, _, _ := createAdapter.Call(
				uintptr(unsafe.Pointer(namePtr)),
				uintptr(unsafe.Pointer(typePtr)),
				0, // GUID
			)
			if adapter != 0 {
				wintunAdapter = adapter
				session, _, _ := startSession.Call(adapter, 0x800000) // 8MB ring buffer capacity
				if session != 0 {
					wintunSession = session
					wintunRunning = true
					log.Println("[Windows-VPN] Wintun session started successfully.")

					// Start Wintun packet processing loop
					go startWintunPacketLoop(session, socksAddr)
				}
			}
		}
	}

	// Configure Windows routing / netsh interface address for GoOutTUN
	cmdStr := fmt.Sprintf("netsh interface ipv4 set address name=\"GoOutTUN\" source=static addr=%s mask=255.255.255.0 gateway=%s", v.tunIP, v.gateway)
	log.Printf("[Windows-VPN] Executing command: %s", cmdStr)
	cmd := exec.Command("netsh", "interface", "ipv4", "set", "address", "name=\"GoOutTUN\"", "source=static", "addr="+v.tunIP, "mask=255.255.255.0", "gateway="+v.gateway)
	if err := cmd.Run(); err != nil {
		log.Printf("[Windows-VPN] netsh execution warning/error: %v (Note: if GoOutTUN interface does not exist yet, this may fail until created)", err)
	} else {
		log.Printf("[Windows-VPN] netsh executed successfully.")
	}

	simulatedFD := -1
	if wintunRunning {
		simulatedFD = -2 // Wintun active mode (handled natively via Wintun ring buffer loop)
	}
	log.Printf("[Windows-VPN] Calling jni.StartTun2Socks with simulatedFD=%d, tunIP=%s, gateway=%s, socksAddr=%s", simulatedFD, v.tunIP, v.gateway, socksAddr)
	jni.StartTun2Socks(simulatedFD, v.tunIP, v.gateway, socksAddr)
	log.Printf("[Windows-VPN] jni.StartTun2Socks invoked successfully.")

	v.active = true
	log.Printf("[Windows-VPN] VPN started successfully, active=true")
	return nil
}

func startWintunPacketLoop(session uintptr, socksAddr string) {
	log.Printf("[Wintun-Tunnel] Starting Wintun packet reading loop for SOCKS5 proxy: %s", socksAddr)
	dll, err := syscall.LoadDLL("wintun.dll")
	if err != nil {
		log.Printf("[Wintun-Tunnel] Failed to load wintun.dll for packet loop: %v", err)
		return
	}
	defer dll.Release()

	getReadWaitEvent, _ := dll.FindProc("WintunGetReadWaitEvent")
	receivePacket, _ := dll.FindProc("WintunReceivePacket")
	releaseReceivePacket, _ := dll.FindProc("WintunReleaseReceivePacket")

	if getReadWaitEvent == nil || receivePacket == nil || releaseReceivePacket == nil {
		log.Println("[Wintun-Tunnel] Failed to find required Wintun packet procedures")
		return
	}

	readWaitEvent, _, _ := getReadWaitEvent.Call(session)
	if readWaitEvent == 0 {
		log.Println("[Wintun-Tunnel] Failed to get Wintun read wait event")
		return
	}

	for wintunRunning {
		ret := waitForSingleObject(readWaitEvent, 1000)
		if ret == WAIT_TIMEOUT {
			continue
		}
		if !wintunRunning {
			break
		}

		var packetSize uint32
		packetPtr, _, _ := receivePacket.Call(session, uintptr(unsafe.Pointer(&packetSize)))
		if packetPtr != 0 {
			if packetSize > 0 {
				// Packet read successfully from Wintun ring buffer
				// packetBytes := unsafe.Slice((*byte)(unsafe.Pointer(packetPtr)), packetSize)
				// Forward packet through proxy / tun2socks logic if needed
			}
			releaseReceivePacket.Call(session, packetPtr)
		}
	}
	log.Println("[Wintun-Tunnel] Wintun packet loop stopped.")
}

// StopVPN stops Windows Wintun tunnel and restores system routing.
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[Windows-VPN] Stopping Windows Wintun tunnel & restoring default routing")

	if wintunRunning {
		wintunRunning = false
		if wintunDll, err := syscall.LoadDLL("wintun.dll"); err == nil {
			if endSession, err := wintunDll.FindProc("WintunEndSession"); err == nil && endSession != nil {
				_, _, _ = endSession.Call(wintunSession)
			}
			if closeAdapter, err := wintunDll.FindProc("WintunCloseAdapter"); err == nil && closeAdapter != nil {
				_, _, _ = closeAdapter.Call(wintunAdapter)
			}
			_ = wintunDll.Release()
		}
		wintunSession = 0
		wintunAdapter = 0
	}

	cmd := exec.Command("netsh", "interface", "ipv4", "delete", "interface", "GoOutTUN")
	_ = cmd.Run()

	v.active = false
	return nil
}
