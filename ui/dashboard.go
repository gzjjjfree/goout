// ui/dashboard.go
package ui

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/gzjjjfree/goout/core"
	"github.com/gzjjjfree/goout/data"
	"github.com/gzjjjfree/goout/service"
	"github.com/gzjjjfree/goout/util"
)

var Version = "1.0.0"

const (
	dashboardMaxLogLines   = 200
	dashboardMaxLineChars  = 4096
	dashboardLogChanCap    = 1024
	dashboardFlushInterval = 250 * time.Millisecond
)

func sanitizeDashboardLine(line string) string {
	cleaned := strings.TrimRight(line, "\r\n")
	if len(cleaned) > dashboardMaxLineChars {
		return cleaned[:dashboardMaxLineChars] + "...(截断)"
	}
	return cleaned
}

func appendBoundedLogLines(existing []string, rawMsgs []string) []string {
	for _, msg := range rawMsgs {
		if !core.ShouldAllowLog(msg) {
			continue
		}
		for _, part := range strings.Split(msg, "\n") {
			line := sanitizeDashboardLine(part)
			if line == "" || !core.ShouldAllowLog(line) {
				continue
			}
			existing = append(existing, line)
		}
	}
	if len(existing) > dashboardMaxLogLines {
		existing = existing[len(existing)-dashboardMaxLogLines:]
	}
	return existing
}

// resolveSharedV2RayConfig 统一解析 V2Ray 配置文件与入站端口，供 V2Ray 与 VPN 共同使用。
func resolveSharedV2RayConfig(cfg *data.Config) (string, []byte, int, error) {
	configPath := strings.TrimSpace(cfg.V2RayConfigPath)
	if configPath == "" {
		configPath = "goout.json"
		cfg.V2RayConfigPath = configPath
	}

	configBytes, err := os.ReadFile(configPath)
	if err != nil {
		dir := filepath.Dir(configPath)
		if dir != "" && dir != "." {
			_ = os.MkdirAll(dir, 0755)
		}
		defaultBytes := data.GetDefaultConfigBytes()
		if writeErr := os.WriteFile(configPath, defaultBytes, 0644); writeErr != nil {
			return configPath, nil, cfg.SocksPort, err
		}
		configBytes, err = os.ReadFile(configPath)
		if err != nil {
			return configPath, nil, cfg.SocksPort, err
		}
	}

	socksPort := cfg.SocksPort
	var rawCfg map[string]interface{}
	if jsonErr := json.Unmarshal(configBytes, &rawCfg); jsonErr == nil {
		if inbounds, ok := rawCfg["inbounds"].([]interface{}); ok {
			for _, ib := range inbounds {
				if ibMap, ok := ib.(map[string]interface{}); ok {
					if p := util.ParsePortToInt(ibMap["port"]); p > 0 {
						socksPort = p
						break
					}
				}
			}
		}
	}
	if socksPort <= 0 {
		socksPort = 10909
	}
	cfg.SocksPort = socksPort

	return configPath, configBytes, socksPort, nil
}

