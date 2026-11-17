//go:build darwin

package service

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/gzjjjfree/goout/jni"
)

var (
	utunFile *os.File
	utunName string
)

const (
	sysprotoControl = 2
	utunOptIfname   = 2
)

func getUtunName(fd uintptr) (string, error) {
	var ifname [64]byte
	l := uint32(len(ifname))
	_, _, errno := syscall.Syscall6(
		syscall.SYS_GETSOCKOPT,
		fd,
		uintptr(sysprotoControl),
		uintptr(utunOptIfname),
		uintptr(unsafe.Pointer(&ifname[0])),
		uintptr(unsafe.Pointer(&l)),
		0,
	)
	if errno != 0 {
		return "", errno
	}
	name := string(ifname[:l])
	for i, c := range name {
		if c == 0 {
			name = name[:i]
			break
		}
	}
	return name, nil
}

// StartVPN initializes macOS UTUN interface and routing table for global proxy with real file descriptor.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	log.Printf("[macOS-VPN] Starting macOS UTUN interface & routing table: IP=%s, Gateway=%s, SOCKS=%s", v.tunIP, v.gateway, socksAddr)

	// 打开 /dev/utun 获取真实的 TUN 文件描述符
	file, err := os.OpenFile("/dev/utun", os.O_RDWR, 0)
	if err != nil {
		log.Printf("[macOS-VPN] Failed to open /dev/utun: %v (requires root/administrative privileges or simulation mode)", err)
	} else {
		utunFile = file
	}

	simulatedFD := -1
	if utunFile != nil {
		simulatedFD = int(utunFile.Fd())
		// 动态获取分配的 utun 接口名称（如 utun0, utun1 等）
		if name, err := getUtunName(utunFile.Fd()); err == nil && name != "" {
			utunName = name
			log.Printf("[macOS-VPN] Allocated UTUN interface name: %s", utunName)
		} else {
			utunName = "utun3" // 如果 getsockopt 失败的兜底名称
			log.Printf("[macOS-VPN] Failed to get UTUN interface name via getsockopt: %v, falling back to %s", err, utunName)
		}
	} else {
		utunName = "utun3"
	}

	// 配置 macOS 路由 / ifconfig
	ifconfigCmdStr := fmt.Sprintf("ifconfig %s %s %s up", utunName, v.tunIP, v.gateway)
	log.Printf("[macOS-VPN] Executing command: %s", ifconfigCmdStr)
	cmdIfconfig := exec.Command("ifconfig", utunName, v.tunIP, v.gateway, "up")
	if err := cmdIfconfig.Run(); err != nil {
		log.Printf("[macOS-VPN] ifconfig setup notice: %v", err)
	} else {
		log.Printf("[macOS-VPN] ifconfig executed successfully.")
	}

	// 设置默认路由指向 VPN 网关
	routeCmd := exec.Command("route", "change", "default", v.gateway)
	log.Printf("[macOS-VPN] Executing command: route change default %s", v.gateway)
	if err := routeCmd.Run(); err != nil {
		log.Printf("[macOS-VPN] route change default notice: %v, trying route add...", err)
		cmdRoute := exec.Command("route", "add", "default", v.gateway)
		if errAdd := cmdRoute.Run(); errAdd != nil {
			log.Printf("[macOS-VPN] route add default error: %v", errAdd)
		} else {
			log.Printf("[macOS-VPN] route added successfully.")
		}
	} else {
		log.Printf("[macOS-VPN] route changed default successfully.")
	}

	log.Printf("[macOS-VPN] Calling jni/RunTunEngine with real FD=%d, tunIP=%s, gateway=%s, socksAddr=%s", simulatedFD, v.tunIP, v.gateway, socksAddr)

	// 如果成功获取了真实的 utun 文件句柄，启动数据包读写转发引擎
	if utunFile != nil {
		go jni.RunTunEngine(utunFile, v.tunIP, v.gateway, socksAddr)
		log.Println("[macOS-VPN] RunTunEngine started successfully for macOS UTUN.")
	} else {
		jni.StartTun2Socks(simulatedFD, v.tunIP, v.gateway, socksAddr)
	}

	v.active = true
	log.Printf("[macOS-VPN] VPN started successfully, active=true")
	return nil
}

// StopVPN stops macOS UTUN tunnel and restores system routing.
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[macOS-VPN] Stopping macOS UTUN tunnel & restoring default routing")

	if utunFile != nil {
		_ = utunFile.Close()
		utunFile = nil
	}

	cmdRoute := exec.Command("route", "delete", "default")
	if err := cmdRoute.Run(); err != nil {
		log.Printf("[macOS-VPN] route restore error: %v", err)
	}

	v.active = false
	return nil
}
