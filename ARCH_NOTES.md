# Architecture Notes (ARCH_NOTES.md)

## 1. Core Architecture & Module Design
- **Entry Point**: [`main.go`](main.go:1) initializes Fyne app and calls [`ui.SetupDashboard`](ui/dashboard.go:21).
- **In-Memory V2Ray Core Manager (`core/manager.go`)**: Fully replaces obsolete process execution to bypass modern Android 10+ W^X (Write xor Execute) security restrictions. Integrates `github.com/gzjjjfree/v5-result` directly as a Go library package in memory. Features thread-safe lifecycle control (`Start`, `Stop`), `sync.Mutex` concurrency protection, `panic recovery` safeguarding the Fyne UI thread from core crashes, and custom log handler interception (`v2log.RegisterHandler`) routing logs to UI log callbacks.
- **Geodata & Asset Management (`data/config.go`)**: [`EnsureGeositeDat`](data/config.go:18) prioritizes the application's writable internal storage (`getWritableDir()`) when deploying embedded `geosite.dat` and setting `v2ray.location.asset` / `V2RAY_LOCATION_ASSET` environment variables, avoiding permission/non-writable directory issues on Android.
- **VPN Redirection (`service/vpn.go`)**: Manages Android `VpnService` permissions, TUN interfaces (`10.0.0.2`), and `0.0.0.0/0` traffic routing using pure Go packet processing.
- **Pure Go Network Stack (`jni/tun2socks.go` & `jni/tun2socks_stub.go`)**: Pure Go packet forwarding / SOCKS proxy tunnel implementation, eliminating CGO dependencies and simplifying cross-compilation.
- **Configuration & Routing (`data/config.go` & [`goout.json`](goout.json:1))**: JSON configuration with default VLESS setup (valid UUID) and routing balancers (`cdn-balancer-proxy`).
- **UI & Thread Safety & Multi-Tab Settings & Config Validation (`ui/dashboard.go`)**: Fyne-based control panel localized entirely in Chinese, featuring start/stop toggle, comprehensive multi-tab settings dialog (`主设置`, `入站设置`, `出站设置`, `路由设置`) supporting complete CRUD operations on `inbounds`, `outbounds`, `domainStrategy`, `balancers`, and `rules` with automatic nesting mapping (`vnext`, `tlsSettings`, `wsSettings`), automatic V2Ray config validation via [`core.V2rayManager.CheckConfig`](core/manager.go:176), full config text display dialog in scrollable view upon saving, asynchronous log updates using `fyne.Do`, and dual configuration persistence (`config_save.json` and V2Ray JSON).

## 2. AGENTS Guidance & AI Assistant Rules
- Followed Go, Android, Fyne, and sandboxed in-memory library execution guidelines.

## 3. Project Status & Current Progress
- **Multi-Tab Advanced Settings Interface**: Successfully implemented the comprehensive settings UI in [`ui/dashboard.go`](ui/dashboard.go:110) with 4 tabs (`主设置`, `入站设置`, `出站设置`, `路由设置`). Refactored `主设置` to retain only TUN IP setting. Optimized `入站设置` and `出站设置` layout using `container.NewBorder` so that scrollable content containers stretch fully to the bottom.
- **In-Memory V2Ray Core Manager**: Implemented [`core/manager.go`](core/manager.go:1) using `github.com/gzjjjfree/v5-result` and `v2core.StartInstance("json", configBytes)` with panic recovery and log redirection.
- **Geosite & Asset Resolution Fixed**: Updated [`EnsureGeositeDat`](data/config.go:18) to deploy `geosite.dat` universally and configure `v2ray.location.asset` environment variables.
- **Redundant File Cleanup**: Successfully removed obsolete `libv2ray.so` binaries (`core/bin/libv2ray.so`, `android/app/src/main/jniLibs/arm64-v8a/libv2ray.so`) and obsolete build artifacts (`v5-goout-android-arm64`) as V2Ray core is now fully compiled in-memory as a Go library. Cleaned up [`release.yml`](.github/workflows/release.yml:1) workflow accordingly.
- **APK Size Optimization**: Added `GOFLAGS="-ldflags=-s -w"` to [`release.yml`](.github/workflows/release.yml:78) to strip debugging symbols. Retained `github.com/gzjjjfree/v5-result/main/distro/all` as the standard initialization package for in-memory core loading.