func SetupDashboard(w fyne.Window) {
	cfg, err := data.LoadConfig("config_save.json")
	if err != nil {
		cfg, err = data.LoadConfig("config.json")
		if err != nil {
			cfg, err = data.DefaultConfig()
			if err != nil {
				cfg = &data.Config{
					V2RayConfigPath: "goout.json",
					SocksPort:       10808,
					TunIP:           "10.0.0.2",
					ServerAddress:   "127.0.0.1",
					ServerPort:      10809,
					Subscriptions:   []string{},
					RoutingBypass:   []string{"10.0.0.0/8", "192.168.0.0/16"},
				}
			}
		}
	}
	_, _, _, _ = resolveSharedV2RayConfig(cfg)

	var logMu sync.Mutex
	logLines := appendBoundedLogLines(nil, []string{
		"GoOut 初始化成功 (内存 V2Ray 核心)。",
		"准备启动代理...",
	})
	logLabel := widget.NewLabel(strings.Join(logLines, "\n"))
	logLabel.Wrapping = fyne.TextWrapWord

	scroll := container.NewVScroll(logLabel)

	logChan := make(chan string, dashboardLogChanCap)
	var flushScheduled atomicBool
	var lastRenderedText string

	flushLogsToUI := func(force bool) {
		var batch []string
		for {
			select {
			case msg := <-logChan:
				batch = append(batch, msg)
				if len(batch) >= dashboardMaxLogLines*2 {
					batch = batch[len(batch)-dashboardMaxLogLines:]
				}
			default:
				goto DRAINED
			}
		}
	DRAINED:
		logMu.Lock()
		if len(batch) > 0 {
			logLines = appendBoundedLogLines(logLines, batch)
		}
		nextText := strings.Join(logLines, "\n")
		if !force && nextText == lastRenderedText {
			logMu.Unlock()
			return
		}
		lastRenderedText = nextText
		logMu.Unlock()

		fyne.Do(func() {
			logLabel.SetText(nextText)
			scroll.ScrollToBottom()
		})
	}

	scheduleFlush := func() {
		if !flushScheduled.CompareAndSwap(false, true) {
			return
		}
		time.AfterFunc(dashboardFlushInterval, func() {
			flushScheduled.Store(false)
			flushLogsToUI(false)
		})
	}

	clearLogQueue := func() {
		for {
			select {
			case <-logChan:
			default:
				goto DRAINED
			}
		}
	DRAINED:
		logMu.Lock()
		logLines = nil
		lastRenderedText = ""
		logMu.Unlock()

		fyne.Do(func() {
			logLabel.SetText("")
		})
	}

	var v2rayMgr *core.V2rayManager
	onLog := func(msg string) {
		if !core.ShouldAllowLog(msg) {
			return
		}
		// lowerMsg := strings.ToLower(msg)
		// if strings.Contains(lowerMsg, "stopping v2ray") ||
		// 	strings.Contains(lowerMsg, "instance stopped") ||
		// 	strings.Contains(lowerMsg, "core closing") ||
		// 	strings.Contains(lowerMsg, "instance close") ||
		// 	strings.Contains(lowerMsg, "closed network connection") ||
		// 	strings.Contains(lowerMsg, "failed to read udp msg") {
		// 	return
		// }
		select {
		case logChan <- msg:
			scheduleFlush()
		default:
			// 高并发下队列满时丢弃最旧一条再写入最新日志，防止阻塞和卡死
			select {
			case <-logChan:
			default:
			}
			select {
			case logChan <- msg:
			default:
			}
			scheduleFlush()
		}
	}

	v2rayMgr = core.GetManager(onLog)
	vpnMgr := service.NewVPNServiceManager(cfg.TunIP, "10.0.0.1", onLog)

	statusLabel := widget.NewLabel("状态: 已停止")
	var v2rayBtn *widget.Button
	var vpnBtn *widget.Button
	var configBtn *widget.Button

	updateStatus := func() {
		v2Run := v2rayMgr.IsRunning()
		vpnRun := vpnMgr.IsRunning()
		if v2Run && vpnRun {
			statusLabel.SetText("状态: V2Ray 运行中 | VPN 运行中")
			v2rayBtn.SetText("停止 V2Ray 核心")
			vpnBtn.SetText("停止 VPN")
			if runtime.GOOS == "android" {
				vpnBtn.Disable()
			}
			if configBtn != nil {
				configBtn.Disable()
			}
		} else if v2Run {
			statusLabel.SetText("状态: V2Ray 运行中 | VPN 已停止")
			v2rayBtn.SetText("停止 V2Ray 核心")
			vpnBtn.SetText("启动 VPN")
			if runtime.GOOS == "android" {
				vpnBtn.Disable()
			}
			if configBtn != nil {
				configBtn.Disable()
			}
		} else if vpnRun {
			statusLabel.SetText("状态: V2Ray 已停止 | VPN 运行中")
			v2rayBtn.SetText("启动 V2Ray 核心")
			vpnBtn.SetText("停止 VPN")
			if runtime.GOOS == "android" {
				vpnBtn.Disable()
			}
			if configBtn != nil {
				configBtn.Enable()
			}
		} else {
			statusLabel.SetText("状态: 已停止")
			v2rayBtn.SetText("启动 V2Ray 核心")
			vpnBtn.SetText("启动 VPN")
			if runtime.GOOS == "android" {
				vpnBtn.Disable()
			}
			if configBtn != nil {
				configBtn.Enable()
			}
		}
	}

	v2rayBtn = widget.NewButton("启动 V2Ray 核心", func() {
		v2rayBtn.Disable()
		go func() {
			defer fyne.Do(func() {
				v2rayBtn.Enable()
				updateStatus()
			})

			if !v2rayMgr.IsRunning() {
				fyne.Do(func() {
					statusLabel.SetText("状态: V2Ray 启动中...")
				})

				configPath, configBytes, socksPort, err := resolveSharedV2RayConfig(cfg)
				if err != nil {
					fyne.Do(func() {
						dialog.ShowError(err, w)
					})
					return
				}
				_ = data.SaveConfig("config_save.json", cfg)

				log.Printf("[Dashboard] Starting V2Ray with shared config=%s (socksPort=%d)", configPath, socksPort)
				err = v2rayMgr.Start(configBytes)
				if err != nil {
					log.Printf("[Dashboard] V2Ray start failed: %v", err)
					fyne.Do(func() {
						dialog.ShowError(err, w)
					})
					return
				}
				flushLogsToUI(true)
				log.Printf("[Dashboard] V2Ray core started successfully.")
			} else {
				fyne.Do(func() {
					statusLabel.SetText("状态: V2Ray 停止中...")
				})
				_ = v2rayMgr.Stop()
				log.Printf("[Dashboard] V2Ray core stopped.")
			}
		}()
	})

	vpnBtn = widget.NewButton("启动 VPN", func() {
		vpnBtn.Disable()
		go func() {
			defer fyne.Do(func() {
				vpnBtn.Enable()
				updateStatus()
			})

			defer func() {
				if r := recover(); r != nil {
					log.Printf("[Dashboard] VPN 启动 panic 已恢复: %v\n%s", r, debug.Stack())
					fyne.Do(func() {
						dialog.ShowError(fmt.Errorf("VPN 启动异常: %v", r), w)
					})
				}
			}()

			if !vpnMgr.IsRunning() {
				fyne.Do(func() {
					statusLabel.SetText("状态: VPN 启动中...")
				})

				configPath, _, socksPort, err := resolveSharedV2RayConfig(cfg)
				if err != nil {
					log.Printf("[Dashboard] Resolve config for VPN warning: %v", err)
				}
				_ = data.SaveConfig("config_save.json", cfg)

				socksAddr := fmt.Sprintf("127.0.0.1:%d", socksPort)
				log.Printf("[Dashboard] Starting VPN manager with shared config=%s, SOCKS addr=%s", configPath, socksAddr)
				err = vpnMgr.StartVPN(socksAddr)
				if err != nil {
					log.Printf("[Dashboard] VPN start failed: %v", err)
					fyne.Do(func() {
						dialog.ShowError(err, w)
					})
					return
				}
				flushLogsToUI(true)
				log.Printf("[Dashboard] VPN started successfully.")
			} else {
				fyne.Do(func() {
					statusLabel.SetText("状态: VPN 停止中...")
				})
				_ = vpnMgr.StopVPN()
				log.Printf("[Dashboard] VPN stopped.")
			}
		}()
	})

	configBtn = widget.NewButton("设置", func() {
		if v2rayMgr.IsRunning() {
			dialog.ShowInformation("提示", "V2Ray 运行中，无法打开设置", w)
			return
		}
		showSettingsDialog(w, cfg, vpnMgr)
	})

	updateStatus()

	clearBtn := widget.NewButton("清除日志", func() {
		clearLogQueue()
		v2rayMgr.ClearLogs()
	})

	appVersion := Version
	if app := fyne.CurrentApp(); app != nil {
		if ver := app.Metadata().Version; ver != "" && ver != "0.0.0" {
			appVersion = ver
		}
	}
	if appVersion == "" || appVersion == "0.0.0" {
		appVersion = Version
	}

	topContent := container.NewVBox(
		widget.NewLabelWithStyle(fmt.Sprintf("GoOut 控制面板 v%s (内存版)", appVersion), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		statusLabel,
		container.NewHBox(v2rayBtn, vpnBtn, configBtn),
		widget.NewSeparator(),
		container.NewBorder(nil, nil, widget.NewLabel("日志:"), clearBtn, nil),
	)

	content := container.NewBorder(topContent, nil, nil, nil, scroll)

	w.SetContent(content)
	w.Resize(fyne.NewSize(450, 650))
}

type atomicBool struct {
	mu  sync.Mutex
	val bool
}

func (b *atomicBool) Store(v bool) {
	b.mu.Lock()
	b.val = v
	b.mu.Unlock()
}

func (b *atomicBool) CompareAndSwap(oldVal, newVal bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.val == oldVal {
		b.val = newVal
		return true
	}
	return false
}
