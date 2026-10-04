package logger

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitAndLogging(t *testing.T) {
	tempDir := t.TempDir()
	logFile := filepath.Join(tempDir, "test.log")

	if err := InitLogger(logFile); err != nil {
		t.Fatalf("InitLogger failed: %v", err)
	}

	Info("TEST_EVENT", "hello %s", "world")
	LogToolCall("test_tool", "arg=123", 15*time.Millisecond, nil)

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	logStr := string(data)
	if !strings.Contains(logStr, "[INFO]") || !strings.Contains(logStr, "hello world") {
		t.Errorf("Expected log content missing, got: %s", logStr)
	}

	if !strings.Contains(logStr, "[TOOL_CALL]") || !strings.Contains(logStr, "test_tool") {
		t.Errorf("Expected tool call log missing, got: %s", logStr)
	}
}
