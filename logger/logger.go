package logger

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

var (
	fileLogger *log.Logger
	mu         sync.Mutex
)

func InitLogger(logPath string) error {
	mu.Lock()
	defer mu.Unlock()

	dir := filepath.Dir(logPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create log directory: %w", err)
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	fileLogger = log.New(f, "", log.LstdFlags)
	log.SetOutput(f)

	return nil
}

func logMessage(level, event, format string, args ...any) {
	mu.Lock()
	defer mu.Unlock()

	if fileLogger == nil {
		return
	}

	msg := fmt.Sprintf(format, args...)
	fileLogger.Printf("[%s] [%s] %s", level, event, msg)
}

func Info(event string, format string, args ...any) {
	logMessage("INFO", event, format, args...)
}

func Warn(event string, format string, args ...any) {
	logMessage("WARN", event, format, args...)
}

func Error(event string, format string, args ...any) {
	logMessage("ERROR", event, format, args...)
}

func LogToolCall(toolName string, details string, duration time.Duration, err error) {
	if err != nil {
		Error("TOOL_CALL", "tool=%s details=%q err=%v (%v)", toolName, details, err, duration)
	} else {
		Info("TOOL_CALL", "tool=%s details=%q (%v)", toolName, details, duration)
	}
}
