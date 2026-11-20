//go:build windows

package service

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/gzjjjfree/goout/data"
	"github.com/gzjjjfree/goout/jni"
)

func runHiddenCommand(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
	return cmd.CombinedOutput()
}

func runHiddenCommandRun(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
	return cmd.Run()
}

// getInterfaceIndexByName 获取指定网卡名称的 Windows 接口索引 (ifIndex)
func getInterfaceIndexByName(alias string) string {
	psCmd := fmt.Sprintf("(Get-NetAdapter -Name '%s' -ErrorAction SilentlyContinue).ifIndex", alias)
	out, err := runHiddenCommand("powershell", "-Command", psCmd)
	if err == nil {
		idx := strings.TrimSpace(string(out))
		if idx != "" {
			return idx
		}
	}
	return ""
}

// getLoopbackMetric 动态获取系统中 127.0.0.0/8 路由的跃点数，与系统保持一致，获取失败时回退默认 331
func getLoopbackMetric() string {
	psCmd := "(Get-NetRoute -DestinationPrefix '127.0.0.0/8' -ErrorAction SilentlyContinue | Select-Object -ExpandProperty RouteMetric -First 1)"
	out, err := runHiddenCommand("powershell", "-Command", psCmd)
	if err == nil {
		metric := strings.TrimSpace(string(out))
		if metric != "" {
			return metric
		}
	}
	return "331"
}

