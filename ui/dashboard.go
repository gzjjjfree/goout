package ui

import (
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/gzjjjfree/goout/core"
	"github.com/gzjjjfree/goout/data"
	"github.com/gzjjjfree/goout/service"
)

func SetupDashboard(w fyne.Window) {
	cfg, err := data.LoadConfig("config.json")
	if err != nil {
		cfg = data.DefaultConfig()
	}

	logArea := widget.NewMultiLineEntry()
	logArea.Wrapping = fyne.TextWrapWord
	logArea.SetText("GoOut initialized successfully.\nReady to start proxy...")

	// Callback for real-time log updates with fyne.Do (UI Thread Safety)
	onLog := func(msg string) {
		fyne.Do(func() {
			current := logArea.Text
			logArea.SetText(current + "\n" + msg)
		})
	}

	pm := core.NewProcessManager(onLog)
	vpnMgr := service.NewVPNServiceManager(cfg.TunIP, "10.0.0.1")

	statusLabel := widget.NewLabel("Status: Stopped")
	var toggleBtn *widget.Button

	toggleBtn = widget.NewButton("Start Proxy & VPN", func() {
		if !pm.IsRunning() {
			// Start process & VPN
			socksAddr := fmt.Sprintf("127.0.0.1:%d", cfg.SocksPort)
			err := pm.Start(cfg.V2RayConfigPath)
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			err = vpnMgr.StartVPN(socksAddr)
			if err != nil {
				_ = pm.Stop()
				dialog.ShowError(err, w)
				return
			}
			statusLabel.SetText("Status: Running")
			toggleBtn.SetText("Stop Proxy & VPN")
		} else {
			// Stop process & VPN
			_ = vpnMgr.StopVPN()
			_ = pm.Stop()
			statusLabel.SetText("Status: Stopped")
			toggleBtn.SetText("Start Proxy & VPN")
		}
	})

	configBtn := widget.NewButton("Settings", func() {
		showSettingsDialog(w, cfg)
	})

	appVersion := "1.0.0"
	if app := fyne.CurrentApp(); app != nil {
		if ver := app.Metadata().Version; ver != "" {
			appVersion = ver
		}
	}

	topContent := container.NewVBox(
		widget.NewLabelWithStyle(fmt.Sprintf("GoOut Control Panel v%s", appVersion), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		statusLabel,
		container.NewHBox(toggleBtn, configBtn),
		widget.NewSeparator(),
		widget.NewLabel("Logs:"),
	)

	content := container.NewBorder(topContent, nil, nil, nil, container.NewVScroll(logArea))

	w.SetContent(content)
	w.Resize(fyne.NewSize(450, 650))
}

func showSettingsDialog(w fyne.Window, cfg *data.Config) {
	addrEntry := widget.NewEntry()
	addrEntry.SetText(cfg.ServerAddress)

	portEntry := widget.NewEntry()
	portEntry.SetText(strconv.Itoa(cfg.ServerPort))

	socksPortEntry := widget.NewEntry()
	socksPortEntry.SetText(strconv.Itoa(cfg.SocksPort))

	form := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "Server Address", Widget: addrEntry},
			{Text: "Server Port", Widget: portEntry},
			{Text: "SOCKS Port", Widget: socksPortEntry},
		},
		OnSubmit: func() {
			cfg.ServerAddress = addrEntry.Text
			if p, err := strconv.Atoi(portEntry.Text); err == nil {
				cfg.ServerPort = p
			}
			if sp, err := strconv.Atoi(socksPortEntry.Text); err == nil {
				cfg.SocksPort = sp
			}
			_ = data.SaveConfig("config.json", cfg)
			dialog.ShowInformation("Success", "Settings saved successfully", w)
		},
	}

	d := dialog.NewCustom("Configuration Settings", "Close", form, w)
	d.Resize(fyne.NewSize(400, 300))
	d.Show()
}
