# Project Coding Rules (Non-Obvious Only)

- **Process Execution (`core/process.go`)**: Always use `context.WithCancel` and pipe stdout/stderr through thread-safe ring buffers (max 500 lines) with callback hooks to prevent hanging or memory leaks.
- **UI Thread Safety (`ui/`)**: Never update Fyne UI components directly from background goroutines; always wrap UI mutations in `fyne.Do`.
- **Shizuku / Non-root binaries**: Binary execution targets `/data/local/tmp/` via Shizuku shell service or `ProcessBuilder` wrappers rather than direct root execution.
- **JNI & Tun2socks (`jni/tun2socks.go`)**: CGO bindings require precise struct alignment and memory cleanup across the Go/C boundary.
