//go:build windows

package service

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gzjjjfree/goout/data"
	"github.com/gzjjjfree/goout/jni"
)

// getInterfaceIndexByName 获取指定网卡名称的 Windows 接口索引 (ifIndex)
func getInterfaceIndexByName(alias string) string {
	psCmd := fmt.Sprintf("(Get-NetAdapter -Name '%s' -ErrorAction SilentlyContinue).ifIndex", alias)
	out, err := exec.Command("powershell", "-Command", psCmd).Output()
	if err == nil {
		idx := strings.TrimSpace(string(out))
		if idx != "" {
			return idx
		}
	}
	return ""
}

// getDefaultGatewayWindows 获取物理网卡的默认网关，用于为代理服务器添加防回环直连路由
func getDefaultGatewayWindows(excludeGateway string) string {
	psCmd := fmt.Sprintf("Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Where-Object { $_.NextHop -ne '%s' -and $_.NextHop -ne '0.0.0.0' } | Sort-Object RouteMetric | Select-Object -ExpandProperty NextHop -First 1", excludeGateway)
	outPS, errPS := exec.Command("powershell", "-Command", psCmd).Output()
	if errPS == nil {
		gw := strings.TrimSpace(string(outPS))
		ip := net.ParseIP(gw)
		if ip != nil && ip.To4() != nil {
			return gw
		}
	}
	return ""
}

