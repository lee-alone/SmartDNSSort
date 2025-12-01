package logger

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
)

// Level defines the log level
type Level int

const (
	DebugLevel Level = iota
	InfoLevel
	WarnLevel
	ErrorLevel
	FatalLevel
)

var (
	currentLevel = InfoLevel
	mu           sync.RWMutex
	logger       = log.New(os.Stderr, "", log.LstdFlags)
)

// SetLevel sets the global log level
func SetLevel(levelStr string) {
	mu.Lock()
	defer mu.Unlock()

	switch strings.ToLower(levelStr) {
	case "debug":
		currentLevel = DebugLevel
	case "info":
		currentLevel = InfoLevel
	case "warn", "warning":
		currentLevel = WarnLevel
	case "error":
		currentLevel = ErrorLevel
	case "fatal":
		currentLevel = FatalLevel
	default:
		currentLevel = InfoLevel
	}
}

// SetOutput sets the output destination for the logger
func SetOutput(w io.Writer) {
	mu.Lock()
	defer mu.Unlock()
	logger.SetOutput(w)
}

// Debug logs a message at DebugLevel
func Debug(v ...interface{}) {
	if shouldLog(DebugLevel) {
		output("DEBUG", fmt.Sprint(v...))
	}
}

// Debugf logs a formatted message at DebugLevel
func Debugf(format string, v ...interface{}) {
	if shouldLog(DebugLevel) {
		output("DEBUG", fmt.Sprintf(format, v...))
	}
}

// Info logs a message at InfoLevel
func Info(v ...interface{}) {
	if shouldLog(InfoLevel) {
		output("INFO", fmt.Sprint(v...))
	}
}

// Infof logs a formatted message at InfoLevel
func Infof(format string, v ...interface{}) {
	if shouldLog(InfoLevel) {
		output("INFO", fmt.Sprintf(format, v...))
	}
}

// Warn logs a message at WarnLevel
func Warn(v ...interface{}) {
	if shouldLog(WarnLevel) {
		output("WARN", fmt.Sprint(v...))
	}
}

// Warnf logs a formatted message at WarnLevel
func Warnf(format string, v ...interface{}) {
	if shouldLog(WarnLevel) {
		output("WARN", fmt.Sprintf(format, v...))
	}
}

// Error logs a message at ErrorLevel
func Error(v ...interface{}) {
	if shouldLog(ErrorLevel) {
		output("ERROR", fmt.Sprint(v...))
	}
}

// Errorf logs a formatted message at ErrorLevel
func Errorf(format string, v ...interface{}) {
	if shouldLog(ErrorLevel) {
		output("ERROR", fmt.Sprintf(format, v...))
	}
}

// Fatal logs a message at FatalLevel and exits
func Fatal(v ...interface{}) {
	output("FATAL", fmt.Sprint(v...))
	os.Exit(1)
}

// Fatalf logs a formatted message at FatalLevel and exits
func Fatalf(format string, v ...interface{}) {
	output("FATAL", fmt.Sprintf(format, v...))
	os.Exit(1)
}

func shouldLog(level Level) bool {
	mu.RLock()
	defer mu.RUnlock()
	return level >= currentLevel
}

func output(levelStr, msg string) {
	// Use standard log package to handle timestamp and concurrency
	logger.Output(3, fmt.Sprintf("[%s] %s", levelStr, msg))
}
