package main

import (
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/theme"
	"github.com/gzjjjfree/goout/ui"
)

func main() {
	a := app.NewWithID("com.gzjjjfree.goout")
	a.Settings().SetTheme(theme.LightTheme())
	w := a.NewWindow("GoOut - 代理与系统工具")

	ui.SetupDashboard(w)

	w.ShowAndRun()
}
