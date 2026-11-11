# Project Debug Rules (Non-Obvious Only)

- **Process Output Logs (`core/process.go`)**: If subprocesses (v2ray) fail silently, check `ProcessManager.GetLogs()` or the thread-safe ring buffer for captured stdout/stderr streams.
- **Android Logcat Filtering**: Filter logcat by app ID (`com.gzjjjfree.goout`) and tag to diagnose Shizuku permission issues or JNI crashes.
- **VPN Service Lifecycle (`service/vpn.go`)**: Connection failures or routing drops often trace back to `BIND_VPN_SERVICE` revocation (`onRevoke()`) or misconfigured TUN descriptors.
