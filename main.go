package main

import (
	"log"
	"os"

	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/theme"
	"github.com/gzjjjfree/goout/core/statusbar"
	"github.com/gzjjjfree/goout/ui"
)

func main() {
	logFile, err := os.OpenFile("goout.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(logFile)
	}

	a := app.NewWithID("com.gzjjjfree.goout")
	a.Settings().SetTheme(theme.LightTheme())
	w := a.NewWindow("GoOut - 代理与系统工具")

	ui.SetupDashboard(w)
	statusbar.AdaptStatusBar(a)

	w.ShowAndRun()
}
