# Architecture Notes (ARCH_NOTES.md)

## 1. Core Architecture & Module Design
- **Entry Point**: [`main.go`](main.go:1) initializes Fyne app and calls [`ui.SetupDashboard`](ui/dashboard.go:1).
- **Process Management (`core/process.go`)**: Manages subprocesses (v2ray) by extracting embedded binaries (`v2ray`) directly to application private **files** storage (`/data/user/0/<pkg>/files/bin`), enforcing `/sdcard` and external storage protection checks, setting `0700` permissions via `os.Chmod`, and verifying executable status (`info.Mode() & 0100`) to completely prevent Android `noexec` and permission denied errors.
- **VPN Redirection (`service/vpn.go`)**: Manages Android `VpnService` permissions, TUN interfaces (`10.0.0.2`), and `0.0.0.0/0` traffic routing using pure Go packet processing.
- **Pure Go Network Stack (`jni/tun2socks.go` & `jni/tun2socks_stub.go`)**: Pure Go packet forwarding / SOCKS proxy tunnel implementation, eliminating CGO dependencies and simplifying cross-compilation.
- **Configuration (`data/config.go`)**: JSON serialization/deserialization for server settings, SOCKS port, TUN IP, and subscription management. Defaults to embedding and deploying [`data/goout.json`](data/goout.json:1) into app private files storage.
- **UI & Thread Safety (`ui/dashboard.go`)**: Fyne-based control panel displaying version number (`GoOut Control Panel v1.0.0`), start/stop toggle, settings dialog, asynchronous log updates using `fyne.Do`, word-wrapped multi-line log entry, and `container.NewBorder` layout.

## 2. AGENTS Guidance & AI Assistant Rules
- Followed Go, Android, Fyne, and non-root sandboxed process execution guidelines (v2rayNG model).

## 3. Project Status & Current Progress
- **Version Display**: Dynamically retrieves and displays app version via `fyne.CurrentApp().Metadata().Version` in [`ui/dashboard.go`](ui/dashboard.go:70), corresponding to GitHub release and Fyne build version.
- **Android Execution Fix**: Extracted binary to app private **files** directory (`files/bin`) rather than `cache` directory to prevent `permission denied` (exit status 126) caused by Android `noexec` cache partition mount.
- **Default Configuration**: Embedded [`data/goout.json`](data/goout.json:1) via `go:embed` in [`data/config.go`](data/config.go:1) and automatically deployed it as the default v2ray configuration file in app storage.
- **Shizuku Removal Verification**: Completely removed Shizuku, `su`, and external probing logic. (Note: Old logs showing fallback/Shizuku were due to running a pre-existing installed APK prior to recompilation).
- **Unit Tests**: All unit tests (`core/process_test.go`, `data/config_test.go`) passed successfully.
- **CI/CD Versioning (`.github/workflows/release.yml`)**: Enhanced GitHub Actions workflow to support manual version input (`workflow_dispatch`), git tag detection (`refs/tags/v*`), `git describe` fallback, and dynamic monotonic `appBuild` (`versionCode`) generation using `git rev-list --count HEAD`.
