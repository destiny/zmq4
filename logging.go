// Copyright 2018 The go-zeromq Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package zmq4

import (
	"io"
	"log"
	"os"
)

// LogLevel represents different logging levels
type LogLevel int

const (
	LogLevelError LogLevel = iota
	LogLevelWarn
	LogLevelInfo
	LogLevelDebug
	LogLevelTrace
)

// String returns the string representation of the log level
func (l LogLevel) String() string {
	switch l {
	case LogLevelError:
		return "ERROR"
	case LogLevelWarn:
		return "WARN"
	case LogLevelInfo:
		return "INFO"
	case LogLevelDebug:
		return "DEBUG"
	case LogLevelTrace:
		return "TRACE"
	default:
		return "UNKNOWN"
	}
}

// Logger provides structured logging with levels
type Logger struct {
	logger *log.Logger
	level  LogLevel
}

// NewLogger creates a new Logger with the specified level
func NewLogger(level LogLevel) *Logger {
	return &Logger{
		logger: log.New(os.Stderr, "zmq4: ", log.LstdFlags),
		level:  level,
	}
}

// NewLoggerWithWriter creates a new Logger with custom writer and level
func NewLoggerWithWriter(w io.Writer, level LogLevel) *Logger {
	return &Logger{
		logger: log.New(w, "zmq4: ", log.LstdFlags),
		level:  level,
	}
}

// SetLevel sets the minimum logging level
func (l *Logger) SetLevel(level LogLevel) {
	l.level = level
}

// GetLevel returns the current logging level
func (l *Logger) GetLevel() LogLevel {
	return l.level
}

// IsEnabled checks if a log level is enabled
func (l *Logger) IsEnabled(level LogLevel) bool {
	return level <= l.level
}

// Error logs at error level (always shown unless disabled entirely)
func (l *Logger) Error(format string, args ...interface{}) {
	if l.IsEnabled(LogLevelError) {
		l.logger.Printf("[ERROR] "+format, args...)
	}
}

// Warn logs at warning level
func (l *Logger) Warn(format string, args ...interface{}) {
	if l.IsEnabled(LogLevelWarn) {
		l.logger.Printf("[WARN] "+format, args...)
	}
}

// Info logs at info level
func (l *Logger) Info(format string, args ...interface{}) {
	if l.IsEnabled(LogLevelInfo) {
		l.logger.Printf("[INFO] "+format, args...)
	}
}

// Debug logs at debug level
func (l *Logger) Debug(format string, args ...interface{}) {
	if l.IsEnabled(LogLevelDebug) {
		l.logger.Printf("[DEBUG] "+format, args...)
	}
}

// Trace logs at trace level (most verbose)
func (l *Logger) Trace(format string, args ...interface{}) {
	if l.IsEnabled(LogLevelTrace) {
		l.logger.Printf("[TRACE] "+format, args...)
	}
}

// Default loggers for different levels
var (
	// DevNull logger that discards all output
	DevNullLogger = NewLoggerWithWriter(io.Discard, LogLevelError)
	
	// Default logger at info level for backward compatibility
	DefaultLogger = NewLogger(LogLevelInfo)
	
	// Error-only logger for production use
	ErrorLogger = NewLogger(LogLevelError)
	
	// Debug logger for development
	DebugLogger = NewLogger(LogLevelDebug)
	
	// Trace logger for detailed debugging
	TraceLogger = NewLogger(LogLevelTrace)
)