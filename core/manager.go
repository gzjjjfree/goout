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
	mu            sync.Mutex
	instance      *v2core.Instance
	isRunning     bool
	isStopping    bool
	stopChan      chan struct{}
	logHandler    *uiLogHandler
	ctx           context.Context
	cancel        context.CancelFunc
	maxLogs       int
	logs          []string
	logMu         sync.Mutex
	onLog         func(string)
	logBuffer     []string
	bufferMu      sync.Mutex
	flushStopChan chan struct{}
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
			maxLogs:       500,
			onLog:         onLog,
			flushStopChan: make(chan struct{}),
		}
		globalManager.initLogHandler()
		globalManager.startLogFlusher()
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

func (m *V2rayManager) startLogFlusher() {
	go func() {
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.flushLogs()
			case <-m.flushStopChan:
				m.flushLogs()
				return
			}
		}
	}()
}

func (m *V2rayManager) flushLogs() {
	m.bufferMu.Lock()
	if len(m.logBuffer) == 0 {
		m.bufferMu.Unlock()
		return
	}
	batch := m.logBuffer
	m.logBuffer = nil
	m.bufferMu.Unlock()

	combined := strings.Join(batch, "\n")
	m.emitLog(combined)
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

	m.bufferMu.Lock()
	m.logBuffer = append(m.logBuffer, msg)
	shouldFlush := len(m.logBuffer) >= 50
	m.bufferMu.Unlock()

	if shouldFlush {
		m.flushLogs()
	}
}

func (m *V2rayManager) initLogHandler() {
	m.logHandler = &uiLogHandler{
		onLog: func(msg string) {
			m.addLog("%s", msg)
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

// Start 在内存中启动 V2Ray 实例（带 Panic Recovery 与并发保护）
func (m *V2rayManager) Start(configJSON []byte) (err error) {
	m.mu.Lock()
	if m.isRunning && m.instance != nil {
		m.mu.Unlock()
		return fmt.Errorf("v2ray core instance is already running")
	}
	if m.isStopping {
		stopChan := m.stopChan
		m.mu.Unlock()
		if stopChan != nil {
			<-stopChan // 等待上一次实例完全关闭并释放端口资源
		}
	} else {
		m.mu.Unlock()
	}

	m.mu.Lock()
	if m.isRunning && m.instance != nil {
		m.mu.Unlock()
		return fmt.Errorf("v2ray core instance is already running")
	}

	// 确保 geosite.dat 资产文件及 result.json 已通过 go:embed 释放到运行目录
	data.EnsureGeositeDat()
	data.EnsureResultJSON()

	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel
	m.mu.Unlock()

	m.emitLog("Starting V2Ray core in-memory instance...")

	// Panic Recovery 保护：防止底层核心崩溃导致整个 Fyne UI 进程闪退
	var instance *v2core.Instance
	var startErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("v2ray core panicked: %v\n%s", r, debug.Stack())
				m.mu.Lock()
				m.isRunning = false
				m.instance = nil
				if m.cancel != nil {
					m.cancel()
				}
				m.mu.Unlock()
				m.emitLog(err.Error())
			}
		}()

		effectiveConfig := ensureV2rayLogEnabled(configJSON)
		instance, startErr = v2core.StartInstance("json", effectiveConfig)
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

	return nil
}

// Stop 优雅关闭 V2Ray 内存实例并同步释放资源（确保端口释放，防止竞态）
func (m *V2rayManager) Stop() error {
	m.mu.Lock()
	if !m.isRunning && !m.isStopping {
		m.mu.Unlock()
		return nil
	}
	if m.isStopping {
		m.mu.Unlock()
		return nil
	}

	m.isRunning = false
	m.isStopping = true
	stopChan := make(chan struct{})
	m.stopChan = stopChan

	if m.cancel != nil {
		m.cancel()
	}

	inst := m.instance
	m.instance = nil
	m.mu.Unlock() // 释放锁，允许其他操作进入

	m.emitLog("Stopping V2Ray core in-memory instance...")

	if inst != nil {
		// 异步关闭实例，避免等待网络回应或连接断开导致阻塞，同时通过 stopChan 保证完全关闭前不可再次 Start 冲突
		go func() {
			defer close(stopChan)
			defer func() {
				if r := recover(); r != nil {
					m.emitLog(fmt.Sprintf("V2Ray instance close panic recovered: %v", r))
				}
			}()
			err := inst.Close()
			if err != nil {
				m.emitLog(fmt.Sprintf("V2Ray instance close warning: %v", err))
			}
			m.mu.Lock()
			m.isStopping = false
			m.mu.Unlock()
			m.emitLog("V2Ray core in-memory instance stopped.")
		}()
	} else {
		close(stopChan)
		m.mu.Lock()
		m.isStopping = false
		m.mu.Unlock()
		m.emitLog("V2Ray core in-memory instance stopped.")
	}

	return nil
}

// IsRunning 检查 V2Ray 内存实例是否在运行
func (m *V2rayManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isRunning
}

// CheckConfig 使用内存中的 V2Ray 核心实例验证配置是否合法
func (m *V2rayManager) CheckConfig(configJSON []byte) error {
	fmt.Println("Checking V2Ray configuration in memory...")
	data.EnsureGeositeDat()
	data.EnsureResultJSON()
	return m.checkConfigInMemory(configJSON)
}

func (m *V2rayManager) checkConfigInMemory(configJSON []byte) error {
	effectiveConfig := ensureV2rayLogEnabled(configJSON)
	instance, err := v2core.StartInstance("json", effectiveConfig)
	if err != nil {
		return fmt.Errorf("v2ray config check failed: %w", err)
	}
	if instance != nil {
		_ = instance.Close()
	}
	return nil
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
