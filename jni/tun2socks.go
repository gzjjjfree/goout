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
func StartTun2Socks(fd int, tunAddr, gateway, socksServer string) {
	log.Printf("[Tun2Socks] StartTun2Socks called: fd=%d, tun=%s, gateway=%s, socks=%s", fd, tunAddr, gateway, socksServer)

	var deviceStr string
	if fd >= 0 {
		deviceStr = fmt.Sprintf("fd://%d", fd)
	} else if runtime.GOOS == "windows" {
		deviceStr = "wintun://GoOutTUN"
	} else {
		deviceStr = "tun://tun0"
	}

	proxyURL := socksServer
	if !strings.Contains(proxyURL, "://") {
		proxyURL = "socks5://" + proxyURL
	}

	log.Printf("[Tun2Socks] Inserting engine config: Device=%s, Proxy=%s", deviceStr, proxyURL)
	engine.Insert(&engine.Key{
		Device: deviceStr,
		Proxy:  proxyURL,
	})

	engine.Start()
	log.Println("[Tun2Socks] tun2socks engine started successfully.")
}

// StopTun2Socks stops the tun2socks engine.
func StopTun2Socks() {
	log.Println("[Tun2Socks] Stopping tun2socks engine...")
	engine.Stop()
	log.Println("[Tun2Socks] tun2socks engine stopped.")
}

// RunTunEngine kept for backward compatibility if any legacy code calls it directly
func RunTunEngine(file interface{}, tunAddr, gateway, socksServer string) {
	log.Println("[Tun2Socks] RunTunEngine invoked (legacy wrapper)")
}
