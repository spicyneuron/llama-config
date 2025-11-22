package logger

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
)

// Level represents log verbosity.
type Level int

const (
	LevelInfo Level = iota
	LevelDebug
)

var (
	currentLevel Level = LevelInfo
	stdLogger          = log.New(os.Stdout, "", log.LstdFlags)
	mu           sync.RWMutex
)

// SetLevel sets the global log level.
func SetLevel(level Level) {
	mu.Lock()
	currentLevel = level
	mu.Unlock()
}

// EnableDebug toggles debug-level logging.
func EnableDebug(enabled bool) {
	if enabled {
		SetLevel(LevelDebug)
		return
	}
	SetLevel(LevelInfo)
}

// IsDebug reports whether debug logs are enabled.
func IsDebug() bool {
	mu.RLock()
	defer mu.RUnlock()
	return currentLevel >= LevelDebug
}

// Info logs informational messages.
func Info(msg string, kv ...any) {
	logWithLevel("INFO", msg, kv...)
}

// Error logs error messages.
func Error(msg string, kv ...any) {
	logWithLevel("ERROR", msg, kv...)
}

// Debug logs debug messages when enabled.
func Debug(msg string, kv ...any) {
	if !IsDebug() {
		return
	}
	logWithLevel("DEBUG", msg, kv...)
}

// Fatal logs a fatal message then exits.
func Fatal(msg string, kv ...any) {
	logWithLevel("FATAL", msg, kv...)
	os.Exit(1)
}

func logWithLevel(level string, msg string, kv ...any) {
	stdLogger.Printf("[%s] %s%s", level, msg, formatFields(kv...))
}

func formatFields(kv ...any) string {
	if len(kv) == 0 {
		return ""
	}

	builder := strings.Builder{}
	builder.WriteString(" |")

	for i := 0; i < len(kv); i += 2 {
		if i+1 >= len(kv) {
			builder.WriteString(fmt.Sprintf(" %v", kv[i]))
			break
		}

		key := fmt.Sprintf("%v", kv[i])
		val := kv[i+1]
		builder.WriteString(fmt.Sprintf(" %s=%v", key, val))
	}

	return builder.String()
}