// getDefaultGatewayWindows 获取物理网卡的默认网关，用于为代理服务器添加防回环直连路由
func getDefaultGatewayWindows(excludeGateway string) string {
	psCmd := fmt.Sprintf("Get-NetRoute -DestinationPrefix '0.0.0.0/0' | Where-Object { $_.NextHop -ne '%s' -and $_.NextHop -ne '0.0.0.0' } | Sort-Object RouteMetric | Select-Object -ExpandProperty NextHop -First 1", excludeGateway)
	outPS, errPS := runHiddenCommand("powershell", "-Command", psCmd)
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

	if errAdmin := runHiddenCommandRun("net", "session"); errAdmin != nil {
		log.Println("[Windows-VPN] CRITICAL WARNING: Not running as Administrator! Wintun creation will likely fail or crash.")
	} else {
		log.Println("[Windows-VPN] Running with Administrator privileges confirmed.")
	}

	v.originalGateway = getDefaultGatewayWindows(v.gateway)
	log.Printf("[Windows-VPN] Recorded physical gateway: %s", v.originalGateway)

	// 0. 清理可能残留的 /1 路由，然后再添加 /32 直连路由
	cleanupSplitRoutes()

	// 提取 SOCKS 服务器 host 及加载 data/result.json 与配置文件中的所有代理 IP，并添加 /32 直连物理网关路由（防止路由回环）
	if idx := strings.LastIndex(socksAddr, ":"); idx != -1 {
		v.socksHost = socksAddr[:idx]
	} else {
		v.socksHost = socksAddr
	}

	v.proxyHosts = v.loadProxyHosts()

	if v.originalGateway != "" {
		for _, host := range v.proxyHosts {
			errRoute := runHiddenCommandRun("route", "add", host, "mask", "255.255.255.255", v.originalGateway, "metric", "1")
			if errRoute != nil {
				log.Printf("[Windows-VPN] Failed to add direct /32 route for proxy host %s: %v", host, errRoute)
			} else {
				log.Printf("[Windows-VPN] Added direct /32 route for proxy host %s via physical gateway %s", host, v.originalGateway)
			}
		}
	}

	// 添加 /32 直连物理网关路由，使 DNS 请求直接发往公网 dnsServer 而不走 TUN 隧道
	if v.originalGateway != "" && net.ParseIP(v.dnsServer) != nil {
		v.logPrintf("[Windows-VPN-DNS] Configuring DNS bypass routing for DNS server: %s via physical gateway: %s", v.dnsServer, v.originalGateway)
		errRoute := runHiddenCommandRun("route", "add", v.dnsServer, "mask", "255.255.255.255", v.originalGateway, "metric", "1")
		if errRoute != nil {
			v.logPrintf("[Windows-VPN-DNS] Failed to add direct /32 route for DNS server %s: %v", v.dnsServer, errRoute)
		} else {
			v.logPrintf("[Windows-VPN-DNS] Added direct /32 route for DNS server %s via physical gateway %s successfully", v.dnsServer, v.originalGateway)
		}
	} else {
		v.logPrintf("[Windows-VPN-DNS] Skipping direct /32 route for DNS server: originalGateway=%s, dnsServer=%s", v.originalGateway, v.dnsServer)
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
	errNetsh := runHiddenCommandRun("netsh", "interface", "ipv4", "set", "address", "name=GoOutTUN", "source=static", "addr="+v.tunIP, "mask=255.255.255.0")
	if errNetsh != nil {
		log.Printf("[Windows-VPN] netsh set address warning: %v", errNetsh)
	} else {
		log.Println("[Windows-VPN] netsh IP address set successfully.")
	}

	// 3. 优化网卡 MTU 为 1400，防止大包（如 TLS ClientHello）被静默丢弃
	errMtu := runHiddenCommandRun("netsh", "interface", "ipv4", "set", "subinterface", "GoOutTUN", "mtu=1400", "store=persistent")
	if errMtu != nil {
		log.Printf("[Windows-VPN] netsh set mtu warning: %v", errMtu)
	} else {
		log.Println("[Windows-VPN] GoOutTUN MTU set to 1400 successfully.")
	}

	// 4. 为 GoOutTUN 配置静态 DNS，确保 DNS 查询走入 TUN 隧道
	dnsServer := v.dnsServer
	if dnsServer == "" {
		dnsServer = "8.8.8.8"
	}
	v.logPrintf("[Windows-VPN-DNS] Setting GoOutTUN interface static DNS server to: %s (DNS request flow entering TUN tunnel)", dnsServer)
	errDns := runHiddenCommandRun("netsh", "interface", "ip", "set", "dns", "name=GoOutTUN", "static", dnsServer)
	if errDns != nil {
		v.logPrintf("[Windows-VPN-DNS] netsh set dns warning: %v", errDns)
	} else {
		v.logPrintf("[Windows-VPN-DNS] GoOutTUN static DNS set to %s successfully. DNS requests will now be intercepted and resolved through tunnel/proxy.", dnsServer)
	}

	// 5. 禁用 GoOutTUN 网卡的 NetBIOS，阻止 137/138 端口局域网广播进入 TUN
	psDisableNetBIOS := fmt.Sprintf("Get-WmiObject Win32_NetworkAdapterConfiguration | Where-Object { $_.IPAddress -contains '%s' } | ForEach-Object { $_.SetTcpIPNetbios(2) }", v.tunIP)
	_ = runHiddenCommandRun("powershell", "-Command", psDisableNetBIOS)

	// 6. 获取 GoOutTUN 的接口索引 (ifIndex)
	tunIfIndex := getInterfaceIndexByName("GoOutTUN")
	log.Printf("[Windows-VPN] Detected GoOutTUN interface index: %s", tunIfIndex)

	// 7. 添加两条 /1 路由（0.0.0.0/1 和 128.0.0.0/1），下一跳设为 0.0.0.0 (on-link) 并绑定到 GoOutTUN 接口
	var err1, err2 error
	if tunIfIndex != "" {
		err1 = runHiddenCommandRun("route", "add", "0.0.0.0", "mask", "128.0.0.0", "0.0.0.0", "metric", "5", "if", tunIfIndex)
		err2 = runHiddenCommandRun("route", "add", "128.0.0.0", "mask", "128.0.0.0", "0.0.0.0", "metric", "5", "if", tunIfIndex)
	} else {
		err1 = runHiddenCommandRun("route", "add", "0.0.0.0", "mask", "128.0.0.0", "0.0.0.0", "metric", "5")
		err2 = runHiddenCommandRun("route", "add", "128.0.0.0", "mask", "128.0.0.0", "0.0.0.0", "metric", "5")
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

// cleanupSplitRoutes 仅清理可能残留的两条 /1 路由，避免清理刚加好的 /32 直连路由
func cleanupSplitRoutes() {
	_ = runHiddenCommandRun("route", "delete", "0.0.0.0", "mask", "128.0.0.0")
	_ = runHiddenCommandRun("route", "delete", "128.0.0.0", "mask", "128.0.0.0")
}

// cleanupRouting 清理添加的两条 /1 路由及所有代理 IP / DNS 直连 /32 路由
func (v *VPNServiceManager) cleanupRouting() {
	cleanupSplitRoutes()
	for _, host := range v.proxyHosts {
		_ = runHiddenCommandRun("route", "delete", host, "mask", "255.255.255.255")
		_ = runHiddenCommandRun("route", "delete", host)
	}
	if v.socksHost != "" {
		if ip := net.ParseIP(v.socksHost); ip == nil || !ip.IsLoopback() {
			_ = runHiddenCommandRun("route", "delete", v.socksHost, "mask", "255.255.255.255")
			_ = runHiddenCommandRun("route", "delete", v.socksHost)
		} else {
			log.Printf("[Windows-VPN] Preserving loopback SOCKS host route: %s", v.socksHost)
		}
	}
	if v.dnsServer != "" {
		_ = runHiddenCommandRun("route", "delete", v.dnsServer, "mask", "255.255.255.255")
		_ = runHiddenCommandRun("route", "delete", v.dnsServer)
	}
	// 确保 127.0.0.1 回环路由存在，动态获取系统中 127.0.0.0/8 的跃点数与接口 1
	metric := getLoopbackMetric()
	if errAdd := runHiddenCommandRun("route", "add", "127.0.0.1", "mask", "255.255.255.255", "0.0.0.0", "metric", metric, "if", "1"); errAdd != nil {
		log.Printf("[Windows-VPN] Failed to restore 127.0.0.1 loopback route: %v", errAdd)
	} else {
		log.Printf("[Windows-VPN] Restored 127.0.0.1 loopback route successfully (metric %s, if 1).", metric)
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

	v.cleanupRouting()
	log.Println("[Windows-VPN] Split routes cleaned up, system restored to physical gateway.")

	v.active = false
	log.Println("[Windows-VPN] VPN stopped completely in milliseconds.")
	return nil
}
