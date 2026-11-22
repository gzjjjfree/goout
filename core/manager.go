package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/gzjjjfree/goout/data"
	v2core "github.com/gzjjjfree/v5-result"
	v2log "github.com/gzjjjfree/v5-result/common/log"
	_ "github.com/gzjjjfree/v5-result/main/distro/all"
)

var (
	earlyLogs     []string
	earlyMu       sync.Mutex
	globalLogFile *os.File
	logFileMu     sync.Mutex
)

func init() {
	setupLogCapture()
}

type logWriter struct {
	file *os.File
	mu   sync.Mutex
}

func (w *logWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.file != nil {
		_, _ = w.file.Write(p)
	}

	line := string(p)
	trimmed := strings.TrimSpace(line)
	if trimmed != "" {
		earlyMu.Lock()
		if globalManager != nil {
			globalManager.addLog("%s", trimmed)
		} else {
			earlyLogs = append(earlyLogs, trimmed)
		}
		earlyMu.Unlock()
	}

	return len(p), nil
}

func setupLogCapture() {
	logFile, err := os.OpenFile("goout.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	var writer io.Writer
	if err == nil {
		logFileMu.Lock()
		globalLogFile = logFile
		logFileMu.Unlock()
		writer = &logWriter{file: logFile}
	} else {
		writer = &logWriter{file: nil}
	}
	log.SetOutput(writer)
}

func CloseLog() {
	logFileMu.Lock()
	defer logFileMu.Unlock()
	if globalLogFile != nil {
		globalLogFile.Close()
		globalLogFile = nil
	}
}

// V2rayManager 管理 V2Ray 核心在内存中的生命周期（适配 Android 10+ W^X 安全策略）
type V2rayManager struct {
	mu         sync.Mutex
	checkMu    sync.Mutex // 串行化配置检查，防止并发创建临时核心实例
	instance   *v2core.Instance
	isRunning  bool
	logHandler *uiLogHandler

	closing   bool
	closingCh chan struct{}

	ctx     context.Context
	cancel  context.CancelFunc
	maxLogs int
	logs    []string
	logMu   sync.Mutex
	onLog   func(string)
}

type uiLogHandler struct {
	onLog func(string)
}

func (h *uiLogHandler) Handle(msg v2log.Message) {
	if h.onLog != nil {
		h.onLog(msg.String())
	}
}

var (
	globalManager *V2rayManager
	managerOnce   sync.Once
)

// GetManager 获取全局 V2rayManager 单例
func GetManager(onLog func(string)) *V2rayManager {
	managerOnce.Do(func() {
		globalManager = &V2rayManager{
			maxLogs: 500,
			onLog:   onLog,
		}
		globalManager.initLogHandler()
	})
	if onLog != nil {
		globalManager.mu.Lock()
		globalManager.onLog = onLog
		globalManager.mu.Unlock()
	}

	earlyMu.Lock()
	if len(earlyLogs) > 0 {
		for _, l := range earlyLogs {
			globalManager.addLog("%s", l)
		}
		earlyLogs = nil
	}
	earlyMu.Unlock()

	return globalManager
}

func (m *V2rayManager) emitLog(msg string) {
	m.mu.Lock()
	fn := m.onLog
	m.mu.Unlock()
	if fn != nil {
		fn(msg)
	}
}

func (m *V2rayManager) addLog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	m.logMu.Lock()
	m.logs = append(m.logs, msg)
	if len(m.logs) > m.maxLogs {
		m.logs = m.logs[1:]
	}
	m.logMu.Unlock()

	m.emitLog(msg)
}

func (m *V2rayManager) initLogHandler() {
	m.logHandler = &uiLogHandler{
		onLog: func(msg string) {
			m.logMu.Lock()
			m.logs = append(m.logs, msg)
			if len(m.logs) > m.maxLogs {
				m.logs = m.logs[1:]
			}
			m.logMu.Unlock()

			m.emitLog(msg)
		},
	}
	v2log.RegisterHandler(m.logHandler)
}

