// core/manager_android.go
//go:build android

package core

import (
	"github.com/gzjjjfree/goout/service"
	"github.com/gzjjjfree/v5-result/transport/internet"
)

// RegisterSocketProtect 将 Android VpnService 的 socket 保护能力注册到 v5-result 拨号器。
func RegisterSocketProtect() {
	internet.SetSocketProtectFunc(service.ProtectSocket)
}
