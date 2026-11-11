# Architecture Notes (ARCH_NOTES.md)

## 1. Core Architecture & Module Design
- **Entry Point**: [`main.go`](main.go:1) initializes Fyne app and calls [`ui.SetupDashboard`](ui/dashboard.go:1).
- **Process Management (`core/process.go`)**: Manages subprocesses (v2ray) in `/data/local/tmp/` via Shizuku / context cancellation, thread-safe ring buffers (500 lines) for log streaming, and mutex locking.
- **VPN Redirection (`service/vpn.go`)**: Manages Android `VpnService` permissions, TUN interfaces (`10.0.0.2`), and `0.0.0.0/0` traffic routing.
- **JNI / Tun2socks (`jni/tun2socks.go` & `jni/tun2socks_stub.go`)**: Bridges C/Go for packet filtering and proxy tunneling with conditional build tags (`//go:build android` vs `//go:build !android`).
- **Configuration (`data/config.go`)**: JSON serialization/deserialization for server settings, SOCKS port, TUN IP, and subscription management.
- **UI & Thread Safety (`ui/dashboard.go`)**: Fyne-based control panel with start/stop toggle, settings dialog, and asynchronous log updates using `fyne.Do`.

## 2. AGENTS Guidance & AI Assistant Rules
- Followed Go, Android, Fyne, and Shizuku integration guidelines.

## 3. Project Status & Current Progress
- **Completed**: All items in `todolist.md` are fully implemented and verified with unit tests (`data/config_test.go`, `core/process_test.go`) and successful build compilation.
- **UI Theme**: Configured application theme to light (`theme.LightTheme()`) ensuring clear font color visibility and contrast across all dashboard and dialog components.
- **V2Ray Integration Strategy**: V2Ray compiled binary (`v5-goout-android-arm64`) is statically embedded via `go:embed` into [`core/process.go`](core/process.go:15), automatically extracted and given executable permissions at runtime, and invoked via [`core/process.go`](core/process.go:57) `ProcessManager` using `exec.CommandContext` with `-config` parameter and real-time log streaming.