// GetLogs 获取内存中的日志缓存
func (m *V2rayManager) GetLogs() []string {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	copied := make([]string, len(m.logs))
	copy(copied, m.logs)
	return copied
}

// ClearLogs 清空内存中的日志缓存
func (m *V2rayManager) ClearLogs() {
	m.logMu.Lock()
	defer m.logMu.Unlock()
	m.logs = nil
}

// Start 在内存中启动 V2Ray 实例（带 Panic Recovery 与并发保护）
func (m *V2rayManager) Start(configJSON []byte) (err error) {
	m.emitLog("Starting V2Ray core instance...")

	m.mu.Lock()
	ch := m.closingCh
	closing := m.closing
	m.mu.Unlock()

	if closing {
		m.emitLog(" V2Ray core closing...")
		select {
		case <-ch:
		case <-time.After(5 * time.Second):
			m.emitLog("Previous instance close still pending after 5s; starting anyway")
		}
	}

	m.mu.Lock()
	if m.isRunning && m.instance != nil {
		m.mu.Unlock()
		return fmt.Errorf("v2ray core instance is already running")
	}
	m.mu.Unlock()

	m.emitLog(" V2Ray core data.EnsureGeositeDat()...")
	data.EnsureGeositeDat()
	m.emitLog(" V2Ray core data.EnsureResultJSON()...")
	data.EnsureResultJSON()

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.ctx = ctx
	m.cancel = cancel
	m.mu.Unlock()

	m.emitLog("Starting V2Ray core in-memory instance...")

	effectiveConfig := ensureV2rayLogEnabled(configJSON)

	var instance *v2core.Instance
	var startErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[V2rayManager] Start panic recovered: %v\n%s", r, debug.Stack())
				if startErr == nil {
					startErr = fmt.Errorf("v2ray core panicked: %v", r)
				}
			}
		}()

		instance, startErr = startInstanceWithTimeout(effectiveConfig, 20*time.Second)
	}()

	if startErr != nil {
		m.mu.Lock()
		if m.cancel != nil {
			m.cancel()
		}
		m.isRunning = false
		m.instance = nil
		m.mu.Unlock()
		return fmt.Errorf("failed to start v2ray in-memory instance: %w", startErr)
	}

	m.mu.Lock()
	m.instance = instance
	m.isRunning = true
	m.mu.Unlock()

	m.emitLog("V2Ray core in-memory instance started successfully.")

	if m.logHandler != nil {
		v2log.RegisterHandler(m.logHandler)
	}

	return nil
}

// Stop 关闭 V2Ray 内存实例
func (m *V2rayManager) Stop() error {
	m.mu.Lock()
	if !m.isRunning {
		m.mu.Unlock()
		return nil
	}

	if m.cancel != nil {
		m.cancel()
	}

	inst := m.instance
	m.closing = true
	m.closingCh = make(chan struct{})
	m.instance = nil
	m.isRunning = false
	m.mu.Unlock()

	m.emitLog("Stopping V2Ray core in-memory instance...")

	if inst == nil {
		close(m.closingCh)
		m.emitLog("V2Ray core in-memory instance stopped.")
		return nil
	}

	go func() {
		defer close(m.closingCh)
		defer func() {
			if r := recover(); r != nil {
				m.emitLog(fmt.Sprintf("V2Ray instance close panic recovered: %v", r))
			}
		}()

		done := make(chan error, 1)
		go func() {
			defer func() {
				if r := recover(); r != nil {
					done <- fmt.Errorf("close panic: %v", r)
				}
			}()
			done <- inst.Close()
		}()
		select {
		case err := <-done:
			if err != nil {
				m.emitLog(fmt.Sprintf("V2Ray instance close warning: %v", err))
			}
		case <-time.After(10 * time.Second):
			m.emitLog("V2Ray instance close timed out after 10s; proceeding with shutdown")
		}
	}()

	return nil
}

// IsRunning 检查 V2Ray 内存实例是否在运行
func (m *V2rayManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isRunning
}

