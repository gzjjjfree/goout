//go:build windows

package service

import (
	"fmt"
	"log"
	"os/exec"
	"syscall"
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

// StartVPN enables Windows System Proxy pointing to the local SOCKS/proxy address.
func (v *VPNServiceManager) StartVPN(socksAddr string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.active {
		return nil
	}

	log.Printf("[Windows-SystemProxy] Enabling Windows System Proxy: %s", socksAddr)

	psCmd := fmt.Sprintf(
		"Set-ItemProperty -Path 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings' -Name ProxyEnable -Value 1; "+
			"Set-ItemProperty -Path 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings' -Name ProxyServer -Value 'socks=%s'; "+
			"Set-ItemProperty -Path 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings' -Name ProxyOverride -Value 'localhost;127.*;10.*;172.16.*;192.168.*;<local>'",
		socksAddr,
	)

	err := runHiddenCommandRun("powershell", "-Command", psCmd)
	if err != nil {
		log.Printf("[Windows-SystemProxy] Failed to set system proxy: %v", err)
		return fmt.Errorf("failed to set windows system proxy: %w", err)
	}

	v.socksHost = socksAddr
	v.active = true
	log.Println("[Windows-SystemProxy] Windows System Proxy enabled successfully.")
	return nil
}

// StopVPN disables Windows System Proxy and restores direct connection.
func (v *VPNServiceManager) StopVPN() error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.active {
		return nil
	}

	log.Println("[Windows-SystemProxy] Disabling Windows System Proxy...")

	psCmd := "Set-ItemProperty -Path 'HKCU:\\Software\\Microsoft\\Windows\\CurrentVersion\\Internet Settings' -Name ProxyEnable -Value 0"
	err := runHiddenCommandRun("powershell", "-Command", psCmd)
	if err != nil {
		log.Printf("[Windows-SystemProxy] Failed to disable system proxy: %v", err)
	}

	v.active = false
	log.Println("[Windows-SystemProxy] Windows System Proxy disabled successfully.")
	return nil
}
