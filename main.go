// main.go
package main

import (
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/theme"
	"github.com/gzjjjfree/goout/core"
	"github.com/gzjjjfree/goout/core/statusbar"
	"github.com/gzjjjfree/goout/data"
	"github.com/gzjjjfree/goout/ui"
)

func main() {
	// 1. 先重定向 home + chdir（这样下面打开日志时 CWD 已是可写目录）
	data.SetupV2rayHome()

	defer core.CloseLog()

	core.InstallStdioCapture() // ← 加这一行：在启动 V2Ray / tun2socks 之前，把 stdout/stderr 注入日志栏

	a := app.NewWithID("com.gzjjjfree.goout")
	a.Settings().SetTheme(theme.LightTheme())
	w := a.NewWindow("GoOut - 代理与系统工具")

	ui.SetupDashboard(w)
	statusbar.AdaptStatusBar(a)

	w.ShowAndRun()
}
