//go:build windows

package service

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"

	"github.com/gzjjjfree/goout/data"
	"github.com/gzjjjfree/goout/jni"
)

func getDefaultGatewayWindows(excludeGateway string) string {
	out, err := exec.Command("route", "print", "0.0.0.0").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 3 && fields[0] == "0.0.0.0" && fields[1] == "0.0.0.0" {
			gateway := fields[2]
			if net.ParseIP(gateway) != nil && gateway != "0.0.0.0" && gateway != excludeGateway {
				return gateway
			}
		}
	}
	return ""
}

// StartVPN initializes Windows Wintun virtual network adapter and routing table for global proxy using tun2socks engine.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	jni.InitProxyContext()

	log.Printf("[Windows-VPN] Starting Wintun TUN interface & routing table: IP=%s, Gateway=%s, SOCKS=%s", v.tunIP, v.gateway, socksAddr)

	data.EnsureWintunDLL()

	v.originalGateway = getDefaultGatewayWindows(v.gateway)
	log.Printf("[Windows-VPN] Recorded original default gateway: %s", v.originalGateway)

	// Start tun2socks engine with wintun://GoOutTUN
	jni.StartTun2Socks(-1, v.tunIP, v.gateway, socksAddr)

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
