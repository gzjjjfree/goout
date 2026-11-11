package core

import (
	"testing"
)

func TestProcessManagerLogAndState(t *testing.T) {
	var lastLog string
	pm := NewProcessManager(func(msg string) {
		lastLog = msg
	})

	if pm.IsRunning() {
		t.Errorf("expected process manager not running initially")
	}

	pm.addLog("test log message")
	logs := pm.GetLogs()
	if len(logs) == 0 || logs[len(logs)-1] != "test log message" {
		t.Errorf("expected last log to be 'test log message', got %s", lastLog)
	}
}

