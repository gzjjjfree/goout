package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/gzjjjfree/goout/core"
	"github.com/gzjjjfree/goout/data"
	"github.com/gzjjjfree/goout/service"
)

func InspectNativeLibs() {

}

func SetupDashboard(w fyne.Window) {
	cfg, err := data.LoadConfig("config_save.json")
	if err != nil {
		cfg, err = data.LoadConfig("config.json")
		if err != nil {
			cfg = data.DefaultConfig()
		}
	}

	logArea := widget.NewMultiLineEntry()
	logArea.Wrapping = fyne.TextWrapWord
	logArea.SetText("GoOut 初始化成功 (内存 V2Ray 核心)。\n准备启动代理...")

	// Callback for real-time log updates with fyne.Do (UI Thread Safety)
	onLog := func(msg string) {
		fyne.Do(func() {
			current := logArea.Text
			logArea.SetText(current + "\n" + msg)
		})
	}

	v2rayMgr := core.GetManager(onLog)
	vpnMgr := service.NewVPNServiceManager(cfg.TunIP, "10.0.0.1")

	statusLabel := widget.NewLabel("状态: 已停止")
	var toggleBtn *widget.Button

	toggleBtn = widget.NewButton("启动代理与 VPN", func() {
		if !v2rayMgr.IsRunning() {
			// 读取 V2Ray 配置文件字节流并在内存中启动
			configBytes, err := os.ReadFile(cfg.V2RayConfigPath)
			if err != nil {
				// 如果配置文件不存在，自动保存默认配置并重新读取
				_ = data.SaveConfig(cfg.V2RayConfigPath, cfg)
				configBytes, err = os.ReadFile(cfg.V2RayConfigPath)
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
			}

			socksAddr := fmt.Sprintf("127.0.0.1:%d", cfg.SocksPort)
			err = v2rayMgr.Start(configBytes)
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			err = vpnMgr.StartVPN(socksAddr)
			if err != nil {
				_ = v2rayMgr.Stop()
				dialog.ShowError(err, w)
				return
			}
			statusLabel.SetText("状态: 运行中")
			toggleBtn.SetText("停止代理与 VPN")
		} else {
			// 停止 VPN 与内存中的 V2Ray 核心
			_ = vpnMgr.StopVPN()
			_ = v2rayMgr.Stop()
			statusLabel.SetText("状态: 已停止")
			toggleBtn.SetText("启动代理与 VPN")
		}
	})

	configBtn := widget.NewButton("设置", func() {
		showSettingsDialog(w, cfg)
	})

	appVersion := "1.0.0"
	if app := fyne.CurrentApp(); app != nil {
		if ver := app.Metadata().Version; ver != "" {
			appVersion = ver
		}
	}

	topContent := container.NewVBox(
		widget.NewLabelWithStyle(fmt.Sprintf("GoOut 控制面板 v%s (内存版)", appVersion), fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
		statusLabel,
		container.NewHBox(toggleBtn, configBtn),
		widget.NewSeparator(),
		widget.NewLabel("日志:"),
	)

	content := container.NewBorder(topContent, nil, nil, nil, container.NewVScroll(logArea))

	w.SetContent(content)
	w.Resize(fyne.NewSize(450, 650))
}

func showSettingsDialog(w fyne.Window, cfg *data.Config) {
	// 读取 V2Ray 配置文件内容到 map[string]interface{}
	configPath := cfg.V2RayConfigPath
	if configPath == "" {
		configPath = "goout.json"
	}
	fileBytes, err := os.ReadFile(configPath)
	var rawCfg map[string]interface{}
	if err != nil {
		rawCfg = make(map[string]interface{})
	} else {
		_ = json.Unmarshal(fileBytes, &rawCfg)
	}

	// 1. 主设置 UI 组件
	tunIPEntry := widget.NewEntry()
	tunIPEntry.SetText(cfg.TunIP)

	mainForm := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "TUN IP", Widget: tunIPEntry},
		},
	}
	mainTab := container.NewVBox(widget.NewLabelWithStyle("基本连接参数设置", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}), mainForm)

	// 2. 入站设置 UI
	inboundsContainer := container.NewVBox()
	var updateInboundsUI func()
	
	getInboundsList := func() []interface{} {
		if rawCfg["inbounds"] != nil {
			if list, ok := rawCfg["inbounds"].([]interface{}); ok {
				return list
			}
		}
		return []interface{}{}
	}

	updateInboundsUI = func() {
		inboundsContainer.Objects = nil
		inboundsList := getInboundsList()

		for i, item := range inboundsList {
			inboundMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			tagEntry := widget.NewEntry()
			if t, ok := inboundMap["tag"].(string); ok {
				tagEntry.SetText(t)
			}
			portEntryItem := widget.NewEntry()
			if p, ok := inboundMap["port"].(float64); ok {
				portEntryItem.SetText(strconv.Itoa(int(p)))
			} else if p, ok := inboundMap["port"].(int); ok {
				portEntryItem.SetText(strconv.Itoa(p))
			}
			listenEntry := widget.NewEntry()
			if l, ok := inboundMap["listen"].(string); ok {
				listenEntry.SetText(l)
			}
			protoEntry := widget.NewEntry()
			if pr, ok := inboundMap["protocol"].(string); ok {
				protoEntry.SetText(pr)
			}

			idx := i
			delBtn := widget.NewButton("删除此入站", func() {
				list := getInboundsList()
				if idx >= 0 && idx < len(list) {
					list = append(list[:idx], list[idx+1:]...)
					rawCfg["inbounds"] = list
					updateInboundsUI()
				}
			})

			box := container.NewVBox(
				widget.NewLabel(fmt.Sprintf("入站 #%d", i+1)),
				widget.NewForm(
					&widget.FormItem{Text: "Tag", Widget: tagEntry},
					&widget.FormItem{Text: "Port", Widget: portEntryItem},
					&widget.FormItem{Text: "Listen", Widget: listenEntry},
					&widget.FormItem{Text: "Protocol", Widget: protoEntry},
				),
				delBtn,
				widget.NewSeparator(),
			)

			// 实时同步修改回 inboundMap
			tagEntry.OnChanged = func(s string) { inboundMap["tag"] = s }
			portEntryItem.OnChanged = func(s string) {
				if pi, err := strconv.Atoi(s); err == nil {
					inboundMap["port"] = pi
				}
			}
			listenEntry.OnChanged = func(s string) { inboundMap["listen"] = s }
			protoEntry.OnChanged = func(s string) { inboundMap["protocol"] = s }

			inboundsContainer.Add(box)
		}
	}
	updateInboundsUI()

	addInboundBtn := widget.NewButton("增加入站", func() {
		list := getInboundsList()
		newInbound := map[string]interface{}{
			"tag":      "in-" + strconv.Itoa(len(list)+1),
			"port":     1080 + len(list),
			"listen":   "127.0.0.1",
			"protocol": "socks",
		}
		list = append(list, newInbound)
		rawCfg["inbounds"] = list
		updateInboundsUI()
	})

	inboundTab := container.NewBorder(addInboundBtn, nil, nil, nil, container.NewVScroll(inboundsContainer))

	// 3. 出站设置 UI
	outboundsContainer := container.NewVBox()
	var updateOutboundsUI func()

	getOutboundsList := func() []interface{} {
		if rawCfg["outbounds"] != nil {
			if list, ok := rawCfg["outbounds"].([]interface{}); ok {
				return list
			}
		}
		return []interface{}{}
	}

	updateOutboundsUI = func() {
		outboundsContainer.Objects = nil
		outboundsList := getOutboundsList()

		for i, item := range outboundsList {
			outMap, ok := item.(map[string]interface{})
			if !ok {
				continue
			}

			tagEntry := widget.NewEntry()
			if t, ok := outMap["tag"].(string); ok {
				tagEntry.SetText(t)
			}
			protoEntry := widget.NewEntry()
			if pr, ok := outMap["protocol"].(string); ok {
				protoEntry.SetText(pr)
			}

			// 解析嵌套字段: address, port, id, serverName, echDohServer, path
			var address, id, serverName, echDohServer, path string
			var port int

			if settings, ok := outMap["settings"].(map[string]interface{}); ok {
				if vnext, ok := settings["vnext"].([]interface{}); ok && len(vnext) > 0 {
					if vn, ok := vnext[0].(map[string]interface{}); ok {
						if addr, ok := vn["address"].(string); ok { address = addr }
						if p, ok := vn["port"].(float64); ok { port = int(p) } else if p, ok := vn["port"].(int); ok { port = p }
						if users, ok := vn["users"].([]interface{}); ok && len(users) > 0 {
							if u, ok := users[0].(map[string]interface{}); ok {
								if uid, ok := u["id"].(string); ok { id = uid }
							}
						}
					}
				}
			}

			if stream, ok := outMap["streamSettings"].(map[string]interface{}); ok {
				if tls, ok := stream["tlsSettings"].(map[string]interface{}); ok {
					if sn, ok := tls["serverName"].(string); ok { serverName = sn }
					if ed, ok := tls["echDohServer"].(string); ok { echDohServer = ed }
				}
				if ws, ok := stream["wsSettings"].(map[string]interface{}); ok {
					if pth, ok := ws["path"].(string); ok { path = pth }
				}
			}

			addrEntryItem := widget.NewEntry()
			addrEntryItem.SetText(address)
			portEntryItem := widget.NewEntry()
			if port > 0 { portEntryItem.SetText(strconv.Itoa(port)) }
			idEntry := widget.NewEntry()
			idEntry.SetText(id)
			snEntry := widget.NewEntry()
			snEntry.SetText(serverName)
			edEntry := widget.NewEntry()
			edEntry.SetText(echDohServer)
			pathEntry := widget.NewEntry()
			pathEntry.SetText(path)

			idx := i
			delBtn := widget.NewButton("删除此出站", func() {
				list := getOutboundsList()
				if idx >= 0 && idx < len(list) {
					list = append(list[:idx], list[idx+1:]...)
					rawCfg["outbounds"] = list
					updateOutboundsUI()
				}
			})

			box := container.NewVBox(
				widget.NewLabel(fmt.Sprintf("出站 #%d", i+1)),
				widget.NewForm(
					&widget.FormItem{Text: "Tag", Widget: tagEntry},
					&widget.FormItem{Text: "Protocol", Widget: protoEntry},
					&widget.FormItem{Text: "Address", Widget: addrEntryItem},
					&widget.FormItem{Text: "Port", Widget: portEntryItem},
					&widget.FormItem{Text: "ID", Widget: idEntry},
					&widget.FormItem{Text: "ServerName", Widget: snEntry},
					&widget.FormItem{Text: "EchDohServer", Widget: edEntry},
					&widget.FormItem{Text: "Path", Widget: pathEntry},
				),
				delBtn,
				widget.NewSeparator(),
			)

			// 实时同步回嵌套结构
			tagEntry.OnChanged = func(s string) { outMap["tag"] = s }
			protoEntry.OnChanged = func(s string) { outMap["protocol"] = s }
			addrEntryItem.OnChanged = func(s string) {
				settings, _ := outMap["settings"].(map[string]interface{})
				if settings == nil { settings = make(map[string]interface{}); outMap["settings"] = settings }
				vnext, _ := settings["vnext"].([]interface{})
				var vn map[string]interface{}
				if len(vnext) > 0 {
					vn, _ = vnext[0].(map[string]interface{})
				}
				if vn == nil {
					vn = make(map[string]interface{})
					vnext = []interface{}{vn}
					settings["vnext"] = vnext
				}
				vn["address"] = s
			}
			portEntryItem.OnChanged = func(s string) {
				pi, err := strconv.Atoi(s)
				if err != nil { return }
				settings, _ := outMap["settings"].(map[string]interface{})
				if settings == nil { settings = make(map[string]interface{}); outMap["settings"] = settings }
				vnext, _ := settings["vnext"].([]interface{})
				var vn map[string]interface{}
				if len(vnext) > 0 {
					vn, _ = vnext[0].(map[string]interface{})
				}
				if vn == nil {
					vn = make(map[string]interface{})
					vnext = []interface{}{vn}
					settings["vnext"] = vnext
				}
				vn["port"] = pi
			}
			idEntry.OnChanged = func(s string) {
				settings, _ := outMap["settings"].(map[string]interface{})
				if settings == nil { settings = make(map[string]interface{}); outMap["settings"] = settings }
				vnext, _ := settings["vnext"].([]interface{})
				var vn map[string]interface{}
				if len(vnext) > 0 {
					vn, _ = vnext[0].(map[string]interface{})
				}
				if vn == nil {
					vn = make(map[string]interface{})
					vnext = []interface{}{vn}
					settings["vnext"] = vnext
				}
				users, _ := vn["users"].([]interface{})
				var u map[string]interface{}
				if len(users) > 0 {
					u, _ = users[0].(map[string]interface{})
				}
				if u == nil {
					u = map[string]interface{}{"encryption": "none"}
					users = []interface{}{u}
					vn["users"] = users
				}
				u["id"] = s
			}
			snEntry.OnChanged = func(s string) {
				stream, _ := outMap["streamSettings"].(map[string]interface{})
				if stream == nil { stream = make(map[string]interface{}); outMap["streamSettings"] = stream }
				tls, _ := stream["tlsSettings"].(map[string]interface{})
				if tls == nil { tls = make(map[string]interface{}); stream["tlsSettings"] = tls }
				tls["serverName"] = s
			}
			edEntry.OnChanged = func(s string) {
				stream, _ := outMap["streamSettings"].(map[string]interface{})
				if stream == nil { stream = make(map[string]interface{}); outMap["streamSettings"] = stream }
				tls, _ := stream["tlsSettings"].(map[string]interface{})
				if tls == nil { tls = make(map[string]interface{}); stream["tlsSettings"] = tls }
				tls["echDohServer"] = s
			}
			pathEntry.OnChanged = func(s string) {
				stream, _ := outMap["streamSettings"].(map[string]interface{})
				if stream == nil { stream = make(map[string]interface{}); outMap["streamSettings"] = stream }
				ws, _ := stream["wsSettings"].(map[string]interface{})
				if ws == nil { ws = make(map[string]interface{}); stream["wsSettings"] = ws }
				ws["path"] = s
			}

			outboundsContainer.Add(box)
		}
	}
	updateOutboundsUI()

	addOutboundBtn := widget.NewButton("增加出站", func() {
		list := getOutboundsList()
		newOut := map[string]interface{}{
			"tag":      "out-" + strconv.Itoa(len(list)+1),
			"protocol": "vless",
			"settings": map[string]interface{}{
				"vnext": []interface{}{
					map[string]interface{}{
						"address": "127.0.0.1",
						"port":    443,
						"users": []interface{}{
							map[string]interface{}{
								"id":         "",
								"encryption": "none",
							},
						},
					},
				},
			},
		}
		list = append(list, newOut)
		rawCfg["outbounds"] = list
		updateOutboundsUI()
	})

	outboundTab := container.NewBorder(addOutboundBtn, nil, nil, nil, container.NewVScroll(outboundsContainer))

	// 4. 路由设置 UI
	routingMap, _ := rawCfg["routing"].(map[string]interface{})
	if routingMap == nil {
		routingMap = make(map[string]interface{})
		rawCfg["routing"] = routingMap
	}

	domainStrategyEntry := widget.NewEntry()
	if ds, ok := routingMap["domainStrategy"].(string); ok {
		domainStrategyEntry.SetText(ds)
	}
	domainStrategyEntry.OnChanged = func(s string) {
		routingMap["domainStrategy"] = s
	}

	balancersContainer := container.NewVBox()
	var updateBalancersUI func()
	getBalancersList := func() []interface{} {
		if routingMap["balancers"] != nil {
			if list, ok := routingMap["balancers"].([]interface{}); ok {
				return list
			}
		}
		return []interface{}{}
	}

	updateBalancersUI = func() {
		balancersContainer.Objects = nil
		list := getBalancersList()
		for i, item := range list {
			bMap, ok := item.(map[string]interface{})
			if !ok { continue }
			tagEntry := widget.NewEntry()
			if t, ok := bMap["tag"].(string); ok { tagEntry.SetText(t) }
			idx := i
			delBtn := widget.NewButton("删除此平衡器", func() {
				lst := getBalancersList()
				if idx >= 0 && idx < len(lst) {
					lst = append(lst[:idx], lst[idx+1:]...)
					routingMap["balancers"] = lst
					updateBalancersUI()
				}
			})
			tagEntry.OnChanged = func(s string) { bMap["tag"] = s }
			balancersContainer.Add(container.NewVBox(
				widget.NewLabel(fmt.Sprintf("平衡器 #%d", i+1)),
				widget.NewForm(&widget.FormItem{Text: "Tag", Widget: tagEntry}),
				delBtn,
				widget.NewSeparator(),
			))
		}
	}
	updateBalancersUI()

	addBalancerBtn := widget.NewButton("增加路由(平衡器)", func() {
		lst := getBalancersList()
		newB := map[string]interface{}{"tag": "balancer-" + strconv.Itoa(len(lst)+1)}
		lst = append(lst, newB)
		routingMap["balancers"] = lst
		updateBalancersUI()
	})

	rulesContainer := container.NewVBox()
	var updateRulesUI func()
	getRulesList := func() []interface{} {
		if routingMap["rules"] != nil {
			if list, ok := routingMap["rules"].([]interface{}); ok {
				return list
			}
		}
		return []interface{}{}
	}

	updateRulesUI = func() {
		rulesContainer.Objects = nil
		list := getRulesList()
		for i, item := range list {
			rMap, ok := item.(map[string]interface{})
			if !ok { continue }

			outboundTagEntry := widget.NewEntry()
			if ot, ok := rMap["outboundTag"].(string); ok { outboundTagEntry.SetText(ot) }
			balancerTagEntry := widget.NewEntry()
			if bt, ok := rMap["balancerTag"].(string); ok { balancerTagEntry.SetText(bt) }
			
			// inboundTag can be slice or string
			var inboundTagsStr string
			if its, ok := rMap["inboundTag"].([]interface{}); ok {
				var strs []string
				for _, s := range its { if str, ok := s.(string); ok { strs = append(strs, str) } }
				inboundTagsStr = strings.Join(strs, ",")
			} else if its, ok := rMap["inboundTag"].(string); ok {
				inboundTagsStr = its
			}

			// domain can be slice or string
			var domainStr string
			if ds, ok := rMap["domain"].([]interface{}); ok {
				var strs []string
				for _, s := range ds { if str, ok := s.(string); ok { strs = append(strs, str) } }
				domainStr = strings.Join(strs, "\n")
			} else if ds, ok := rMap["domain"].(string); ok {
				domainStr = ds
			}

			// ip can be slice or string
			var ipStr string
			if ips, ok := rMap["ip"].([]interface{}); ok {
				var strs []string
				for _, s := range ips { if str, ok := s.(string); ok { strs = append(strs, str) } }
				ipStr = strings.Join(strs, "\n")
			} else if ips, ok := rMap["ip"].(string); ok {
				ipStr = ips
			}

			itEntry := widget.NewEntry()
			itEntry.SetText(inboundTagsStr)
			domEntry := widget.NewMultiLineEntry()
			domEntry.SetText(domainStr)
			ipEntry := widget.NewMultiLineEntry()
			ipEntry.SetText(ipStr)

			idx := i
			delBtn := widget.NewButton("删除此规则", func() {
				lst := getRulesList()
				if idx >= 0 && idx < len(lst) {
					lst = append(lst[:idx], lst[idx+1:]...)
					routingMap["rules"] = lst
					updateRulesUI()
				}
			})

			outboundTagEntry.OnChanged = func(s string) { rMap["outboundTag"] = s }
			balancerTagEntry.OnChanged = func(s string) { rMap["balancerTag"] = s }
			itEntry.OnChanged = func(s string) {
				parts := strings.Split(s, ",")
				var clean []interface{}
				for _, p := range parts {
					trim := strings.TrimSpace(p)
					if trim != "" { clean = append(clean, trim) }
				}
				rMap["inboundTag"] = clean
			}
			domEntry.OnChanged = func(s string) {
				lines := strings.Split(s, "\n")
				var clean []interface{}
				for _, l := range lines {
					trim := strings.TrimSpace(l)
					if trim != "" { clean = append(clean, trim) }
				}
				rMap["domain"] = clean
			}
			ipEntry.OnChanged = func(s string) {
				lines := strings.Split(s, "\n")
				var clean []interface{}
				for _, l := range lines {
					trim := strings.TrimSpace(l)
					if trim != "" { clean = append(clean, trim) }
				}
				rMap["ip"] = clean
			}

			rulesContainer.Add(container.NewVBox(
				widget.NewLabel(fmt.Sprintf("规则 #%d (type: field)", i+1)),
				widget.NewForm(
					&widget.FormItem{Text: "OutboundTag", Widget: outboundTagEntry},
					&widget.FormItem{Text: "BalancerTag", Widget: balancerTagEntry},
					&widget.FormItem{Text: "InboundTag(逗号)", Widget: itEntry},
					&widget.FormItem{Text: "Domain(每行一个)", Widget: domEntry},
					&widget.FormItem{Text: "IP(每行一个)", Widget: ipEntry},
				),
				delBtn,
				widget.NewSeparator(),
			))
		}
	}
	updateRulesUI()

	addRuleBtn := widget.NewButton("增加路由(规则)", func() {
		lst := getRulesList()
		newR := map[string]interface{}{
			"type":        "field",
			"outboundTag": "direct",
		}
		lst = append(lst, newR)
		routingMap["rules"] = lst
		updateRulesUI()
	})

	routingTab := container.NewVBox(
		widget.NewForm(&widget.FormItem{Text: "DomainStrategy", Widget: domainStrategyEntry}),
		widget.NewLabel("负载均衡器 (Balancers):"),
		addBalancerBtn,
		balancersContainer,
		widget.NewSeparator(),
		widget.NewLabel("分流规则 (Rules):"),
		addRuleBtn,
		rulesContainer,
	)

	// AppTabs 组合四个Tab
	tabs := container.NewAppTabs(
		container.NewTabItem("主设置", mainTab),
		container.NewTabItem("入站设置", inboundTab),
		container.NewTabItem("出站设置", outboundTab),
		container.NewTabItem("路由设置", container.NewVScroll(routingTab)),
	)

	saveBtn := widget.NewButton("保存所有配置", func() {
		// 保存主设置到 cfg
		cfg.TunIP = tunIPEntry.Text

		_ = data.SaveConfig("config_save.json", cfg)

		// 保存 V2Ray JSON 配置
		if outBytes, err := json.MarshalIndent(rawCfg, "", "    "); err == nil {
			_ = os.WriteFile(configPath, outBytes, 0644)
		}

		dialog.ShowInformation("成功", "所有配置及 V2Ray 核心规则已成功保存！", w)
	})

	dialogContent := container.NewBorder(nil, saveBtn, nil, nil, tabs)

	d := dialog.NewCustom("系统与代理设置", "关闭", dialogContent, w)
	d.Resize(fyne.NewSize(550, 700))
	d.Show()
}
