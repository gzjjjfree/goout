# Project Documentation Rules (Non-Obvious Only)

- **Architecture Layout**: [`main.go`](main.go:1) delegates immediately to [`ui/dashboard.go`](ui/dashboard.go:1); low-level process management is isolated in [`core/process.go`](core/process.go:1).
- **Fyne Cross-Compilation**: Packaging relies on [`FyneApp.toml`](FyneApp.toml:1) targeting Android ARM64 (`arm64-v8a`) via `fyne-cross`.
- **VPN & Networking**: [`service/vpn.go`](service/vpn.go:1) integrates with [`jni/tun2socks.go`](jni/tun2socks.go:1) for system-wide packet redirection through local proxy listeners.
