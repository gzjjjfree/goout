package core

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
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
	instance   *v2core.Instance
	isRunning  bool
	logHandler *uiLogHandler
	// 通过 RegisterHandler 把 UI handler 设成核心日志的全局 handler。
	// 注意：core 在 StartInstance 内部初始化日志功能时会再次调用 RegisterHandler，
	// 把自己注册的 logger 实例设成全局，导致本 handler 被顶掉，
	// 因此需要在 StartInstance 成功后重新注册一次（见 Start 方法）。

	// closing 标记存在尚未完成的关闭流程；closingCh 在其完成后被关闭。
	// 用于防止"上一个实例尚未释放端口就立即启动新实例"而引起的端口冲突。
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
	// 启动阶段就把 UI handler 设成全局 handler，这样核心在 StartInstance 之前
	// 产生的日志也能进入 UI（随后会被 core 的注册顶掉，需见 Start 重新注册）。
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
	m.emitLog("Starting V2Ray core instance...")

	// 若上一次关闭尚未完成，先等待其结束，
	// 保证旧实例的监听端口已释放，避免启动新实例时端口冲突。
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
	// 确保 geosite.dat 资产文件及 result.json 已通过 go:embed 释放到运行目录
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

	// 核心在 StartInstance 内部初始化日志功能时调过 RegisterHandler，
	// 把全局 handler 换成了它自己的日志实例，使我们之前的注册失效。
	// 这里重新注册一次，之后所有核心日志（diagnostic + access）都会经由
	// 我们的 uiLogHandler 转发到 UI。
	v2log.RegisterHandler(m.logHandler)

	return nil
}

// Stop 关闭 V2Ray 内存实例。
//
// 实例的关闭在后台 goroutine 中进行，Stop 本身立即返回。
// 关闭完成后会通过 closingCh 通知；启动新实例时会等待其完成，
// 以保证旧实例的监听端口已彻底释放，避免端口冲突。
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
	m.mu.Unlock() // 立即释放锁，使启动流程能够进入 waiting

	m.emitLog("Stopping V2Ray core in-memory instance...")

	if inst == nil {
		close(m.closingCh)
		m.emitLog("V2Ray core in-memory instance stopped.")
		return nil
	}

	go func() {
		defer close(m.closingCh) // 无论成功/失败/panic 都确保"已完成"通知被发出
		defer func() {
			if r := recover(); r != nil {
				m.emitLog(fmt.Sprintf("V2Ray instance close panic recovered: %v", r))
			}
		}()

		// 给 Close 加超时保护，避免极端情况下卡死导致 closing 永不为真。
		done := make(chan error, 1)
		go func() {
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

// startInstanceWithTimeout 调用 v2ray-core 的 StartInstance，并加超时保护。
// 防止某个平台（如 Android 的 netpoll/epoll）让 StartInstance 永久挂起，
// 导致整个 UI 进程卡在"启动中"。
func startInstanceWithTimeout(config []byte, timeout time.Duration) (*v2core.Instance, error) {
	done := make(chan struct {
		inst *v2core.Instance
		err  error
	}, 1)

	go func() {
		func() {
			defer func() {
				if r := recover(); r != nil {
					done <- struct {
						inst *v2core.Instance
						err  error
					}{nil, fmt.Errorf("v2ray core panicked: %v", r)}
					return
				}
			}()
			inst, err := v2core.StartInstance("json", config)
			done <- struct {
				inst *v2core.Instance
				err  error
			}{inst, err}
		}()
	}()

	select {
	case res := <-done:
		return res.inst, res.err
	case <-time.After(timeout):
		return nil, fmt.Errorf("v2ray core StartInstance timed out after %s (Android 端可能是 netpoll/epoll 挂起)", timeout)
	}
}
