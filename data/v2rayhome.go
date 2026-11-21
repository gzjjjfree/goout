// data/v2rayhome.go
package data

import (
	"log"
	"os"
)

// SetupV2rayHome 必须在启动 v2ray 核心、打开日志之前调用。
// 它会把 v2ray 的 home 目录 + 进程工作目录重定向到可写目录，
// 避免 v2ray-core 通过 os.Executable()（Android 上返回 /system/bin）
// 去只读的系统目录里找/写资源。
func SetupV2rayHome() {
	// 1. 把内嵌资产释放到可写目录（geosite.dat、result/result.json）
	EnsureGeositeDat()
	EnsureResultJSON()

	// 2. 拿到可写 home 目录
	dir := getTargetDir()
	if dir == "" {
		dir = "."
	}

	// 3. 让 v2ray 的资产目录(geosite.dat / geoip.dat) 落在可写目录
	_ = os.Setenv("v2ray.location.asset", dir)
	_ = os.Setenv("V2RAY_LOCATION_ASSET", dir)
	// 4. 部分 v2ray 分支用 HOME 定位配置/资源
	_ = os.Setenv("HOME", dir)

	// 5. 把工作目录切到可写目录 —— 此后所有相对路径操作都落到这里
	if err := os.Chdir(dir); err != nil {
		log.Printf("[GoOut] failed to chdir to %s: %v", dir, err)
		return
	}

	log.Printf("[GoOut] v2ray home dir set to: %s", dir)
}