// StartVPN initializes Windows Wintun virtual network adapter and routing table for global proxy using tun2socks engine.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Windows-VPN] Panic recovered: %v", r)
			v.cleanupRouting()
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

	if cmdAdmin := exec.Command("net", "session"); cmdAdmin.Run() != nil {
		log.Println("[Windows-VPN] CRITICAL WARNING: Not running as Administrator! Wintun creation will likely fail or crash.")
	} else {
		log.Println("[Windows-VPN] Running with Administrator privileges confirmed.")
	}

	v.originalGateway = getDefaultGatewayWindows(v.gateway)
	log.Printf("[Windows-VPN] Recorded physical gateway: %s", v.originalGateway)

	// 提取 SOCKS 服务器 host 并添加 /32 直连物理网关路由（防止路由回环）
	if idx := strings.LastIndex(socksAddr, ":"); idx != -1 {
		v.socksHost = socksAddr[:idx]
	} else {
		v.socksHost = socksAddr
	}

	if v.originalGateway != "" && net.ParseIP(v.socksHost) != nil {
		errRoute := exec.Command("route", "add", v.socksHost, "mask", "255.255.255.255", v.originalGateway, "metric", "1").Run()
		if errRoute != nil {
			log.Printf("[Windows-VPN] Failed to add direct /32 route for SOCKS server %s: %v", v.socksHost, errRoute)
		} else {
			log.Printf("[Windows-VPN] Added direct /32 route for SOCKS server %s via physical gateway %s", v.socksHost, v.originalGateway)
		}
	}

	// 1. 启动 tun2socks 引擎（会自动创建 GoOutTUN 网卡）
	if err := jni.StartTun2Socks(-1, v.tunIP, v.gateway, socksAddr); err != nil {
		log.Printf("[Windows-VPN] tun2socks engine failed to start: %v", err)
		v.cleanupRouting()
		return fmt.Errorf("start tun2socks: %w", err)
	}

	// 稍等片刻确保 Wintun 驱动在系统中注册完毕
	time.Sleep(300 * time.Millisecond)

	// 2. 配置 GoOutTUN 的静态 IP 地址
	cmd := exec.Command("netsh", "interface", "ipv4", "set", "address", "name=GoOutTUN", "source=static", "addr="+v.tunIP, "mask=255.255.255.0")
	if err := cmd.Run(); err != nil {
		log.Printf("[Windows-VPN] netsh set address warning: %v", err)
	} else {
		log.Println("[Windows-VPN] netsh IP address set successfully.")
	}

	// 3. 优化网卡 MTU 为 1400，防止大包（如 TLS ClientHello）被静默丢弃
	mtuCmd := exec.Command("netsh", "interface", "ipv4", "set", "subinterface", "GoOutTUN", "mtu=1400", "store=persistent")
	if err := mtuCmd.Run(); err != nil {
		log.Printf("[Windows-VPN] netsh set mtu warning: %v", err)
	} else {
		log.Println("[Windows-VPN] GoOutTUN MTU set to 1400 successfully.")
	}

	// 4. 为 GoOutTUN 配置静态 DNS，确保 DNS 查询走入 TUN 隧道
	dnsServer := v.dnsServer
	if dnsServer == "" {
		dnsServer = "1.1.1.1"
	}
	dnsCmd := exec.Command("netsh", "interface", "ip", "set", "dns", "name=GoOutTUN", "static", dnsServer)
	if err := dnsCmd.Run(); err != nil {
		log.Printf("[Windows-VPN] netsh set dns warning: %v", err)
	} else {
		log.Printf("[Windows-VPN] GoOutTUN static DNS set to %s successfully.", dnsServer)
	}

	// 5. 禁用 GoOutTUN 网卡的 NetBIOS，阻止 137/138 端口局域网广播进入 TUN
	psDisableNetBIOS := fmt.Sprintf("Get-WmiObject Win32_NetworkAdapterConfiguration | Where-Object { $_.IPAddress -contains '%s' } | ForEach-Object { $_.SetTcpIPNetbios(2) }", v.tunIP)
	_ = exec.Command("powershell", "-Command", psDisableNetBIOS).Run()

	// 6. 获取 GoOutTUN 的接口索引 (ifIndex)
	tunIfIndex := getInterfaceIndexByName("GoOutTUN")
	log.Printf("[Windows-VPN] Detected GoOutTUN interface index: %s", tunIfIndex)

	// 7. 清理可能残留的 /1 路由
	v.cleanupRouting()

	// 8. 添加两条 /1 路由（0.0.0.0/1 和 128.0.0.0/1），下一跳设为 0.0.0.0 (on-link) 并绑定到 GoOutTUN 接口
	var err1, err2 error
	if tunIfIndex != "" {
		err1 = exec.Command("route", "add", "0.0.0.0", "mask", "128.0.0.0", "0.0.0.0", "metric", "5", "if", tunIfIndex).Run()
		err2 = exec.Command("route", "add", "128.0.0.0", "mask", "128.0.0.0", "0.0.0.0", "metric", "5", "if", tunIfIndex).Run()
	} else {
		err1 = exec.Command("route", "add", "0.0.0.0", "mask", "128.0.0.0", "0.0.0.0", "metric", "5").Run()
		err2 = exec.Command("route", "add", "128.0.0.0", "mask", "128.0.0.0", "0.0.0.0", "metric", "5").Run()
	}

	if err1 != nil || err2 != nil {
		log.Printf("[Windows-VPN] Failed to add /1 split routes: err1=%v, err2=%v", err1, err2)
	} else {
		log.Println("[Windows-VPN] Global split routes (0.0.0.0/1 & 128.0.0.0/1) bound to GoOutTUN successfully.")
	}

	v.active = true
	log.Printf("[Windows-VPN] VPN started successfully, active=true")
	return nil
}

// cleanupRouting 清理添加的两条 /1 路由及 SOCKS 直连 /32 路由
func (v *VPNServiceManager) cleanupRouting() {
	_ = exec.Command("route", "delete", "0.0.0.0", "mask", "128.0.0.0").Run()
	_ = exec.Command("route", "delete", "128.0.0.0", "mask", "128.0.0.0").Run()
	if v.socksHost != "" {
		_ = exec.Command("route", "delete", v.socksHost).Run()
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

	go func() {
		v.cleanupRouting()
		log.Println("[Windows-VPN] Split routes cleaned up, system restored to physical gateway.")
	}()

	v.active = false
	log.Println("[Windows-VPN] VPN stopped completely in milliseconds.")
	return nil
}
