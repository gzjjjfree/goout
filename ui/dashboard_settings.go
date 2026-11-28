// ui/dashboard_settings.go
package ui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/gzjjjfree/goout/core"
	"github.com/gzjjjfree/goout/data"
	"github.com/gzjjjfree/goout/service"
	"github.com/gzjjjfree/goout/util"
)

func showConfigCheckResultDialog(w fyne.Window, statusTitle string, configContent string) {
	configTextEntry := widget.NewMultiLineEntry()
	configTextEntry.SetText(configContent)
	configTextEntry.Wrapping = fyne.TextWrapWord

	scroll := container.NewVScroll(configTextEntry)
	scroll.SetMinSize(fyne.NewSize(500, 350))

	statusLabel := widget.NewLabelWithStyle(statusTitle, fyne.TextAlignLeading, fyne.TextStyle{Bold: true})
	statusLabel.Wrapping = fyne.TextWrapWord

	infoBox := container.NewVBox(
		statusLabel,
		widget.NewLabel("Config 全文如下："),
		scroll,
	)

	dInfo := dialog.NewCustom("配置保存与 V2Ray 检查结果", "确定", infoBox, w)
	dInfo.Resize(fyne.NewSize(600, 500))
	dInfo.Show()
}

func showSettingsDialog(w fyne.Window, cfg *data.Config, vpnMgr *service.VPNServiceManager) {
	var d dialog.Dialog

	configPath := cfg.V2RayConfigPath
	if configPath == "" {
		configPath = "goout.json"
	}
	fileBytes, err := os.ReadFile(configPath)
	var rawCfg map[string]interface{}

	defaultBytes := data.GetDefaultConfigBytes()
	_ = json.Unmarshal(defaultBytes, &rawCfg)

	if err == nil {
		if mergedBytes, mergeErr := data.MergeConfigTemplate(defaultBytes, fileBytes); mergeErr == nil {
			_ = json.Unmarshal(mergedBytes, &rawCfg)
		}
	}

	tunIPEntry := widget.NewEntry()
	tunIPEntry.SetText(cfg.TunIP)

	mainForm := &widget.Form{
		Items: []*widget.FormItem{
			{Text: "TUN IP", Widget: tunIPEntry},
		},
	}
	mainTab := container.NewVBox(
		widget.NewLabelWithStyle("基本连接参数设置", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		mainForm,
	)

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
			if pVal, exists := inboundMap["port"]; exists && pVal != nil {
				portEntryItem.SetText(fmt.Sprintf("%v", pVal))
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

			tagEntry.OnChanged = func(s string) { inboundMap["tag"] = s }
			portEntryItem.OnChanged = func(s string) {
				if pi, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && pi > 0 {
					inboundMap["port"] = pi
				}
			}
			listenEntry.OnChanged = func(s string) { inboundMap["listen"] = s }
			protoEntry.OnChanged = func(s string) { inboundMap["protocol"] = s }

			inboundsContainer.Add(box)
		}
		inboundsContainer.Refresh()
	}
	updateInboundsUI()

	addInboundBtn := widget.NewButton("增加入站", func() {
		list := getInboundsList()
		newInbound := map[string]interface{}{
			"tag":      "in-" + strconv.Itoa(len(list)+1),
			"port":     10808 + len(list),
			"listen":   "127.0.0.1",
			"protocol": "socks",
			"sniffing": map[string]interface{}{
				"enabled": true,
				"destOverride": []interface{}{
					"http",
					"tls",
				},
				"metadataOnly": false,
			},
			"settings": map[string]interface{}{
				"auth":    "noauth",
				"udp":     true,
				"ip":      nil,
				"address": nil,
				"clients": nil,
			},
			"streamSettings": nil,
		}
		list = append(list, newInbound)
		rawCfg["inbounds"] = list
		updateInboundsUI()
	})

	inboundTab := container.NewBorder(addInboundBtn, nil, nil, nil, container.NewVScroll(inboundsContainer))

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

			if pr, ok := outMap["protocol"].(string); ok {
				if pr == "freedom" || pr == "blackhole" {
					continue
				}
			}

			tagEntry := widget.NewEntry()
			if t, ok := outMap["tag"].(string); ok {
				tagEntry.SetText(t)
			}
			protoEntry := widget.NewEntry()
			if pr, ok := outMap["protocol"].(string); ok {
				protoEntry.SetText(pr)
			}

			var address, id, serverName, echDohServer, path, host, userAgent string
			var port int

			if settings, ok := outMap["settings"].(map[string]interface{}); ok {
				if vnext, ok := settings["vnext"].([]interface{}); ok && len(vnext) > 0 {
					if vn, ok := vnext[0].(map[string]interface{}); ok {
						if addr, ok := vn["address"].(string); ok {
							address = addr
						}
						if p, ok := vn["port"].(float64); ok {
							port = int(p)
						} else if p, ok := vn["port"].(int); ok {
							port = p
						}
						if users, ok := vn["users"].([]interface{}); ok && len(users) > 0 {
							if u, ok := users[0].(map[string]interface{}); ok {
								if uid, ok := u["id"].(string); ok {
									id = uid
								}
							}
						}
					}
				}
			}

			if stream, ok := outMap["streamSettings"].(map[string]interface{}); ok {
				if tls, ok := stream["tlsSettings"].(map[string]interface{}); ok {
					if sn, ok := tls["serverName"].(string); ok {
						serverName = sn
					}
					if ed, ok := tls["echDohServer"].(string); ok {
						echDohServer = ed
					}
				}
				if ws, ok := stream["wsSettings"].(map[string]interface{}); ok {
					if pth, ok := ws["path"].(string); ok {
						path = pth
					}
					if headers, ok := ws["headers"].(map[string]interface{}); ok {
						if h, ok := headers["Host"].(string); ok {
							host = h
						}
						if ua, ok := headers["User-Agent"].(string); ok {
							userAgent = ua
						}
					}
				}
			}

			addrEntryItem := widget.NewEntry()
			addrEntryItem.SetText(address)
			portEntryItem := widget.NewEntry()
			if port > 0 {
				portEntryItem.SetText(strconv.Itoa(port))
			}
			idEntry := widget.NewEntry()
			idEntry.SetText(id)
			snEntry := widget.NewEntry()
			snEntry.SetText(serverName)
			edEntry := widget.NewEntry()
			edEntry.SetText(echDohServer)
			pathEntry := widget.NewEntry()
			pathEntry.SetText(path)
			hostEntry := widget.NewEntry()
			hostEntry.SetText(host)
			userAgentEntry := widget.NewEntry()
			userAgentEntry.SetText(userAgent)

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
					&widget.FormItem{Text: "Host", Widget: hostEntry},
					&widget.FormItem{Text: "User-Agent", Widget: userAgentEntry},
				),
				delBtn,
				widget.NewSeparator(),
			)

			tagEntry.OnChanged = func(s string) { outMap["tag"] = s }
			protoEntry.OnChanged = func(s string) { outMap["protocol"] = s }
			addrEntryItem.OnChanged = func(s string) {
				settings, _ := outMap["settings"].(map[string]interface{})
				if settings == nil {
					settings = make(map[string]interface{})
					outMap["settings"] = settings
				}
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
				pi, err := strconv.Atoi(strings.TrimSpace(s))
				if err != nil {
					return
				}
				settings, _ := outMap["settings"].(map[string]interface{})
				if settings == nil {
					settings = make(map[string]interface{})
					outMap["settings"] = settings
				}
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
				if settings == nil {
					settings = make(map[string]interface{})
					outMap["settings"] = settings
				}
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
				if stream == nil {
					stream = make(map[string]interface{})
					outMap["streamSettings"] = stream
				}
				tls, _ := stream["tlsSettings"].(map[string]interface{})
				if tls == nil {
					tls = make(map[string]interface{})
					stream["tlsSettings"] = tls
				}
				tls["serverName"] = s
			}
			edEntry.OnChanged = func(s string) {
				stream, _ := outMap["streamSettings"].(map[string]interface{})
				if stream == nil {
					stream = make(map[string]interface{})
					outMap["streamSettings"] = stream
				}
				tls, _ := stream["tlsSettings"].(map[string]interface{})
				if tls == nil {
					tls = make(map[string]interface{})
					stream["tlsSettings"] = tls
				}
				tls["echDohServer"] = s
			}
			pathEntry.OnChanged = func(s string) {
				stream, _ := outMap["streamSettings"].(map[string]interface{})
				if stream == nil {
					stream = make(map[string]interface{})
					outMap["streamSettings"] = stream
				}
				ws, _ := stream["wsSettings"].(map[string]interface{})
				if ws == nil {
					ws = make(map[string]interface{})
					stream["wsSettings"] = ws
				}
				ws["path"] = s
			}
			hostEntry.OnChanged = func(s string) {
				stream, _ := outMap["streamSettings"].(map[string]interface{})
				if stream == nil {
					stream = make(map[string]interface{})
					outMap["streamSettings"] = stream
				}
				ws, _ := stream["wsSettings"].(map[string]interface{})
				if ws == nil {
					ws = make(map[string]interface{})
					stream["wsSettings"] = ws
				}
				headers, _ := ws["headers"].(map[string]interface{})
				if headers == nil {
					headers = make(map[string]interface{})
					ws["headers"] = headers
				}
				headers["Host"] = s
			}
			userAgentEntry.OnChanged = func(s string) {
				stream, _ := outMap["streamSettings"].(map[string]interface{})
				if stream == nil {
					stream = make(map[string]interface{})
					outMap["streamSettings"] = stream
				}
				ws, _ := stream["wsSettings"].(map[string]interface{})
				if ws == nil {
					ws = make(map[string]interface{})
					stream["wsSettings"] = ws
				}
				headers, _ := ws["headers"].(map[string]interface{})
				if headers == nil {
					headers = make(map[string]interface{})
					ws["headers"] = headers
				}
				headers["User-Agent"] = s
			}

			outboundsContainer.Add(box)
		}
		outboundsContainer.Refresh()
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
			"streamSettings": map[string]interface{}{
				"network":  "ws",
				"security": "tls",
				"tlsSettings": map[string]interface{}{
					"serverName":    "",
					"echDohServer":  "",
					"allowInsecure": false,
				},
				"wsSettings": map[string]interface{}{
					"path": "/ws",
					"headers": map[string]interface{}{
						"Host":       "",
						"User-Agent": "",
					},
				},
			},
			"mux": map[string]interface{}{
				"enabled":     true,
				"concurrency": 8,
			},
		}
		list = append(list, newOut)
		rawCfg["outbounds"] = list
		updateOutboundsUI()
	})

	outboundTab := container.NewBorder(addOutboundBtn, nil, nil, nil, container.NewVScroll(outboundsContainer))

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

	hasBalancer := false
	if lst, ok := routingMap["balancers"].([]interface{}); ok {
		for _, item := range lst {
			if bMap, ok := item.(map[string]interface{}); ok {
				if bMap["tag"] == "cdn-balancer-proxy" {
					hasBalancer = true
					break
				}
			}
		}
	}
	if !hasBalancer {
		routingMap["balancers"] = []interface{}{
			map[string]interface{}{
				"tag":      "cdn-balancer-proxy",
				"selector": []interface{}{"cdn-"},
				"strategy": map[string]interface{}{"type": "random"},
			},
		}
	}

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
			if !ok {
				continue
			}

			outboundTagEntry := widget.NewEntry()
			if ot, ok := rMap["outboundTag"].(string); ok {
				outboundTagEntry.SetText(ot)
			}
			balancerTagEntry := widget.NewEntry()
			if bt, ok := rMap["balancerTag"].(string); ok {
				balancerTagEntry.SetText(bt)
			}

			var portStr string
			if pVal, exists := rMap["port"]; exists && pVal != nil {
				switch p := pVal.(type) {
				case float64:
					portStr = strconv.Itoa(int(p))
				case int:
					portStr = strconv.Itoa(p)
				case string:
					portStr = p
				default:
					portStr = fmt.Sprintf("%v", pVal)
				}
			}

			var inboundTagsStr string
			if its, ok := rMap["inboundTag"].([]interface{}); ok {
				var strs []string
				for _, s := range its {
					if str, ok := s.(string); ok {
						strs = append(strs, str)
					}
				}
				inboundTagsStr = strings.Join(strs, ",")
			} else if its, ok := rMap["inboundTag"].(string); ok {
				inboundTagsStr = its
			}

			var domainStr string
			if ds, ok := rMap["domain"].([]interface{}); ok {
				var strs []string
				for _, s := range ds {
					if str, ok := s.(string); ok {
						strs = append(strs, str)
					}
				}
				domainStr = strings.Join(strs, "\n")
			} else if ds, ok := rMap["domain"].(string); ok {
				domainStr = ds
			}

			var ipStr string
			if ips, ok := rMap["ip"].([]interface{}); ok {
				var strs []string
				for _, s := range ips {
					if str, ok := s.(string); ok {
						strs = append(strs, str)
					}
				}
				ipStr = strings.Join(strs, "\n")
			} else if ips, ok := rMap["ip"].(string); ok {
				ipStr = ips
			}

			itEntry := widget.NewEntry()
			itEntry.SetText(inboundTagsStr)
			portEntry := widget.NewEntry()
			portEntry.SetText(portStr)
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
			portEntry.OnChanged = func(s string) {
				trimmed := strings.TrimSpace(s)
				if trimmed == "" {
					delete(rMap, "port")
					return
				}
				if pi, err := strconv.Atoi(trimmed); err == nil {
					rMap["port"] = pi
				} else {
					rMap["port"] = trimmed
				}
			}
			itEntry.OnChanged = func(s string) {
				parts := strings.Split(s, ",")
				var clean []interface{}
				for _, p := range parts {
					trim := strings.TrimSpace(p)
					if trim != "" {
						clean = append(clean, trim)
					}
				}
				rMap["inboundTag"] = clean
			}
			domEntry.OnChanged = func(s string) {
				lines := strings.Split(s, "\n")
				var clean []interface{}
				for _, l := range lines {
					trim := strings.TrimSpace(l)
					if trim != "" {
						clean = append(clean, trim)
					}
				}
				rMap["domain"] = clean
			}
			ipEntry.OnChanged = func(s string) {
				lines := strings.Split(s, "\n")
				var clean []interface{}
				for _, l := range lines {
					trim := strings.TrimSpace(l)
					if trim != "" {
						clean = append(clean, trim)
					}
				}
				rMap["ip"] = clean
			}

			rulesContainer.Add(container.NewVBox(
				widget.NewLabel(fmt.Sprintf("规则 #%d (type: field)", i+1)),
				widget.NewForm(
					&widget.FormItem{Text: "OutboundTag", Widget: outboundTagEntry},
					&widget.FormItem{Text: "BalancerTag", Widget: balancerTagEntry},
					&widget.FormItem{Text: "Port", Widget: portEntry},
					&widget.FormItem{Text: "InboundTag(逗号)", Widget: itEntry},
					&widget.FormItem{Text: "Domain(每行一个)", Widget: domEntry},
					&widget.FormItem{Text: "IP(每行一个)", Widget: ipEntry},
				),
				delBtn,
				widget.NewSeparator(),
			))
		}
		rulesContainer.Refresh()
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
		widget.NewLabel("平衡器 Tag : cdn-balancer-proxy"),
		widget.NewSeparator(),
		widget.NewLabel("分流规则 (Rules):"),
		addRuleBtn,
		rulesContainer,
	)

	tabs := container.NewAppTabs(
		container.NewTabItem("主设置", mainTab),
		container.NewTabItem("入站设置", inboundTab),
		container.NewTabItem("出站设置", outboundTab),
		container.NewTabItem("路由设置", container.NewVScroll(routingTab)),
	)

	var lastCheckedConfigHash string
	var hashMu sync.Mutex
	var saveBtn *widget.Button

	saveBtn = widget.NewButton("保存所有配置", func() {
		saveBtn.Disable()
		saveBtn.SetText("正在检查配置...")

		cfg.TunIP = tunIPEntry.Text
		vpnMgr.SetTunIP(cfg.TunIP)

		if inbounds, ok := rawCfg["inbounds"].([]interface{}); ok && len(inbounds) > 0 {
			for _, ib := range inbounds {
				if ibMap, ok := ib.(map[string]interface{}); ok {
					if p := util.ParsePortToInt(ibMap["port"]); p > 0 {
						cfg.SocksPort = p
						break
					}
				}
			}
		}

		_ = data.SaveConfig("config_save.json", cfg)

		outBytes, err := json.MarshalIndent(rawCfg, "", "    ")
		if err != nil {
			saveBtn.Enable()
			saveBtn.SetText("保存所有配置")
			if d != nil {
				d.Hide()
			}
			showConfigCheckResultDialog(
				w,
				fmt.Sprintf("配置保存失败 (JSON 序列化失败: %v)", err),
				fmt.Sprintf("%v", rawCfg),
			)
			return
		}

		if mergedBytes, mergeErr := data.MergeConfigTemplate(data.GetDefaultConfigBytes(), outBytes); mergeErr == nil {
			outBytes = mergedBytes
		}
		_ = os.WriteFile(configPath, outBytes, 0644)

		hashBytes := sha256.Sum256(outBytes)
		currentHash := hex.EncodeToString(hashBytes[:])

		hashMu.Lock()
		sameHash := currentHash == lastCheckedConfigHash
		hashMu.Unlock()

		if sameHash {
			saveBtn.Enable()
			saveBtn.SetText("保存所有配置")
			if d != nil {
				d.Hide()
			}
			showConfigCheckResultDialog(
				w,
				"配置保存成功 (配置未修改，跳过重复核心检查)！",
				string(outBytes),
			)
			return
		}

		go func(cfgBytes []byte, hashStr string) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[Settings] CheckConfig goroutine panic recovered: %v\n%s", r, debug.Stack())
					fyne.Do(func() {
						saveBtn.Enable()
						saveBtn.SetText("保存所有配置")
						if d != nil {
							d.Hide()
						}
						showConfigCheckResultDialog(
							w,
							fmt.Sprintf("配置已写入文件，但 V2Ray 核心检查发生异常: %v", r),
							string(cfgBytes),
						)
					})
				}
			}()

			mgr := core.GetManager(nil)
			if checkErr := mgr.CheckConfig(cfgBytes); checkErr != nil {
				log.Printf("[Settings] V2Ray CheckConfig failed: %v", checkErr)
				fyne.Do(func() {
					saveBtn.Enable()
					saveBtn.SetText("保存所有配置")
					if d != nil {
						d.Hide()
					}
					showConfigCheckResultDialog(
						w,
						fmt.Sprintf("配置已写入文件，但 V2Ray 核心检查未通过: %v", checkErr),
						string(cfgBytes),
					)
				})
				return
			}

			hashMu.Lock()
			lastCheckedConfigHash = hashStr
			hashMu.Unlock()

			fyne.Do(func() {
				saveBtn.Enable()
				saveBtn.SetText("保存所有配置")
				if d != nil {
					d.Hide()
				}
				successMsg := "配置保存成功，V2Ray 核心检查通过！"
				if runtime.GOOS == "android" {
					successMsg = "配置保存成功，监听端口已修改，请重启应用后生效！"
				}
				showConfigCheckResultDialog(
					w,
					successMsg,
					string(cfgBytes),
				)
			})
		}(outBytes, currentHash)
	})

	restoreDefaultBtn := widget.NewButton("恢复默认", func() {
		dialog.ShowConfirm("确认", "确定要恢复默认设置吗？这将重载原始的 goout.json 配置。", func(ok bool) {
			if !ok {
				return
			}
			defaultBytes := data.GetDefaultConfigBytes()
			_ = os.WriteFile(configPath, defaultBytes, 0644)

			_ = json.Unmarshal(defaultBytes, &rawCfg)
			cfg.TunIP = "10.0.0.2"
			tunIPEntry.SetText(cfg.TunIP)

			updateInboundsUI()
			updateOutboundsUI()

			routingMap, _ = rawCfg["routing"].(map[string]interface{})
			if routingMap == nil {
				routingMap = make(map[string]interface{})
				rawCfg["routing"] = routingMap
			}
			if ds, ok := routingMap["domainStrategy"].(string); ok {
				domainStrategyEntry.SetText(ds)
			} else {
				domainStrategyEntry.SetText("")
			}
			updateRulesUI()

			dialog.ShowInformation("成功", "已成功恢复默认设置！", w)
		}, w)
	})

	bottomContainer := container.NewHBox(restoreDefaultBtn, saveBtn)
	dialogContent := container.NewBorder(nil, bottomContainer, nil, nil, tabs)

	d = dialog.NewCustom("系统与代理设置", "关闭", dialogContent, w)
	d.Resize(fyne.NewSize(550, 700))
	d.Show()
}
