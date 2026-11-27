package core

import (
	"bytes"
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

const (
	logLevelDebug = iota
	logLevelInfo
	logLevelWarning
	logLevelError
	logLevelNone
)

var (
	earlyLogs     []string
	earlyMu       sync.Mutex
	globalLogFile *os.File
	logFileMu     sync.Mutex

	logLevelMu      sync.RWMutex
	currentLogLevel = logLevelInfo
)

// DefaultMinLogLevel 是日志级别的最低下限。即使配置里写了 "none"/"off"，
// UI 仍至少能看到 warning 级别的可观测性日志，避免日志栏完全静默。
const DefaultMinLogLevel = logLevelDebug

func parseLogLevel(raw string) int {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return logLevelDebug
	case "info":
		return logLevelInfo
	case "warning", "warn":
		return logLevelWarning
	case "error", "err":
		return logLevelError
	case "none", "off", "silent":
		return logLevelNone
	default:
		// 空串 / 非法值一律按 info 级别处理。
		return logLevelInfo
	}
}

// clampLogLevel 将任意级别钳位到 [DefaultMinLogLevel, logLevelNone] 区间内。
func clampLogLevel(lvl int) int {
	if lvl < DefaultMinLogLevel {
		return DefaultMinLogLevel
	}
	if lvl > logLevelNone {
		return logLevelNone
	}
	return lvl
}

// SetLogLevel 设置运行时日志级别。空串或非法值会被钳位到 DefaultMinLogLevel。
func SetLogLevel(levelStr string) {
	lvl := clampLogLevel(parseLogLevel(levelStr))
	logLevelMu.Lock()
	currentLogLevel = lvl
	logLevelMu.Unlock()
}

// GetLogLevel 返回当前运行时日志级别（整数）。
func GetLogLevel() int {
	logLevelMu.RLock()
	defer logLevelMu.RUnlock()
	return currentLogLevel
}

// LogLevelName 将整数级别映射为可读字符串，供 UI 选择器回显。
func LogLevelName(level int) string {
	switch level {
	case logLevelDebug:
		return "debug"
	case logLevelInfo:
		return "info"
	case logLevelWarning:
		return "warning"
	case logLevelError:
		return "error"
	case logLevelNone:
		return "none"
	default:
		return "warning"
	}
}

// LogLevelOptions 返回 UI 选择器可用的全部级别字符串列表。
func LogLevelOptions() []string {
	return []string{"debug", "info", "warning", "error", "none"}
}

func detectMessageLevel(text string) int {
	lower := strings.ToLower(text)
	switch {
	case strings.Contains(text, "[Error]"), strings.Contains(text, "[error]"), strings.Contains(lower, "error"), strings.Contains(lower, "failed"):
		return logLevelError
	case strings.Contains(text, "[Warning]"), strings.Contains(text, "[Warn]"), strings.Contains(lower, "warning"), strings.Contains(lower, "warn"):
		return logLevelWarning
	case strings.Contains(text, "[Debug]"), strings.Contains(text, "[debug]"):
		return logLevelDebug
	case strings.Contains(text, "[Info]"), strings.Contains(text, "[info]"):
		return logLevelInfo
	default:
		return logLevelInfo
	}
}

func ShouldAllowLog(text string) bool {
	return true
}

func extractLogLevelFromConfig(configJSON []byte) string {
	return "none"
}

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
	if trimmed != "" && ShouldAllowLog(trimmed) {
		earlyMu.Lock()
		if globalManager != nil {
			globalManager.addLog("%s", trimmed)
		} else {
			if len(earlyLogs) >= 500 {
				earlyLogs = earlyLogs[len(earlyLogs)-499:]
			}
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
	checkMu    sync.Mutex
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
	if msg == nil {
		return
	}
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
		RegisterSocketProtect()
		globalManager = &V2rayManager{
			maxLogs: 200,
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
			globalManager.addLogUnfiltered("%s", l)
		}
		earlyLogs = nil
	}
	earlyMu.Unlock()

	return globalManager
}

func (m *V2rayManager) emitLog(msg string) {
	if !ShouldAllowLog(msg) {
		return
	}
	m.mu.Lock()
	fn := m.onLog
	m.mu.Unlock()
	if fn != nil {
		fn(msg)
	}
}

