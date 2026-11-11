# Project Architecture Rules (Non-Obvious Only)

- **Non-Root Execution Model**: Shizuku IPC and `/data/local/tmp/` binary staging allow system utility operations without requiring root access.
- **State Separation**: [`core/process.go`](core/process.go:1) maintains thread-safe execution state with `sync.Mutex`, decoupled from UI state in [`ui/dashboard.go`](ui/dashboard.go:1).
- **Cross-Platform Bridge**: `tun2socks` JNI bindings bridge Android system `VpnService` TUN interfaces with Go networking stacks.
