package core

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	v2core "github.com/gzjjjfree/v5-result"
	v2log "github.com/gzjjjfree/v5-result/common/log"
	_ "github.com/gzjjjfree/v5-result/main/distro/all"
	"github.com/gzjjjfree/goout/data"
)

// V2rayManager 管理 V2Ray 核心在内存中的生命周期（适配 Android 10+ W^X 安全策略）
type V2rayManager struct {
	mu         sync.Mutex
	instance   *v2core.Instance
	isRunning  bool
	logHandler *uiLogHandler
	ctx        context.Context
	cancel     context.CancelFunc
	maxLogs    int
	logs       []string
	logMu      sync.Mutex
	onLog      func(string)
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
	return globalManager
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

			if m.onLog != nil {
				m.onLog(msg)
			}
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
	defer m.mu.Unlock()

	if m.isRunning && m.instance != nil {
		return fmt.Errorf("v2ray core instance is already running")
	}

	// 确保 geosite.dat 资产文件已通过 go:embed 释放到运行目录
	data.EnsureGeositeDat()

	// Panic Recovery 保护：防止底层核心崩溃导致整个 Fyne UI 进程闪退
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("v2ray core panicked: %v\n%s", r, debug.Stack())
			m.isRunning = false
			m.instance = nil
			if m.cancel != nil {
				m.cancel()
			}
			if m.onLog != nil {
				m.onLog(err.Error())
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	m.ctx = ctx
	m.cancel = cancel

	if m.onLog != nil {
		m.onLog("Starting V2Ray core in-memory instance...")
	}

	// 在内存中直接通过 StartInstance 加载并启动配置（格式为 json）
	instance, startErr := v2core.StartInstance("json", configJSON)
	if startErr != nil {
		cancel()
		return fmt.Errorf("failed to start v2ray in-memory instance: %w", startErr)
	}

	m.instance = instance
	m.isRunning = true

	if m.onLog != nil {
		m.onLog("V2Ray core in-memory instance started successfully.")
	}

	return nil
}

// Stop 优雅关闭 V2Ray 内存实例并释放资源
func (m *V2rayManager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.isRunning {
		return nil
	}

	if m.onLog != nil {
		m.onLog("Stopping V2Ray core in-memory instance...")
	}

	var closeErr error
	if m.instance != nil {
		closeErr = m.instance.Close()
	}

	if m.cancel != nil {
		m.cancel()
	}

	m.instance = nil
	m.isRunning = false

	if m.onLog != nil {
		m.onLog("V2Ray core in-memory instance stopped.")
	}

	return closeErr
}

// IsRunning 检查 V2Ray 内存实例是否在运行
func (m *V2rayManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isRunning
}
