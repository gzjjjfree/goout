package core

import (
	"bufio"
	"log"
	"os"
	"strings"
	"sync"
)

var iostreamMu sync.Mutex

// InstallStdioCapture 重定向进程的标准输出/标准错误，逐行注入到共享日志管道。
//
// v2ray-core 在启动阶段会把诊断日志（例如 "V2Ray X started"）通过它内部独立的
// 日志写入器直接写到终端。这条路径既不走 Go 标准 log 的 log.SetOutput，也不走
// 已注册的 v2log handler，因此只会显示在终端、进不了 UI 日志栏。
// 本函数把 stdout/stderr 切到管道后喂给同样的日志管道，补齐这部分输出。
//
// 请尽早调用（必须在启动 V2Ray / tun2socks 之前，推荐放在 main 函数最前面）。
func InstallStdioCapture() {
	iostreamMu.Lock()
	defer iostreamMu.Unlock()

	redirect := func(target **os.File) {
		r, w, err := os.Pipe()
		if err != nil {
			log.Printf("[StdioCapture] failed to redirect stdout/stderr: %v", err)
			return
		}
		go drainPipe(r)
		*target = w
	}

	redirect(&os.Stdout)
	redirect(&os.Stderr)
}

func drainPipe(r *os.File) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 支持较长的单行日志
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		forwardToLog(line)
	}
	_ = r.Close()
}

func forwardToLog(line string) {
	earlyMu.Lock()
	if globalManager != nil {
		globalManager.addLog("%s", line)
	} else {
		earlyLogs = append(earlyLogs, line)
	}
	earlyMu.Unlock()
}
