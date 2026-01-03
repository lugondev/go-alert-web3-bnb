package logger

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected Level
	}{
		{"debug", LevelDebug},
		{"DEBUG", LevelDebug},
		{"info", LevelInfo},
		{"INFO", LevelInfo},
		{"warn", LevelWarn},
		{"warning", LevelWarn},
		{"error", LevelError},
		{"fatal", LevelFatal},
		{"unknown", LevelInfo}, // default
		{"", LevelInfo},        // default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := ParseLevel(tt.input)
			if result != tt.expected {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	tests := []struct {
		level    Level
		expected string
	}{
		{LevelDebug, "DEBUG"},
		{LevelInfo, "INFO"},
		{LevelWarn, "WARN"},
		{LevelError, "ERROR"},
		{LevelFatal, "FATAL"},
		{Level(99), "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := tt.level.String()
			if result != tt.expected {
				t.Errorf("Level.String() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestLoggerJSONFormat(t *testing.T) {
	var buf bytes.Buffer

	log := New(Config{
		Level:      LevelDebug,
		Format:     "json",
		Output:     &buf,
		TimeFormat: "2006-01-02",
	})

	log.Info("test message", F("key", "value"))

	output := buf.String()

	// charmbracelet/log uses lowercase level names in JSON
	if !strings.Contains(output, `"level":"info"`) {
		t.Errorf("expected level in JSON output, got: %s", output)
	}
	if !strings.Contains(output, `"msg":"test message"`) {
		t.Errorf("expected message in JSON output, got: %s", output)
	}
	if !strings.Contains(output, `"key":"value"`) {
		t.Errorf("expected field in JSON output, got: %s", output)
	}
}

func TestLoggerTextFormat(t *testing.T) {
	var buf bytes.Buffer

	log := New(Config{
		Level:      LevelDebug,
		Format:     "text",
		Output:     &buf,
		TimeFormat: "2006-01-02",
	})

	log.Info("test message", F("key", "value"))

	output := buf.String()

	// charmbracelet/log uses INFO (uppercase) in text format
	if !strings.Contains(output, "INFO") {
		t.Errorf("expected INFO in text output, got: %s", output)
	}
	if !strings.Contains(output, "test message") {
		t.Errorf("expected message in text output, got: %s", output)
	}
	if !strings.Contains(output, "key=value") {
		t.Errorf("expected field in text output, got: %s", output)
	}
}

func TestLoggerLevelFiltering(t *testing.T) {
	var buf bytes.Buffer

	log := New(Config{
		Level:  LevelWarn,
		Format: "json",
		Output: &buf,
	})

	log.Debug("debug message")
	log.Info("info message")

	output := buf.String()

	if output != "" {
		t.Errorf("expected no output for filtered levels, got: %s", output)
	}

	log.Warn("warn message")
	output = buf.String()

	if !strings.Contains(output, "warn message") {
		t.Errorf("expected warn message in output, got: %s", output)
	}
}

func TestLoggerWith(t *testing.T) {
	var buf bytes.Buffer

	log := New(Config{
		Level:  LevelDebug,
		Format: "json",
		Output: &buf,
	})

	childLog := log.With(F("service", "test"))
	childLog.Info("test message")

	output := buf.String()

	if !strings.Contains(output, `"service":"test"`) {
		t.Errorf("expected service field in output, got: %s", output)
	}
}

func TestField(t *testing.T) {
	f := F("key", "value")

	if f.Key != "key" {
		t.Errorf("expected key to be 'key', got: %s", f.Key)
	}
	if f.Value != "value" {
		t.Errorf("expected value to be 'value', got: %v", f.Value)
	}
}
