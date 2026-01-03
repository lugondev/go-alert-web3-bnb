package logger

import (
	"context"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/log"
)

// Level represents the log level
type Level int

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
	LevelFatal
)

// String returns the string representation of the log level
func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	case LevelFatal:
		return "FATAL"
	default:
		return "UNKNOWN"
	}
}

// ParseLevel parses a string into a Level
func ParseLevel(s string) Level {
	switch strings.ToLower(s) {
	case "debug":
		return LevelDebug
	case "info":
		return LevelInfo
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	case "fatal":
		return LevelFatal
	default:
		return LevelInfo
	}
}

// toCharmLevel converts our Level to charmbracelet/log Level
func toCharmLevel(l Level) log.Level {
	switch l {
	case LevelDebug:
		return log.DebugLevel
	case LevelInfo:
		return log.InfoLevel
	case LevelWarn:
		return log.WarnLevel
	case LevelError:
		return log.ErrorLevel
	case LevelFatal:
		return log.FatalLevel
	default:
		return log.InfoLevel
	}
}

// Field represents a key-value pair for structured logging
type Field struct {
	Key   string
	Value interface{}
}

// F creates a new Field
func F(key string, value interface{}) Field {
	return Field{Key: key, Value: value}
}

// Logger is the main logger interface
type Logger interface {
	Debug(msg string, fields ...Field)
	Info(msg string, fields ...Field)
	Warn(msg string, fields ...Field)
	Error(msg string, fields ...Field)
	Fatal(msg string, fields ...Field)
	With(fields ...Field) Logger
	WithContext(ctx context.Context) Logger
}

// Config holds logger configuration
type Config struct {
	Level      Level
	Format     string // "json" or "text"
	Output     io.Writer
	TimeFormat string
	AppName    string
}

// logger implements the Logger interface using charmbracelet/log
type logger struct {
	charm  *log.Logger
	fields []Field
}

// New creates a new logger instance
func New(cfg Config) Logger {
	output := cfg.Output
	if output == nil {
		output = os.Stdout
	}

	timeFormat := cfg.TimeFormat
	if timeFormat == "" {
		timeFormat = time.Kitchen
	}

	charm := log.NewWithOptions(output, log.Options{
		Level:           toCharmLevel(cfg.Level),
		ReportCaller:    cfg.Level <= LevelDebug,
		ReportTimestamp: true,
		TimeFormat:      timeFormat,
		Prefix:          cfg.AppName,
	})

	// Set styles based on format
	if cfg.Format == "json" {
		charm.SetFormatter(log.JSONFormatter)
	} else {
		charm.SetFormatter(log.TextFormatter)
	}

	return &logger{
		charm:  charm,
		fields: make([]Field, 0),
	}
}

// NewDefault creates a logger with default configuration
func NewDefault() Logger {
	return New(Config{
		Level:      LevelInfo,
		Format:     "text",
		Output:     os.Stdout,
		TimeFormat: time.Kitchen,
	})
}

// fieldsToKeyvals converts Field slice to key-value pairs for charmbracelet/log
func fieldsToKeyvals(fields []Field) []interface{} {
	keyvals := make([]interface{}, 0, len(fields)*2)
	for _, f := range fields {
		keyvals = append(keyvals, f.Key, f.Value)
	}
	return keyvals
}

// allFields combines base fields with provided fields
func (l *logger) allFields(fields []Field) []interface{} {
	all := make([]Field, 0, len(l.fields)+len(fields))
	all = append(all, l.fields...)
	all = append(all, fields...)
	return fieldsToKeyvals(all)
}

// Debug logs a debug message
func (l *logger) Debug(msg string, fields ...Field) {
	l.charm.Debug(msg, l.allFields(fields)...)
}

// Info logs an info message
func (l *logger) Info(msg string, fields ...Field) {
	l.charm.Info(msg, l.allFields(fields)...)
}

// Warn logs a warning message
func (l *logger) Warn(msg string, fields ...Field) {
	l.charm.Warn(msg, l.allFields(fields)...)
}

// Error logs an error message
func (l *logger) Error(msg string, fields ...Field) {
	l.charm.Error(msg, l.allFields(fields)...)
}

// Fatal logs a fatal message and exits
func (l *logger) Fatal(msg string, fields ...Field) {
	l.charm.Fatal(msg, l.allFields(fields)...)
}

// With creates a new logger with additional fields
func (l *logger) With(fields ...Field) Logger {
	newFields := make([]Field, len(l.fields)+len(fields))
	copy(newFields, l.fields)
	copy(newFields[len(l.fields):], fields)

	return &logger{
		charm:  l.charm,
		fields: newFields,
	}
}

// WithContext creates a new logger with context values
func (l *logger) WithContext(ctx context.Context) Logger {
	var fields []Field

	// Extract common context values
	if reqID := ctx.Value(contextKeyRequestID); reqID != nil {
		fields = append(fields, F("request_id", reqID))
	}
	if traceID := ctx.Value(contextKeyTraceID); traceID != nil {
		fields = append(fields, F("trace_id", traceID))
	}

	if len(fields) == 0 {
		return l
	}

	return l.With(fields...)
}

// Context keys for extracting values
type contextKey string

const (
	contextKeyRequestID contextKey = "request_id"
	contextKeyTraceID   contextKey = "trace_id"
)

// ContextWithRequestID adds a request ID to the context
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, contextKeyRequestID, requestID)
}

// ContextWithTraceID adds a trace ID to the context
func ContextWithTraceID(ctx context.Context, traceID string) context.Context {
	return context.WithValue(ctx, contextKeyTraceID, traceID)
}

// Global logger instance
var globalLogger Logger = NewDefault()

// SetGlobal sets the global logger instance
func SetGlobal(l Logger) {
	globalLogger = l
}

// Global returns the global logger instance
func Global() Logger {
	return globalLogger
}

// Package-level functions that use the global logger
func Debug(msg string, fields ...Field) { globalLogger.Debug(msg, fields...) }
func Info(msg string, fields ...Field)  { globalLogger.Info(msg, fields...) }
func Warn(msg string, fields ...Field)  { globalLogger.Warn(msg, fields...) }
func Error(msg string, fields ...Field) { globalLogger.Error(msg, fields...) }
func Fatal(msg string, fields ...Field) { globalLogger.Fatal(msg, fields...) }
