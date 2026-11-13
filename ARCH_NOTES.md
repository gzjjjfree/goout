# Architecture Notes (ARCH_NOTES.md)

## 1. Core Architecture & Module Design
- **Entry Point**: [`main.go`](main.go:1) initializes Fyne app and calls [`ui.SetupDashboard`](ui/dashboard.go:1).
- **Process Management (`core/process.go`)**: Manages subprocesses (`libv2ray.so`) by placing the binary as `libv2ray.so` in `android/app/src/main/jniLibs/arm64-v8a/` with `android:extractNativeLibs="true"` in [`AndroidManifest.xml`](android/app/src/main/AndroidManifest.xml:1). At runtime, it directly executes from Android's `nativeLibraryDir` (`/data/user/0/<pkg>/lib/libv2ray.so` or `/data/data/<pkg>/lib/libv2ray.so`), completely bypassing any runtime file writing or permission denied errors, while maintaining fallback embedding for desktop/testing.
- **VPN Redirection (`service/vpn.go`)**: Manages Android `VpnService` permissions, TUN interfaces (`10.0.0.2`), and `0.0.0.0/0` traffic routing using pure Go packet processing.
- **Pure Go Network Stack (`jni/tun2socks.go` & `jni/tun2socks_stub.go`)**: Pure Go packet forwarding / SOCKS proxy tunnel implementation, eliminating CGO dependencies and simplifying cross-compilation.
- **Configuration (`data/config.go`)**: JSON serialization/deserialization for server settings, SOCKS port, TUN IP, and subscription management. Defaults to embedding and deploying [`data/goout.json`](data/goout.json:1) into app private files storage.
- **UI & Thread Safety (`ui/dashboard.go`)**: Fyne-based control panel displaying version number (`GoOut Control Panel v1.0.0`), start/stop toggle, settings dialog, asynchronous log updates using `fyne.Do`, word-wrapped multi-line log entry, and `container.NewBorder` layout.

## 2. AGENTS Guidance & AI Assistant Rules
- Followed Go, Android, Fyne, and non-root sandboxed process execution guidelines (v2rayNG model).

## 3. Project Status & Current Progress
- **Version Display**: Dynamically retrieves and displays app version via `fyne.CurrentApp().Metadata().Version` in [`ui/dashboard.go`](ui/dashboard.go:70).
- **Android Native Library Execution Refactoring**: Refactored binary deployment to use `libv2ray.so` inside `jniLibs/arm64-v8a/` with `android:extractNativeLibs="true"` in [`AndroidManifest.xml`](android/app/src/main/AndroidManifest.xml:1). Runtime process manager directly locates `libv2ray.so` via `os.Executable()` (`nativeLibraryDir`) with glob pattern fallbacks, avoiding any random hash directory mismatches or permission errors.
- **Default Configuration**: Embedded [`data/goout.json`](data/goout.json:1) via `go:embed` in [`data/config.go`](data/config.go:1).
- **Shizuku Removal Verification**: Completely removed Shizuku, `su`, and external probing logic.
- **Unit Tests**: All unit tests (`core/process_test.go`, `data/config_test.go`) passed successfully.
- **CI/CD Versioning (`.github/workflows/release.yml`)**: Enhanced GitHub Actions workflow to support manual version input, git tag detection, and dynamic monotonic `appBuild` generation.
