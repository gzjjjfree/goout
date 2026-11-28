package jni

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"strings"
	"sync"

	"github.com/xjasonlyu/tun2socks/v2/engine"
)

// 全局 Context 管理（用于点击停止时秒停）
var (
	proxyCtx    context.Context
	proxyCancel context.CancelFunc
	proxyMutex  sync.Mutex
)

func InitProxyContext() {
	proxyMutex.Lock()
	defer proxyMutex.Unlock()
	if proxyCancel != nil {
		proxyCancel()
	}
	proxyCtx, proxyCancel = context.WithCancel(context.Background())
}

func CancelProxyContext() {
	proxyMutex.Lock()
	defer proxyMutex.Unlock()
	if proxyCancel != nil {
		proxyCancel()
		proxyCancel = nil
	}
}

// StartTun2Socks starts the embedded tun2socks engine (gVisor netstack) with xjasonlyu/tun2socks v2.
func StartTun2Socks(fd int, tunAddr, gateway, socksServer string) error {
	log.Printf("[Tun2Socks] StartTun2Socks called: fd=%d, tun=%s, gateway=%s, socks=%s", fd, tunAddr, gateway, socksServer)

	var deviceStr string
	if fd >= 0 {
		// 真实 fd（Android VpnService 提供）：直接用 fd:// 启动引擎。
		deviceStr = fmt.Sprintf("fd://%d", fd)
	} else if runtime.GOOS == "windows" {
		deviceStr = "tun://GoOutTUN"
	} else if runtime.GOOS == "linux" {
		// Linux 分支：当前没有真实 TUN fd 来源（无 VpnService/TUN 接口），
		// 明确 stub 语义——不伪装成真实 TUN，直接返回错误，避免引擎在错误设备上 panic。
		return fmt.Errorf("linux tun2socks stub: no real TUN fd available (fd=%d); requires a real TUN fd source (e.g. VpnService/TUN interface)", fd)
	} else {
		deviceStr = "tun://tun0"
	}

	proxyURL := socksServer
	if !strings.Contains(proxyURL, "://") {
		proxyURL = "socks5://" + proxyURL
	}

	log.Printf("[Tun2Socks] Inserting engine config: Device=%s, Proxy=%s", deviceStr, proxyURL)

	// engine.Insert / engine.Start 在错误设备（如 Android 上 fd=-1 时构造出的
	// tun://tun0）上可能 panic。用 recover 将 panic 转为 error 返回，避免冒泡到
	// 上层 goroutine 导致整个进程闪退。
	var insertErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				insertErr = fmt.Errorf("tun2socks engine.Insert panic: %v", r)
			}
		}()
		engine.Insert(&engine.Key{
			Device:   deviceStr,
			Proxy:    proxyURL,
			LogLevel: "info",
		})
	}()
	if insertErr != nil {
		return insertErr
	}

	var startErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				startErr = fmt.Errorf("tun2socks engine.Start panic: %v", r)
			}
		}()
		engine.Start()
	}()
	if startErr != nil {
		return startErr
	}

	log.Println("[Tun2Socks] tun2socks engine started successfully.")
	return nil
}

// StopTun2Socks stops the tun2socks engine.
func StopTun2Socks() {
	log.Println("[Tun2Socks] Stopping tun2socks engine...")

	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Tun2Socks] Stop panic recovered: %v", r)
			}
		}()
		engine.Stop()
	}()

	log.Println("[Tun2Socks] tun2socks engine stopped.")
}
