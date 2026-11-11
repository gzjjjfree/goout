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
	"sync"
)

//go:embed bin/v5-goout-android-arm64
var v2rayBinary []byte

// ProcessManager manages native binary execution (e.g. v2ray) via Shizuku / non-root process builder.
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
	targetDir := "/data/local/tmp"
	extractPath := filepath.Join(targetDir, "v2ray")

	// 1. Try direct write
	if err := os.MkdirAll(targetDir, 0755); err == nil {
		if err := os.WriteFile(extractPath, v2rayBinary, 0755); err == nil {
			return extractPath, nil
		}
	}

	// 2. Try writing via shell (Shizuku / su / shell service)
	tmpFile, err := os.CreateTemp("", "v2ray-temp-*")
	if err == nil {
		tmpPath := tmpFile.Name()
		_, _ = tmpFile.Write(v2rayBinary)
		_ = tmpFile.Close()
		defer os.Remove(tmpPath)

		shellCmds := []string{
			fmt.Sprintf("mkdir -p %s && cp %s %s && chmod 755 %s", targetDir, tmpPath, extractPath, extractPath),
			fmt.Sprintf("su -c 'mkdir -p %s && cp %s %s && chmod 755 %s'", targetDir, tmpPath, extractPath, extractPath),
		}
		for _, sc := range shellCmds {
			if err := exec.Command("sh", "-c", sc).Run(); err == nil {
				if _, err := os.Stat(extractPath); err == nil {
					pm.addLog("Successfully extracted v2ray to %s via shell/Shizuku", extractPath)
					return extractPath, nil
				}
			}
		}
	}

	// 3. Fallback to user cache dir or temp dir when /data/local/tmp is not writable
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		cacheDir = os.TempDir()
	}
	fallbackDir := filepath.Join(cacheDir, "goout")
	if err := os.MkdirAll(fallbackDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create fallback dir %s: %w", fallbackDir, err)
	}
	fallbackPath := filepath.Join(fallbackDir, "v2ray")
	if err := os.WriteFile(fallbackPath, v2rayBinary, 0755); err != nil {
		return "", fmt.Errorf("failed to write embedded v2ray binary to fallback %s: %w", fallbackPath, err)
	}
	pm.addLog("Extracted v2ray binary to fallback path: %s", fallbackPath)
	return fallbackPath, nil
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

	// On Android, executing native binaries in /data/local/tmp/ or temp dir via Shizuku 
	// can be done directly or wrapped through shizuku shell service / sh -c.
	pm.addLog("Starting embedded v2ray process: %s with config: %s", binPath, configPath)

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
		// Fallback to shell / Shizuku execution if direct execution fails
		pm.addLog("Direct execution failed (%v), trying via shell/Shizuku...", err)
		cmd = exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("%s -config %s", binPath, configPath))
		pm.cmd = cmd

		stdout, err = cmd.StdoutPipe()
		if err != nil {
			cancel()
			return fmt.Errorf("failed to create stdout pipe via shell: %w", err)
		}
		stderr, err = cmd.StderrPipe()
		if err != nil {
			cancel()
			return fmt.Errorf("failed to create stderr pipe via shell: %w", err)
		}

		if err := cmd.Start(); err != nil {
			cancel()
			return fmt.Errorf("failed to start process via shell/Shizuku: %w", err)
		}
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
