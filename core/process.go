package core

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// 1. 通过 os.Executable() 获取当前正在运行的动态库目录
	if exe, err := os.Executable(); err == nil && exe != "" {
		dir := filepath.Dir(exe)
		pm.addLog("Current running library dir: %s", dir)

		// 检查同级目录
		target := filepath.Join(dir, "libv2ray.so")
		if info, err := os.Stat(target); err == nil && !info.IsDir() {
			pm.addLog("Found libv2ray.so directly at: %s", target)
			return target, nil
		}

		// 检查上级目录或同级子目录（应对 arm64 与 arm64-v8a 命名差异）
		parentDir := filepath.Dir(dir)
		patterns := []string{
			filepath.Join(dir, "*", "libv2ray.so"),
			filepath.Join(parentDir, "*", "libv2ray.so"),
			filepath.Join(parentDir, "libv2ray.so"),
		}
		for _, pat := range patterns {
			if matches, err := filepath.Glob(pat); err == nil {
				for _, m := range matches {
					if info, err := os.Stat(m); err == nil && !info.IsDir() {
						pm.addLog("Found libv2ray.so via pattern %s at: %s", pat, m)
						return m, nil
					}
				}
			}
		}

		// 【关键调试】如果还没找到，打印当前目录下所有文件，方便在日志中观察真实结构
		if entries, err := os.ReadDir(dir); err == nil {
			var names []string
			for _, e := range entries {
				names = append(names, e.Name())
			}
			pm.addLog("DEBUG: Files in current library dir (%s): [%s]", dir, strings.Join(names, ", "))
		}
	}

	// 2. 兜底：使用更广泛的全局通配符扫描
	fallbackPatterns := []string{
		"/data/app/*/com.gzjjjfree.goout*/lib/*/libv2ray.so",
		"/data/app/*/com.gzjjjfree.goout*/lib/libv2ray.so",
		"/data/user/0/com.gzjjjfree.goout/lib/*/libv2ray.so",
		"/data/user/0/com.gzjjjfree.goout/lib/libv2ray.so",
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

	return "", fmt.Errorf("libv2ray.so not found. Check app logs for 'DEBUG: Files in current library dir'")
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
