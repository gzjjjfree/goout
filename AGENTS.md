# AGENTS.md - Project Guidelines for AI Assistants

## 1. Project Overview

This repository contains a cross-platform Android Proxy & System Utility application built primarily with **Go**, **Fyne UI**, **Shizuku**, and networking components (`v2ray` core and `tun2socks`).

### Key Capabilities:

- **UI Framework**: Pure Go-based cross-platform UI using **Fyne**, packaged for Android via `fyne-cross`.
- **Process Management**: Executes and manages native binaries (e.g., `v2ray`) in `/data/local/tmp/` using **Shizuku** permissions (non-root execution).
- **VPN & Traffic Redirection**: Integrates Android `VpnService` with a Go-compiled `tun2socks` JNI dynamic library (`.so`) to route system-wide traffic through local proxy ports (SOCKS5).
- **System Debloating**: Safe package inspection and management (via Shizuku/UAD databases).

---

## 2. Directory Structure Conventions

- `main.go`: Fyne UI entry point & main event loop
- `FyneApp.toml`: Fyne cross-compilation & app metadata configuration
- `core/`: Process manager & binary execution (v2ray controller)
- `service/`: Android VpnService & TUN interface management
- `jni/`: C/Go bridge for tun2socks integration (.so bindings)
- `data/`: Configuration storage & local state management
- `ui/`: Modularized UI views, logs, and state controllers

---

## 3. Core Technical Rules & Constraints

- **Concurrency & UI Safety**: All background tasks (process launching, stream reading) must run in separate Goroutines. UI updates must be dispatched safely (e.g., using `fyne.Do`).
- **Process Lifecycles**: Always use `context.Context` and ensure proper cleanup (killing subprocesses, closing file descriptors) when stopping services to prevent memory and handle leaks.
- **Shizuku Integration**: Never assume direct root access; all low-level system commands and binary executions must leverage Shizuku IPC or `ProcessBuilder` wrappers safely.
- **VPN Integrity**: When modifying `VpnService` logic, ensure proper configuration of routing (`0.0.0.0/0`) and handle revocation (`onRevoke()`) gracefully.

---

## 4. Coding Style Guidelines (Go / Android)

- **Idiomatic Go**: Keep code modular, handle errors explicitly, and use mutexes (`sync.Mutex`) when sharing state between UI goroutines and background event loops.
- **Logging**: Redirect standard output/error from native subprocesses into thread-safe ring buffers or channels for real-time display in the Fyne UI log terminal.
- **Cross-Platform Awareness**: Code intended for Android execution must gracefully handle mobile life-cycle events and architecture constraints (e.g., `arm64-v8a`).
