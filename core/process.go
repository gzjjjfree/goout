package core

import (
	"bufio"
	"bytes"
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
	var candidatePaths []string

	// 1. 核心方法：通过当前正在运行的可执行文件/动态库路径推导同级目录
	if execPath, err := os.Executable(); err == nil && execPath != "" {
		execDir := filepath.Dir(execPath)
		candidatePaths = append(candidatePaths, filepath.Join(execDir, "libv2ray.so"))
		candidatePaths = append(candidatePaths, filepath.Join(filepath.Dir(execDir), "libv2ray.so"))
		candidatePaths = append(candidatePaths, filepath.Join(filepath.Dir(execDir), "arm64", "libv2ray.so"))
	}

	// 2. 备用方法：通过解析 /proc/self/maps 内存映射查找已加载的库路径
	if mapsData, err := os.ReadFile("/proc/self/maps"); err == nil {
		scanner := bufio.NewScanner(bytes.NewReader(mapsData))
		for scanner.Scan() {
			line := scanner.Text()
			if strings.Contains(line, "libv2ray.so") || strings.Contains(line, "libmain.so") || strings.Contains(line, "libapp.so") {
				fields := strings.Fields(line)
				if len(fields) > 0 {
					path := fields[len(fields)-1]
					if strings.HasPrefix(path, "/") {
						dir := filepath.Dir(path)
						candidatePaths = append(candidatePaths, filepath.Join(dir, "libv2ray.so"))
					}
				}
			}
		}
	}

	// 3. 补充常见的 Android 绝对沙盒路径兜底
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		pkgDir := home
		base := filepath.Base(home)
		if base == "cache" || base == "files" || base == "code_cache" {
			pkgDir = filepath.Dir(home)
		}
		candidatePaths = append(candidatePaths,
			filepath.Join(pkgDir, "lib", "libv2ray.so"),
			filepath.Join(pkgDir, "lib", "arm64", "libv2ray.so"),
			filepath.Join(pkgDir, "lib", "arm64-v8a", "libv2ray.so"),
		)
	}

	candidatePaths = append(candidatePaths,
		"/data/user/0/com.gzjjjfree.goout/lib/libv2ray.so",
		"/data/data/com.gzjjjfree.goout/lib/libv2ray.so",
	)

	// 遍历候选路径并进行精确校验
	seen := make(map[string]bool)
	for _, path := range candidatePaths {
		if path == "" || seen[path] {
			continue
		}
		seen[path] = true
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			pm.addLog("Found native library dynamically at: %s", path)
			return path, nil
		}
	}

	return "", fmt.Errorf("native library libv2ray.so not found. Ensure it is placed in app/src/main/jniLibs/arm64-v8a/ and android:extractNativeLibs='true'")
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
