package main

import (
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/theme"
	"github.com/gzjjjfree/goout/ui"
)

func main() {
	a := app.NewWithID("com.gzjjjfree.goout")
	a.Settings().SetTheme(theme.LightTheme())
	w := a.NewWindow("GoOut - Proxy & System Utility")

	ui.SetupDashboard(w)

	w.ShowAndRun()
}
