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

	// Wintun transmit buffer and send procedure used to write packets back into the virtual adapter.
	wintunTransmitBuffer unsafe.Pointer
	wintunTransmitSize   uint32
	sendPacketProc       *syscall.Proc
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
					go startWintunPacketLoop(session, v.tunIP, v.gateway, socksAddr)
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

func startWintunPacketLoop(session uintptr, tunIP, gateway, socksAddr string) {
	log.Printf("[Wintun-Tunnel] Starting Wintun packet reading loop for SOCKS5 proxy: %s", socksAddr)
	dll, err := syscall.LoadDLL("wintun.dll")
	if err != nil {
		log.Printf("[Wintun-Tunnel] Failed to load wintun.dll for packet loop: %v", err)
		return
	}
	defer dll.Release()

	getReadWaitEvent, _ := dll.FindProc("WintunGetReadWaitEvent")
	receivePacket, _ := dll.FindProc("WintunReceivePacket")
	receiveReleasePacket, _ := dll.FindProc("WintunReleaseReceivePacket")
	getTransmitBuffer, _ := dll.FindProc("WintunGetTransmitBuffer")
	sendPacket, _ := dll.FindProc("WintunSendPacket")

	if getReadWaitEvent == nil || receivePacket == nil || receiveReleasePacket == nil {
		log.Println("[Wintun-Tunnel] Failed to find required Wintun packet procedures")
		return
	}

	readWaitEvent, _, _ := getReadWaitEvent.Call(session)
	if readWaitEvent == 0 {
		log.Println("[Wintun-Tunnel] Failed to get Wintun read wait event")
		return
	}

	// Acquire the transmit buffer so we can write packets back into the adapter.
	if getTransmitBuffer != nil && sendPacket != nil {
		var transmitSize uint32
		transmitPtr, _, _ := getTransmitBuffer.Call(session, uintptr(unsafe.Pointer(&transmitSize)))
		if transmitPtr != 0 && transmitSize > 0 {
			wintunTransmitBuffer = unsafe.Pointer(transmitPtr)
			wintunTransmitSize = transmitSize
			sendPacketProc = sendPacket
			log.Printf("[Wintun-Tunnel] Wintun transmit buffer ready (size=%d)", transmitSize)
		} else {
			log.Println("[Wintun-Tunnel] Failed to acquire Wintun transmit buffer; send path disabled")
		}
	}

	for wintunRunning {
		ret := waitForSingleObject(readWaitEvent, 50)
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
				// Clone the payload because releasing invalidates the ring buffer memory.
				copied := make([]byte, packetSize)
				copy(copied, unsafe.Slice((*byte)(unsafe.Pointer(packetPtr)), packetSize))
				go processWintunPacket(session, tunIP, gateway, socksAddr, copied)
			}
			receiveReleasePacket.Call(session, packetPtr)
		}
	}
	log.Println("[Wintun-Tunnel] Wintun packet loop stopped.")
}

// processWintunPacket forwards a cloned IP packet through the shared forwarding engine and
// writes any ICMP reply back into the Wintun transmit buffer using correct dynamic allocation.
func processWintunPacket(session uintptr, tunIP, gateway, socksAddr string, packet []byte) {
	jni.ForwardPacket(packet, tunIP, gateway, socksAddr, func(reply []byte) {
		// 加载动态发送所需的 proc
		dll, err := syscall.LoadDLL("wintun.dll")
		if err != nil {
			return
		}
		defer dll.Release()

		getTransmitBuffer, _ := dll.FindProc("WintunGetTransmitBuffer")
		sendPacket, _ := dll.FindProc("WintunSendPacket")
		if getTransmitBuffer == nil || sendPacket == nil {
			return
		}

		var transmitSize uint32 = uint32(len(reply))
		// 每次发送前必须实时向 Wintun 申请一块对应大小的发送缓冲区
		transmitPtr, _, _ := getTransmitBuffer.Call(session, uintptr(transmitSize))
		if transmitPtr == 0 {
			return
		}

		// 将回复包数据拷贝到 Wintun 分配的内存中
		destSlice := unsafe.Slice((*byte)(unsafe.Pointer(transmitPtr)), transmitSize)
		copy(destSlice, reply)

		// 调用 WintunSendPacket 提交发送
		sendPacket.Call(session, transmitPtr)
	})
}

// StopVPN stops Windows Wintun tunnel and restores system routing immediately.
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[Windows-VPN] Stopping Windows Wintun tunnel & restoring default routing immediately")

	v.active = false

	if wintunRunning {
		wintunRunning = false
		session := wintunSession
		adapter := wintunAdapter
		wintunSession = 0
		wintunAdapter = 0
		go func() {
			if wintunDll, err := syscall.LoadDLL("wintun.dll"); err == nil {
				if endSession, err := wintunDll.FindProc("WintunEndSession"); err == nil && endSession != nil {
					_, _, _ = endSession.Call(session)
				}
				if closeAdapter, err := wintunDll.FindProc("WintunCloseAdapter"); err == nil && closeAdapter != nil {
					_, _, _ = closeAdapter.Call(adapter)
				}
				_ = wintunDll.Release()
			}
		}()
	}

	go func() {
		cmd := exec.Command("netsh", "interface", "ipv4", "delete", "interface", "GoOutTUN")
		_ = cmd.Run()
	}()

	return nil
}
