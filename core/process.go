package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// ProcessManager manages native binary execution (e.g. v2ray) strictly via jniLibs (nativeLibraryDir).
type ProcessManager struct {
	mu      sync.Mutex
	cancel  context.CancelFunc
	running bool
	cmd     *exec.Cmd
	logMu   sync.Mutex
	logs    []string
	maxLogs int
	onLog   func(string)
}

func NewProcessManager(onLog func(string)) *ProcessManager {
	return &ProcessManager{
		maxLogs: 500,
		onLog:   onLog,
	}
}

func (pm *ProcessManager) addLog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	pm.logMu.Lock()
	defer pm.logMu.Unlock()
	pm.logs = append(pm.logs, msg)
	if len(pm.logs) > pm.maxLogs {
		pm.logs = pm.logs[1:]
	}
	if pm.onLog != nil {
		pm.onLog(msg)
	}
}

func (pm *ProcessManager) GetLogs() []string {
	pm.logMu.Lock()
	defer pm.logMu.Unlock()
	copied := make([]string, len(pm.logs))
	copy(copied, pm.logs)
	return copied
}

func (pm *ProcessManager) ExtractAndGetBinaryPath() (string, error) {
	// 明确已知的应用包名
	pkgName := "com.gzjjjfree.goout"

	// 1. 直接精准罗列 Android 标准原生库目录（系统解压APK中 jniLibs 的固定位置）
	var candidatePaths []string

	// 如果能拿到 UserHomeDir（例如 /data/user/0/com.gzjjjfree.goout 或其 cache 子目录）
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		pkgDir := home
		base := filepath.Base(home)
		if base == "cache" || base == "files" || base == "code_cache" {
			pkgDir = filepath.Dir(home)
		}
		candidatePaths = append(candidatePaths,
			filepath.Join(pkgDir, "lib", "arm64-v8a", "libv2ray.so"),
			filepath.Join(pkgDir, "lib", "arm64", "libv2ray.so"),
			filepath.Join(pkgDir, "lib", "libv2ray.so"),
		)
	}

	// 绝对路径强力兜底（覆盖所有 Android 版本的标准的沙盒 UID 目录）
	candidatePaths = append(candidatePaths,
		fmt.Sprintf("/data/user/0/%s/lib/arm64-v8a/libv2ray.so", pkgName),
		fmt.Sprintf("/data/user/0/%s/lib/arm64/libv2ray.so", pkgName),
		fmt.Sprintf("/data/user/0/%s/lib/libv2ray.so", pkgName),
		fmt.Sprintf("/data/data/%s/lib/arm64-v8a/libv2ray.so", pkgName),
		fmt.Sprintf("/data/data/%s/lib/arm64/libv2ray.so", pkgName),
		fmt.Sprintf("/data/data/%s/lib/libv2ray.so", pkgName),
	)

	// 2. 依次检查硬编码的绝对路径
	for _, path := range candidatePaths {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			pm.addLog("Found native library via absolute path at: %s", path)
			return path, nil
		}
	}

	// 3. 如果绝对路径没命中，再用 Glob 兜底扫描动态安装路径（如 /data/app/~~.../）
	fallbackPatterns := []string{
		fmt.Sprintf("/data/app/*/%s*/lib/*/libv2ray.so", pkgName),
		fmt.Sprintf("/data/app/*/%s*/lib/libv2ray.so", pkgName),
	}

	for _, pattern := range fallbackPatterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				pm.addLog("Found native library via fallback glob at: %s", path)
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("libv2ray.so not found. Absolute paths checked but none matched.")
}

func (pm *ProcessManager) Start(configPath string) error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.running {
		return nil
	}

	binPath, err := pm.ExtractAndGetBinaryPath()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	pm.cancel = cancel

	pm.addLog("Starting v2ray process: %s with config: %s", binPath, configPath)

	cmd := exec.CommandContext(ctx, binPath, "-config", configPath)
	pm.cmd = cmd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		cancel()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("failed to start v2ray process: %w", err)
	}

	pm.running = true

	go pm.streamOutput(stdout)
	go pm.streamOutput(stderr)

	go func() {
		err := cmd.Wait()
		pm.mu.Lock()
		defer pm.mu.Unlock()
		pm.running = false
		if err != nil {
			pm.addLog("Process exited with error: %v", err)
		} else {
			pm.addLog("Process exited normally")
		}
	}()

	return nil
}

func (pm *ProcessManager) streamOutput(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := scanner.Text()
		pm.addLog("%s", line)
	}
}

func (pm *ProcessManager) Stop() error {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if !pm.running {
		return nil
	}

	pm.addLog("Stopping process...")
	if pm.cancel != nil {
		pm.cancel()
	}

	if pm.cmd != nil && pm.cmd.Process != nil {
		_ = pm.cmd.Process.Kill()
	}

	pm.running = false
	pm.addLog("Process stopped.")
	return nil
}

func (pm *ProcessManager) IsRunning() bool {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	return pm.running
}
