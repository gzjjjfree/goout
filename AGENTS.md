# AGENTS.md - Project Guidelines for AI Assistants

## 1. Project Overview

This repository contains a cross-platform Android Proxy & System Utility application built primarily with **Go**, **Fyne UI**, and networking components (`v2ray` core and `tun2socks`), modeled after v2rayNG architecture.

### Key Capabilities:

- **UI Framework**: Pure Go-based cross-platform UI using **Fyne**, packaged for Android via `fyne-cross`.
- **Process Management**: Automatically extracts embedded native binaries (e.g., `v2ray`) to the application's private internal storage/cache directory at runtime, granting executable permissions and managing them as local subprocesses (completely non-root, no Shizuku required).
- **VPN & Traffic Redirection**: Integrates Android `VpnService` with a Go-compiled `tun2socks` proxy tunnel implementation to route system-wide traffic through local proxy ports (SOCKS5).
- **Configuration & Subscriptions**: Dynamic generation and management of `config.json` files within app sandboxed storage.

---

## 2. Directory Structure Conventions

- `main.go`: Fyne UI entry point & main event loop
- `FyneApp.toml`: Fyne cross-compilation & app metadata configuration
- `core/`: Process manager & binary execution (v2ray controller)
- `service/`: Android VpnService & TUN interface management
- `jni/`: Pure Go packet forwarding & proxy tunnel integration
- `data/`: Configuration storage & local state management
- `ui/`: Modularized UI views, logs, and state controllers

---

## 3. Core Technical Rules & Constraints

- **Concurrency & UI Safety**: All background tasks (process launching, stream reading) must run in separate Goroutines. UI updates must be dispatched safely (e.g., using `fyne.Do`).
- **Process Lifecycles**: Always use `context.Context` and ensure proper cleanup (killing subprocesses, closing file descriptors) when stopping services to prevent memory and handle leaks.
- **Sandboxed Execution**: Native binaries run strictly within the application's private internal storage/cache directory as local subprocesses, adhering to standard Android sandboxed execution without requiring Shizuku or root permissions.
- **VPN Integrity**: When modifying `VpnService` logic, ensure proper configuration of routing (`0.0.0.0/0`) and handle revocation (`onRevoke()`) gracefully.

---

## 4. Coding Style Guidelines (Go / Android)

- **Idiomatic Go**: Keep code modular, handle errors explicitly, and use mutexes (`sync.Mutex`) when sharing state between UI goroutines and background event loops.
- **Logging**: Redirect standard output/error from native subprocesses into thread-safe ring buffers or channels for real-time display in the Fyne UI log terminal.
- **Cross-Platform Awareness**: Code intended for Android execution must gracefully handle mobile life-cycle events and architecture constraints (e.g., `arm64-v8a`).
