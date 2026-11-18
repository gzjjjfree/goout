//go:build windows

package service

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"

	"github.com/gzjjjfree/goout/data"
	"github.com/gzjjjfree/goout/jni"
)

func getDefaultGatewayWindows(excludeGateway string) string {
	// 1. 尝试通过 route print 获取默认网关（兼容中英文 Windows 输出）
	out, err := exec.Command("route", "print", "0.0.0.0").Output()
	if err == nil {
		lines := strings.Split(string(out), "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "0.0.0.0") || strings.Contains(line, "0.0.0.0") {
				fields := strings.Fields(line)
				for _, field := range fields {
					ip := net.ParseIP(field)
					if ip != nil && ip.To4() != nil && field != "0.0.0.0" && field != "255.255.255.255" && field != excludeGateway {
						return field
					}
				}
			}
		}
	}

	// 2. 备用方案：通过 PowerShell 获取默认网关（绝对可靠且与语言无关）
	psCmd := "Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Sort-Object Metric | Select-Object -ExpandProperty NextHop -First 1"
	outPS, errPS := exec.Command("powershell", "-Command", psCmd).Output()
	if errPS == nil {
		gw := strings.TrimSpace(string(outPS))
		ip := net.ParseIP(gw)
		if ip != nil && ip.To4() != nil && gw != "0.0.0.0" && gw != excludeGateway {
			return gw
		}
	}

	return ""
}

// StartVPN initializes Windows Wintun virtual network adapter and routing table for global proxy using tun2socks engine.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	// 关键：用 recover 区分正常退出与 panic
	defer func() {
		if r := recover(); r != nil { // 只有 panic 时 r 才非 nil
			log.Printf("[Windows-VPN] Panic recovered: %v", r)
			// err := fmt.Errorf("StartVPN panicked, cleaning up routing: %v", r)
			v.cleanupRouting() // ← 仅在此处清理残留路由
		}
	}()

	if v.active {
		return nil
	}

	jni.InitProxyContext()

	log.Printf("[Windows-VPN] Starting Wintun TUN interface & routing table: IP=%s, Gateway=%s, SOCKS=%s", v.tunIP, v.gateway, socksAddr)

	data.EnsureWintunDLL()
	if fi, err := os.Stat("wintun.dll"); err == nil {
		log.Printf("[Windows-VPN] wintun.dll verified: size=%d bytes", fi.Size())
	} else {
		log.Printf("[Windows-VPN] WARNING: wintun.dll stat failed: %v", err)
	}

	// 检查管理员权限
	if cmdAdmin := exec.Command("net", "session"); cmdAdmin.Run() != nil {
		log.Println("[Windows-VPN] CRITICAL WARNING: Not running as Administrator! Wintun creation will likely fail or crash.")
	} else {
		log.Println("[Windows-VPN] Running with Administrator privileges confirmed.")
	}

	v.originalGateway = getDefaultGatewayWindows(v.gateway)
	log.Printf("[Windows-VPN] Recorded original default gateway: %s", v.originalGateway)

	// Start tun2socks engine with tun://GoOutTUN
	if err := jni.StartTun2Socks(-1, v.tunIP, v.gateway, socksAddr); err != nil {
		log.Printf("[Windows-VPN] tun2socks engine failed to start: %v", err)
		v.cleanupRouting()
		return fmt.Errorf("start tun2socks: %w", err)
	}

	// Configure Windows routing / netsh interface address for GoOutTUN
	cmdStr := fmt.Sprintf("netsh interface ipv4 set address name=GoOutTUN source=static addr=%s mask=255.255.255.0 gateway=%s", v.tunIP, v.gateway)
	log.Printf("[Windows-VPN] Executing command: %s", cmdStr)
	cmd := exec.Command("netsh", "interface", "ipv4", "set", "address", "name=GoOutTUN", "source=static", "addr="+v.tunIP, "mask=255.255.255.0", "gateway="+v.gateway)
	if err := cmd.Run(); err != nil {
		log.Printf("[Windows-VPN] netsh execution warning/error: %v (Note: if GoOutTUN interface does not exist yet, this may fail until created)", err)
	} else {
		log.Printf("[Windows-VPN] netsh executed successfully.")
	}

	_ = exec.Command("route", "delete", "0.0.0.0", "mask", "0.0.0.0", v.gateway).Run()

	routeCmd := exec.Command("route", "add", "0.0.0.0", "mask", "0.0.0.0", v.gateway, "metric", "5")
	log.Printf("[Windows-VPN] Executing route command: route add 0.0.0.0 mask 0.0.0.0 %s metric 5", v.gateway)
	if errRoute := routeCmd.Run(); errRoute != nil {
		log.Printf("[Windows-VPN] Failed to add default route: %v", errRoute)
	} else {
		log.Println("[Windows-VPN] Global default route added successfully to GoOutTUN.")
	}

	v.active = true
	log.Printf("[Windows-VPN] VPN started successfully, active=true")
	return nil
}

// cleanupRouting 删除崩溃残留的默认路由并恢复原始网关
func (v *VPNServiceManager) cleanupRouting() {
	_ = exec.Command("route", "delete", "0.0.0.0", "mask", "0.0.0.0", v.gateway).Run()
	if v.originalGateway != "" {
		_ = exec.Command("route", "add", "0.0.0.0", "mask", "0.0.0.0", v.originalGateway, "metric", "25").Run()
	}
}

// StopVPN stops Windows Wintun tunnel and restores system routing.
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[Windows-VPN] Stopping Windows Wintun tunnel instantly...")

	jni.CancelProxyContext()
	jni.StopTun2Socks()

	origGw := v.originalGateway
	v.originalGateway = ""
	go func() {
		_ = exec.Command("route", "delete", "0.0.0.0", "mask", "0.0.0.0", v.gateway).Run()
		if origGw != "" {
			_ = exec.Command("route", "add", "0.0.0.0", "mask", "0.0.0.0", origGw, "metric", "25").Run()
			log.Printf("[Windows-VPN] Restored original default gateway: %s", origGw)
		}
	}()

	v.active = false
	log.Println("[Windows-VPN] VPN stopped completely in milliseconds.")
	return nil
}
