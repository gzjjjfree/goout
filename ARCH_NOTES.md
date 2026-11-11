# Architecture Notes (ARCH_NOTES.md)

## 1. Core Architecture & Module Design
- **Entry Point**: [`main.go`](main.go:1) initializes Fyne app and calls [`ui.SetupDashboard`](ui/dashboard.go:1).
- **Process Management (`core/process.go`)**: Manages subprocesses (v2ray) strictly in `/data/local/tmp/v2ray` (without falling back to temporary cache directories) via `os.MkdirAll` and Shizuku / context cancellation, thread-safe ring buffers (500 lines) for log streaming, and mutex locking.
- **VPN Redirection (`service/vpn.go`)**: Manages Android `VpnService` permissions, TUN interfaces (`10.0.0.2`), and `0.0.0.0/0` traffic routing using pure Go packet processing.
- **Pure Go Network Stack (`jni/tun2socks.go` & `jni/tun2socks_stub.go`)**: Replaced CGO bindings and native `.so` libraries with a pure Go packet forwarding / SOCKS proxy tunnel implementation, eliminating all CGO dependencies and simplifying cross-compilation.
- **Configuration (`data/config.go`)**: JSON serialization/deserialization for server settings, SOCKS port, TUN IP, and subscription management (default v2ray config path set to `/data/local/tmp/goout.json`).
- **UI & Thread Safety (`ui/dashboard.go`)**: Fyne-based control panel with start/stop toggle, settings dialog, asynchronous log updates using `fyne.Do`, word-wrapped multi-line log entry (`logArea.Wrapping = fyne.TextWrapWord`), and `container.NewBorder` layout ensuring the log scroll container extends vertically to the bottom of the window.

## 2. AGENTS Guidance & AI Assistant Rules
- Followed Go, Android, Fyne, and Shizuku integration guidelines.

## 3. Project Status & Current Progress
- **Completed**: All items in `todolist.md` are fully implemented and verified with unit tests (`data/config_test.go`, `core/process_test.go`) and successful build compilation.
- **Log Display**: Enabled automatic word wrapping (`fyne.TextWrapWord`) on the dashboard's multi-line log display entry.
- **Binary Execution & Configuration Paths**:
  - Main binary execution path strictly enforced at `/data/local/tmp/v2ray` (without fallback to `os.TempDir()`), utilizing `os.MkdirAll` to ensure directory existence.
  - Default V2Ray configuration path set to `/data/local/tmp/goout.json`.
- **UI Theme**: Configured application theme to light (`theme.LightTheme()`) ensuring clear font color visibility and contrast across all dashboard and dialog components.
- **V2Ray Integration Strategy**: V2Ray compiled binary (`v5-goout-android-arm64`) is statically embedded via `go:embed` into [`core/process.go`](core/process.go:15), automatically extracted and given executable permissions at runtime, and invoked via [`core/process.go`](core/process.go:57) `ProcessManager` using `exec.CommandContext` with `-config` parameter and real-time log streaming.
- **Release Automation**: Added [`build.sh`](build.sh:1) script and GitHub Actions workflow [`.github/workflows/release.yml`](.github/workflows/release.yml:1) to automate cross-platform release compilation (Android arm64 via bundletool & macOS arm64 via fyne package) for GoOut (`com.gzjjjfree.goout`, key alias `goout-alias`).
