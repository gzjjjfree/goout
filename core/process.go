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
	// 定义所有可能的路径通配符模式（兼容 Android 各种版本的随机哈希目录与架构子目录）
	patterns := []string{
		// 1. 动态安装路径模糊匹配 (/data/app/~~.../lib/arm64/libv2ray.so)
		"/data/app/*/com.gzjjjfree.goout*/lib/*/libv2ray.so",
		"/data/app/*/com.gzjjjfree.goout*/lib/libv2ray.so",

		// 2. 经典沙盒私有路径匹配
		"/data/user/0/com.gzjjjfree.goout/lib/*/libv2ray.so",
		"/data/user/0/com.gzjjjfree.goout/lib/libv2ray.so",
		"/data/data/com.gzjjjfree.goout/lib/*/libv2ray.so",
		"/data/data/com.gzjjjfree.goout/lib/libv2ray.so",
	}

	// 如果通过 UserHomeDir 能获取到当前用户的私有目录，也加入通配符
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		pkgDir := home
		base := filepath.Base(home)
		if base == "cache" || base == "files" || base == "code_cache" {
			pkgDir = filepath.Dir(home)
		}
		patterns = append(patterns,
			filepath.Join(pkgDir, "lib", "*", "libv2ray.so"),
			filepath.Join(pkgDir, "lib", "libv2ray.so"),
		)
	}

	// 遍历所有模式进行 Glob 匹配
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, path := range matches {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				pm.addLog("Found native library via glob at: %s", path)
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("libv2ray.so not found via glob. Check if jniLibs is packaged correctly")
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
