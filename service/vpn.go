// service/vpn.go
package service

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"syscall"

	"github.com/gzjjjfree/goout/util"
)

// protectSocketFD 默认在非 Android / 未启用 VPN 保护的平台上直接放行（返回 true）。
// Android 平台会在 vpn_android.go 的 init() 中覆盖为真实的 VpnService.protect(fd) 调用。
var protectSocketFD = func(fd int) bool {
	return true
}

// ProtectDialerControl 可直接赋给 net.Dialer.Control。
// 在 Android 开启 VPN 时会将出站 Socket FD 排除出 TUN 路由以走物理网卡；
// 在其他平台（不使用 TUN）则直接返回 nil。
func ProtectDialerControl(network, address string, c syscall.RawConn) error {
	var protectErr error
	if err := c.Control(func(fd uintptr) {
		if !protectSocketFD(int(fd)) {
			protectErr = fmt.Errorf("failed to protect socket fd=%d for %s (%s)", fd, address, network)
		}
	}); err != nil {
		return err
	}
	return protectErr
}

// CheckHttpInbound checks if an HTTP inbound is configured in any of the standard config files,
// returning whether it's HTTP, along with the proxy host and port.
func CheckHttpInbound(socksAddr string) (bool, string, string) {
	host := "127.0.0.1"
	port := "10908"
	if parts := strings.Split(socksAddr, ":"); len(parts) == 2 {
		host = parts[0]
		port = parts[1]
	}

	isHttp := false
	proxyHost := host
	proxyPort := port

	configPaths := []string{"goout.json", "data/goout.json", "config_save.json"}
	for _, path := range configPaths {
		if content, err := os.ReadFile(path); err == nil {
			var raw map[string]interface{}
			if err := json.Unmarshal(content, &raw); err == nil {
				if inbounds, ok := raw["inbounds"].([]interface{}); ok {
					for _, ib := range inbounds {
						if ibMap, ok := ib.(map[string]interface{}); ok {
							if proto, ok := ibMap["protocol"].(string); ok && proto == "http" {
								isHttp = true
								if p := util.ParsePortToInt(ibMap["port"]); p > 0 {
									proxyPort = fmt.Sprintf("%d", p)
								}
								break
							}
						}
					}
				}
			}
		}
	}
	return isHttp, proxyHost, proxyPort
}

var (
	serviceLogHookMu     sync.Mutex
	globalServiceLogHook func(string)
)

// SetGlobalServiceLogHook 设置全局服务日志钩子（例如接入 core.V2rayManager.addLog 统一过滤与缓存）
func SetGlobalServiceLogHook(fn func(string)) {
	serviceLogHookMu.Lock()
	globalServiceLogHook = fn
	serviceLogHookMu.Unlock()
}

// VPNServiceManager handles cross-platform VPN/TUN lifecycle and routing configuration.
type VPNServiceManager struct {
	mu        sync.Mutex
	active    bool
	tunIP     string
	gateway   string
	socksHost string
	onLog     func(string)
}

func NewVPNServiceManager(tunIP, gateway string, onLog func(string)) *VPNServiceManager {
	return &VPNServiceManager{
		tunIP:   tunIP,
		gateway: gateway,
		onLog:   onLog,
	}
}

func (v *VPNServiceManager) SetTunIP(tunIP string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.tunIP = tunIP
}

func (v *VPNServiceManager) IsRunning() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.active
}

// emitLog 通过全局日志钩子或 onLog 回调把消息推入统一日志系统与 UI 日志栏。
// 同时保留 Go 标准 log 输出，确保 goout.log 文件仍记录完整日志。
func (v *VPNServiceManager) emitLog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("%s", msg)

	serviceLogHookMu.Lock()
	hook := globalServiceLogHook
	serviceLogHookMu.Unlock()

	if hook != nil {
		hook(msg)
	} else if v.onLog != nil {
		v.onLog(msg)
	}
}
