package config

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// envVarPattern matches ${VAR_NAME} or $VAR_NAME patterns
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$([a-zA-Z_][a-zA-Z0-9_]*)`)

// Config holds all application configuration
type Config struct {
	App       AppConfig       `yaml:"app"`
	WebSocket WebSocketConfig `yaml:"websocket"`
	Telegram  TelegramConfig  `yaml:"telegram"`
	Redis     RedisConfig     `yaml:"redis"`
	Logger    LoggerConfig    `yaml:"logger"`
}

// AppConfig holds application-level configuration
type AppConfig struct {
	Name        string `yaml:"name"`
	Environment string `yaml:"environment"`
}

// WebSocketConfig holds WebSocket connection configuration
type WebSocketConfig struct {
	URL               string        `yaml:"url"`
	ReconnectInterval time.Duration `yaml:"reconnect_interval"`
	MaxRetries        int           `yaml:"max_retries"`
	PingInterval      time.Duration `yaml:"ping_interval"`
	PongTimeout       time.Duration `yaml:"pong_timeout"`
	WriteTimeout      time.Duration `yaml:"write_timeout"`
	ReadTimeout       time.Duration `yaml:"read_timeout"`
}

// TelegramConfig holds Telegram bot configuration
type TelegramConfig struct {
	BotToken   string        `yaml:"bot_token"`
	ChatID     string        `yaml:"chat_id"`
	RateLimit  int           `yaml:"rate_limit"`
	Timeout    time.Duration `yaml:"timeout"`
	RetryCount int           `yaml:"retry_count"`
}

// RedisConfig holds Redis connection configuration
type RedisConfig struct {
	Host         string        `yaml:"host"`
	Port         int           `yaml:"port"`
	Password     string        `yaml:"password"`
	DB           int           `yaml:"db"`
	PoolSize     int           `yaml:"pool_size"`
	MinIdleConns int           `yaml:"min_idle_conns"`
	DialTimeout  time.Duration `yaml:"dial_timeout"`
	ReadTimeout  time.Duration `yaml:"read_timeout"`
	WriteTimeout time.Duration `yaml:"write_timeout"`

	// Deduplication settings
	LockTTL      time.Duration `yaml:"lock_ttl"`
	ProcessedTTL time.Duration `yaml:"processed_ttl"`

	// Rate limiting settings
	RateLimitWindow time.Duration `yaml:"rate_limit_window"`
	RateLimitMax    int           `yaml:"rate_limit_max"`
}

// LoggerConfig holds logging configuration
type LoggerConfig struct {
	Level      string `yaml:"level"`
	Format     string `yaml:"format"` // json or text
	Output     string `yaml:"output"` // stdout, stderr, or file path
	TimeFormat string `yaml:"time_format"`
}

// Load loads configuration from file and environment variables
func Load(configPath string) (*Config, error) {
	cfg := defaultConfig()

	// Load from file if exists
	if configPath != "" {
		if err := loadFromFile(configPath, cfg); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	}

	// Override with environment variables
	loadFromEnv(cfg)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// defaultConfig returns configuration with default values
func defaultConfig() *Config {
	return &Config{
		App: AppConfig{
			Name:        "go-alert-web3-bnb",
			Environment: "development",
		},
		WebSocket: WebSocketConfig{
			URL:               "wss://stream.binance.com:9443/ws",
			ReconnectInterval: 5 * time.Second,
			MaxRetries:        10,
			PingInterval:      30 * time.Second,
			PongTimeout:       10 * time.Second,
			WriteTimeout:      10 * time.Second,
			ReadTimeout:       60 * time.Second,
		},
		Telegram: TelegramConfig{
			RateLimit:  30,
			Timeout:    30 * time.Second,
			RetryCount: 3,
		},
		Redis: RedisConfig{
			Host:            "localhost",
			Port:            6379,
			Password:        "",
			DB:              0,
			PoolSize:        10,
			MinIdleConns:    3,
			DialTimeout:     5 * time.Second,
			ReadTimeout:     3 * time.Second,
			WriteTimeout:    3 * time.Second,
			LockTTL:         30 * time.Second,
			ProcessedTTL:    5 * time.Minute,
			RateLimitWindow: time.Minute,
			RateLimitMax:    30,
		},
		Logger: LoggerConfig{
			Level:      "info",
			Format:     "json",
			Output:     "stdout",
			TimeFormat: time.RFC3339,
		},
	}
}

// loadFromFile loads configuration from a YAML file
func loadFromFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // File doesn't exist, use defaults
		}
		return err
	}

	// Expand environment variables in YAML content
	expanded := expandEnvVars(string(data))

	return yaml.Unmarshal([]byte(expanded), cfg)
}

// expandEnvVars replaces ${VAR} or $VAR with environment variable values
func expandEnvVars(content string) string {
	return envVarPattern.ReplaceAllStringFunc(content, func(match string) string {
		// Extract variable name from ${VAR} or $VAR
		var varName string
		if strings.HasPrefix(match, "${") {
			varName = match[2 : len(match)-1] // Remove ${ and }
		} else {
			varName = match[1:] // Remove $
		}

		// Get environment variable value
		if value := os.Getenv(varName); value != "" {
			return value
		}

		// Return empty string if env var not set (will be treated as disabled)
		return ""
	})
}

// loadFromEnv overrides configuration with environment variables
func loadFromEnv(cfg *Config) {
	// App
	if v := os.Getenv("APP_NAME"); v != "" {
		cfg.App.Name = v
	}
	if v := os.Getenv("APP_ENV"); v != "" {
		cfg.App.Environment = v
	}

	// WebSocket
	if v := os.Getenv("WS_URL"); v != "" {
		cfg.WebSocket.URL = v
	}
	if v := os.Getenv("WS_RECONNECT_INTERVAL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.WebSocket.ReconnectInterval = d
		}
	}
	if v := os.Getenv("WS_MAX_RETRIES"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.WebSocket.MaxRetries = n
		}
	}

	// Telegram
	if v := os.Getenv("TELEGRAM_BOT_TOKEN"); v != "" {
		cfg.Telegram.BotToken = v
	}
	if v := os.Getenv("TELEGRAM_CHAT_ID"); v != "" {
		cfg.Telegram.ChatID = v
	}
	if v := os.Getenv("TELEGRAM_RATE_LIMIT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Telegram.RateLimit = n
		}
	}

	// Redis
	if v := os.Getenv("REDIS_HOST"); v != "" {
		cfg.Redis.Host = v
	}
	if v := os.Getenv("REDIS_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Redis.Port = n
		}
	}
	if v := os.Getenv("REDIS_PASSWORD"); v != "" {
		cfg.Redis.Password = v
	}
	if v := os.Getenv("REDIS_DB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Redis.DB = n
		}
	}
	if v := os.Getenv("REDIS_POOL_SIZE"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Redis.PoolSize = n
		}
	}
	if v := os.Getenv("REDIS_LOCK_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Redis.LockTTL = d
		}
	}
	if v := os.Getenv("REDIS_PROCESSED_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			cfg.Redis.ProcessedTTL = d
		}
	}

	// Logger
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Logger.Level = v
	}
	if v := os.Getenv("LOG_FORMAT"); v != "" {
		cfg.Logger.Format = v
	}
	if v := os.Getenv("LOG_OUTPUT"); v != "" {
		cfg.Logger.Output = v
	}
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.WebSocket.URL == "" {
		return fmt.Errorf("websocket URL is required")
	}
	if c.Redis.Host == "" {
		return fmt.Errorf("redis host is required")
	}
	// Telegram is optional - will be skipped if not configured
	return nil
}

// IsTelegramEnabled returns true if Telegram is configured
func (c *Config) IsTelegramEnabled() bool {
	return c.Telegram.BotToken != "" && c.Telegram.ChatID != ""
}