// CheckConfig 使用内存中的 V2Ray 核心实例验证配置是否合法（带 Panic Recovery、端口隔离与日志恢复）
func (m *V2rayManager) CheckConfig(configJSON []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[V2rayManager] CheckConfig panic recovered: %v\n%s", r, debug.Stack())
			err = fmt.Errorf("v2ray config check panicked: %v", r)
		}
	}()

	m.checkMu.Lock()
	defer m.checkMu.Unlock()

	log.Println("Checking V2Ray configuration in memory...")
	data.EnsureGeositeDat()
	data.EnsureResultJSON()
	return m.checkConfigInMemory(configJSON)
}

func (m *V2rayManager) checkConfigInMemory(configJSON []byte) (err error) {
	// 1. 先做基础 JSON 结构与必填字段预检，提前拦截明显错误
	var raw map[string]interface{}
	if jsonErr := json.Unmarshal(configJSON, &raw); jsonErr != nil {
		return fmt.Errorf("invalid JSON syntax: %w", jsonErr)
	}

	// 2. 如果当前主实例正在运行，移除测试配置中的 inbounds，避免绑定相同端口（如 10808）导致端口占用失败
	testConfig := configJSON
	if m.IsRunning() {
		testConfig = stripInboundsForCheck(configJSON)
	}
	effectiveConfig := ensureV2rayLogEnabled(testConfig)

	// 3. 确保测试结束后重新挂载全局 UI 日志处理器，避免被临时实例覆盖
	defer func() {
		if m.logHandler != nil {
			v2log.RegisterHandler(m.logHandler)
		}
	}()

	// 4. 使用带 recover 与超时的启动方式，防止非法配置导致核心 panic 或卡死
	instance, startErr := startInstanceWithTimeout(effectiveConfig, 10*time.Second)
	if startErr != nil {
		return fmt.Errorf("v2ray config check failed: %w", startErr)
	}
	if instance != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[V2rayManager] CheckConfig instance close panic: %v", r)
				}
			}()
			_ = instance.Close()
		}()
	}
	return nil
}

// stripInboundsForCheck 在 V2Ray 已运行时去除测试配置中的端口监听，仅验证核心/出站/路由配置
func stripInboundsForCheck(configJSON []byte) []byte {
	var raw map[string]interface{}
	if err := json.Unmarshal(configJSON, &raw); err != nil {
		return configJSON
	}
	raw["inbounds"] = []interface{}{}
	if modified, err := json.Marshal(raw); err == nil {
		return modified
	}
	return configJSON
}

func ensureV2rayLogEnabled(configJSON []byte) []byte {
	var raw map[string]interface{}
	if err := json.Unmarshal(configJSON, &raw); err != nil {
		return configJSON
	}
	logObj, ok := raw["log"].(map[string]interface{})
	if !ok {
		logObj = make(map[string]interface{})
		raw["log"] = logObj
	}
	level, _ := logObj["loglevel"].(string)
	if level == "" || level == "none" {
		logObj["loglevel"] = "warning"
	}
	if modified, err := json.Marshal(raw); err == nil {
		return modified
	}
	return configJSON
}

// startInstanceWithTimeout 调用 v2ray-core 的 StartInstance，并加超时与 panic 保护。
func startInstanceWithTimeout(config []byte, timeout time.Duration) (*v2core.Instance, error) {
	done := make(chan struct {
		inst *v2core.Instance
		err  error
	}, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[V2rayManager] StartInstance goroutine panic: %v\n%s", r, debug.Stack())
				done <- struct {
					inst *v2core.Instance
					err  error
				}{nil, fmt.Errorf("v2ray core panicked: %v", r)}
			}
		}()
		inst, err := v2core.StartInstance("json", config)
		done <- struct {
			inst *v2core.Instance
			err  error
		}{inst, err}
	}()

	select {
	case res := <-done:
		return res.inst, res.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("v2ray core StartInstance timed out after %s", timeout)
	}
}
