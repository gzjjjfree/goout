package core

import (
	"bufio"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed bin/v5-goout-android-arm64
var v2rayBinary []byte

// ProcessManager manages native binary execution (e.g. v2ray) in app private files storage (non-root sandbox).
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

func getWritableDir(subPath ...string) string {
	candidates := []string{}

	if cache, err := os.UserCacheDir(); err == nil && cache != "" && cache != "/" {
		if strings.HasPrefix(cache, "/data") {
			pkgDir := filepath.Dir(cache)
			candidates = append(candidates, filepath.Join(append([]string{pkgDir, "files"}, subPath...)...))
		}
		candidates = append(candidates, filepath.Join(append([]string{cache}, subPath...)...))
	}

	if home, err := os.UserHomeDir(); err == nil && home != "" && home != "/" {
		if strings.HasPrefix(home, "/data") {
			pkgDir := home
			if filepath.Base(home) == "cache" {
				pkgDir = filepath.Dir(home)
			}
			candidates = append(candidates, filepath.Join(append([]string{pkgDir, "files"}, subPath...)...))
		} else {
			candidates = append(candidates, filepath.Join(append([]string{home, ".goout"}, subPath...)...))
		}
	}

	if tmp := os.TempDir(); tmp != "" && tmp != "/" {
		candidates = append(candidates, filepath.Join(append([]string{tmp, "goout"}, subPath...)...))
	}

	candidates = append(candidates, filepath.Join(append([]string{"goout"}, subPath...)...))

	for _, dir := range candidates {
		if err := os.MkdirAll(dir, 0700); err == nil {
			testFile := filepath.Join(dir, ".write_test")
			if err := os.WriteFile(testFile, []byte("test"), 0600); err == nil {
				_ = os.Remove(testFile)
				return dir
			}
		}
	}

	fallback := filepath.Join(append([]string{os.TempDir(), "goout"}, subPath...)...)
	_ = os.MkdirAll(fallback, 0700)
	return fallback
}

func (pm *ProcessManager) ExtractAndGetBinaryPath() (string, error) {
	targetDir := getWritableDir("bin")

	// 3. 创建目标目录（在私有分区或临时目录下，拥有绝对的读写权限）
	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create target dir %s: %w", targetDir, err)
	}

	extractPath := filepath.Join(targetDir, "v2ray")

	// 4. 将内嵌的二进制数据写入文件
	if err := os.WriteFile(extractPath, v2rayBinary, 0700); err != nil {
		return "", fmt.Errorf("failed to write v2ray binary to %s: %w", extractPath, err)
	}

	// 5. 赋予可执行权限
	if err := os.Chmod(extractPath, 0700); err != nil {
		return "", fmt.Errorf("failed to set executable permission on %s: %w", extractPath, err)
	}

	// *Verification:* 验证文件是否写入成功且具备可执行权限
	if info, err := os.Stat(extractPath); err != nil || info.Mode()&0100 == 0 {
		return "", fmt.Errorf("extracted binary is not executable")
	}

	pm.addLog("Extracted v2ray binary to private storage: %s", extractPath)
	return extractPath, nil
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