func (m *V2rayManager) emitLogUnfiltered(msg string) {
	m.mu.Lock()
	fn := m.onLog
	m.mu.Unlock()
	if fn != nil {
		fn(msg)
	}
}

// logAndEmit 统一过滤并写入内存缓存与 UI 回调
func (m *V2rayManager) logAndEmit(msg string) {
	if !ShouldAllowLog(msg) {
		return
	}
	m.logMu.Lock()
	m.appendLogLocked(msg)
	m.logMu.Unlock()
	m.emitLog(msg)
}

func (m *V2rayManager) logAndEmitUnfiltered(msg string) {
	m.logMu.Lock()
	m.appendLogLocked(msg)
	m.logMu.Unlock()
	m.emitLogUnfiltered(msg)
}

func (m *V2rayManager) appendLogLocked(msg string) {
	limit := m.maxLogs
	if limit <= 0 {
		limit = 200
	}
	m.logs = append(m.logs, msg)
	if len(m.logs) > limit {
		m.logs = m.logs[len(m.logs)-limit:]
	}
}

func (m *V2rayManager) addLog(format string, args ...any) {
	m.logAndEmit(fmt.Sprintf(format, args...))
}

func (m *V2rayManager) addLogUnfiltered(format string, args ...any) {
	m.logAndEmitUnfiltered(fmt.Sprintf(format, args...))
}

func (m *V2rayManager) initLogHandler() {
	m.logHandler = &uiLogHandler{
		onLog: m.logAndEmit,
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

func formatConfigForLog(cfgBytes []byte) string {
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, cfgBytes, "", "  "); err == nil {
		return pretty.String()
	}
	return string(cfgBytes)
}

// Start 在内存中启动 V2Ray 实例（带 Panic Recovery、并发保护与完整配置输出）
func (m *V2rayManager) Start(configJSON []byte) (err error) {
	RegisterSocketProtect()
	SetLogLevel(extractLogLevelFromConfig(configJSON))
	m.emitLog("Starting V2Ray core instance...")

	m.mu.Lock()
	ch := m.closingCh
	closing := m.closing
	m.mu.Unlock()

	if closing {
		m.emitLog("V2Ray core closing...")
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

	m.emitLog("V2Ray core data.EnsureGeositeDat()...")
	data.EnsureGeositeDat()
	m.emitLog("V2Ray core data.EnsureResultJSON()...")
	data.EnsureResultJSON()

	ctx, cancel := context.WithCancel(context.Background())
	m.mu.Lock()
	m.ctx = ctx
	m.cancel = cancel
	m.mu.Unlock()

	effectiveConfig := ensureV2rayLogEnabled(configJSON)

	// 在启动 V2Ray 核心时将完整配置输出到日志栏
	m.emitLog("=== V2Ray Config 开始 ===\n" + formatConfigForLog(effectiveConfig) + "\n=== V2Ray Config 结束 ===")
	m.emitLog("Starting V2Ray core in-memory instance...")

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

// CheckConfig 使用内存中的 V2Ray 核心实例验证配置是否合法
func (m *V2rayManager) CheckConfig(configJSON []byte) (err error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[V2rayManager] CheckConfig panic recovered: %v\n%s", r, debug.Stack())
			err = fmt.Errorf("v2ray config check panicked: %v", r)
		}
	}()

	m.checkMu.Lock()
	defer m.checkMu.Unlock()

	SetLogLevel(extractLogLevelFromConfig(configJSON))
	log.Println("Checking V2Ray configuration in memory...")
	data.EnsureGeositeDat()
	data.EnsureResultJSON()
	return m.checkConfigInMemory(configJSON)
}

func (m *V2rayManager) checkConfigInMemory(configJSON []byte) (err error) {
	var raw map[string]interface{}
	if jsonErr := json.Unmarshal(configJSON, &raw); jsonErr != nil {
		return fmt.Errorf("invalid JSON syntax: %w", jsonErr)
	}

	testConfig := configJSON
	if m.IsRunning() {
		testConfig = stripInboundsForCheck(configJSON)
	}
	effectiveConfig := ensureV2rayLogEnabled(testConfig)

	defer func() {
		if m.logHandler != nil {
			v2log.RegisterHandler(m.logHandler)
		}
	}()

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
	logObj["loglevel"] = "none"

	if modified, err := json.Marshal(raw); err == nil {
		return modified
	}
	return configJSON
}

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
