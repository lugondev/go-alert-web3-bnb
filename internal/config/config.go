package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// envVarPattern matches ${VAR_NAME} or $VAR_NAME patterns
var envVarPattern = regexp.MustCompile(`\$\{([^}]+)\}|\$([a-zA-Z_][a-zA-Z0-9_]*)`)

// ChainType constants for supported chains
const (
	ChainTypeSolana = "CT_501" // Solana - Chain ID: 16
	ChainTypeBSC    = "56"     // BNB Smart Chain - Chain ID: 14
	ChainTypeBase   = "8453"   // Base - Chain ID: 199
)

// chainTypeToID maps chain type to chain ID
var chainTypeToID = map[string]int{
	ChainTypeSolana: 16,
	ChainTypeBSC:    14,
	ChainTypeBase:   199,
}

// validChainTypes contains all supported chain types
var validChainTypes = map[string]bool{
	ChainTypeSolana: true,
	ChainTypeBSC:    true,
	ChainTypeBase:   true,
}

// Config holds all application configuration
type Config struct {
	App       AppConfig       `yaml:"app"`
	WebSocket WebSocketConfig `yaml:"websocket"`
	Telegram  TelegramConfig  `yaml:"telegram"`
	Redis     RedisConfig     `yaml:"redis"`
	Logger    LoggerConfig    `yaml:"logger"`
	Tokens    []TokenConfig   `yaml:"tokens"`
}

// TokenConfig holds configuration for a token to subscribe
type TokenConfig struct {
	Address   string   `yaml:"address"`    // Token contract address
	ChainType string   `yaml:"chain_type"` // Chain type: CT_501 (Solana), 56 (BSC), 8453 (Base)
	Streams   []string `yaml:"streams"`    // Streams to subscribe: ticker24h, holders, tx, kline
}

// GetChainID returns the chain ID for this token based on chain type
func (t *TokenConfig) GetChainID() int {
	if id, ok := chainTypeToID[t.ChainType]; ok {
		return id
	}
	return 16 // Default to Solana chain ID
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
	Level        string   `yaml:"level"`
	Format       string   `yaml:"format"` // json or text
	Output       string   `yaml:"output"` // stdout, stderr, or file path
	TimeFormat   string   `yaml:"time_format"`
	DebugStreams []string `yaml:"debug_streams"` // Enable debug logging for specific stream types
}

// IsStreamDebugEnabled checks if debug logging is enabled for a specific stream type
func (l *LoggerConfig) IsStreamDebugEnabled(streamType string) bool {
	for _, s := range l.DebugStreams {
		if s == streamType {
			return true
		}
	}
	return false
}

// Load loads configuration from file and environment variables
// Load order (later overrides earlier):
// 1. Default values
// 2. .env file (if exists) - loaded into process environment
// 3. Process environment variables (already set in shell)
// 4. YAML config file with ${VAR} expansion
// 5. Environment variable overrides (explicit mappings)
func Load(configPath string) (*Config, error) {
	cfg := defaultConfig()

	// Load .env file first (if exists), won't override existing env vars
	loadDotEnv(configPath)

	// Load from YAML file if exists (with env var expansion)
	if configPath != "" {
		if err := loadFromFile(configPath, cfg); err != nil {
			return nil, fmt.Errorf("failed to load config file: %w", err)
		}
	}

	// Override with environment variables (explicit mappings take precedence)
	loadFromEnv(cfg)

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	return cfg, nil
}

// loadDotEnv loads .env file from multiple possible locations
// It does NOT override existing environment variables
func loadDotEnv(configPath string) {
	// Possible .env file locations (in order of priority)
	envPaths := []string{
		".env",       // Current working directory
		".env.local", // Local override
	}

	// Also check relative to config file location
	if configPath != "" {
		configDir := filepath.Dir(configPath)
		envPaths = append(envPaths,
			filepath.Join(configDir, ".env"),
			filepath.Join(configDir, "..", ".env"),
		)
	}

	// Load first found .env file (godotenv.Load won't override existing env vars)
	for _, envPath := range envPaths {
		if _, err := os.Stat(envPath); err == nil {
			if err := godotenv.Load(envPath); err == nil {
				// Successfully loaded, can continue to load more files
				// godotenv.Load doesn't override, so loading multiple is safe
			}
		}
	}
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
	// LOG_DEBUG_STREAMS: comma-separated stream types to enable debug logging
	// Example: LOG_DEBUG_STREAMS=tx,ticker24h,holders
	if v := os.Getenv("LOG_DEBUG_STREAMS"); v != "" {
		streams := strings.Split(v, ",")
		cfg.Logger.DebugStreams = make([]string, 0, len(streams))
		for _, s := range streams {
			s = strings.TrimSpace(s)
			if s != "" {
				cfg.Logger.DebugStreams = append(cfg.Logger.DebugStreams, s)
			}
		}
	}

	// Tokens from environment (semicolon-separated tokens)
	// Format: address:chain_type:streams
	// Supported chain_type: CT_501 (Solana), 56 (BSC), 8453 (Base)
	// Example: TOKENS=addr1:CT_501:ticker24h,holders,tx;addr2:56:ticker24h
	if v := os.Getenv("TOKENS"); v != "" {
		tokens := parseTokensFromEnv(v)
		if len(tokens) > 0 {
			cfg.Tokens = tokens
		}
	}
}

// parseTokensFromEnv parses tokens from environment variable
// Format: address:chain_type:streams (semicolon-separated for multiple tokens)
// Example: addr1:CT_501:ticker24h,holders,tx;addr2:56:ticker24h
func parseTokensFromEnv(value string) []TokenConfig {
	var tokens []TokenConfig
	tokenParts := strings.Split(value, ";")

	for _, part := range tokenParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		fields := strings.Split(part, ":")
		if len(fields) < 1 || fields[0] == "" {
			continue
		}

		token := TokenConfig{
			Address:   fields[0],
			ChainType: ChainTypeSolana, // Default to Solana
			Streams:   []string{"ticker24h", "holders", "tx"},
		}

		if len(fields) >= 2 && fields[1] != "" {
			chainType := fields[1]
			if validChainTypes[chainType] {
				token.ChainType = chainType
			}
		}
		if len(fields) >= 3 && fields[2] != "" {
			token.Streams = strings.Split(fields[2], ",")
		}

		tokens = append(tokens, token)
	}

	return tokens
}

// Validate validates the configuration
func (c *Config) Validate() error {
	if c.WebSocket.URL == "" {
		return fmt.Errorf("websocket URL is required")
	}
	if c.Redis.Host == "" {
		return fmt.Errorf("redis host is required")
	}

	// Validate token configurations
	for i, token := range c.Tokens {
		if token.Address == "" {
			return fmt.Errorf("token[%d]: address is required", i)
		}
		if !validChainTypes[token.ChainType] {
			return fmt.Errorf("token[%d]: invalid chain_type '%s', supported: CT_501 (Solana), 56 (BSC), 8453 (Base)", i, token.ChainType)
		}
	}

	// Telegram is optional - will be skipped if not configured
	return nil
}

// IsTelegramEnabled returns true if Telegram is configured
func (c *Config) IsTelegramEnabled() bool {
	return c.Telegram.BotToken != "" && c.Telegram.ChatID != ""
}

// BuildSubscribeChannels builds WebSocket subscribe channels from token configs
func (c *Config) BuildSubscribeChannels() []string {
	var channels []string

	for _, token := range c.Tokens {
		chainID := token.GetChainID()
		for _, stream := range token.Streams {
			switch stream {
			case "ticker24h", "holders":
				// Format: w3w@<contract_address>@<chain_type>@<stream_type>
				channel := fmt.Sprintf("w3w@%s@%s@%s", token.Address, token.ChainType, stream)
				channels = append(channels, channel)
			case "tx":
				// Format: tx@<chain_id>_<contract_address>
				channel := fmt.Sprintf("tx@%d_%s", chainID, token.Address)
				channels = append(channels, channel)
			case "kline":
				// Format: kl@<chain_id>@<contract_address>@<interval>
				// Default interval: 1m
				channel := fmt.Sprintf("kl@%d@%s@1m", chainID, token.Address)
				channels = append(channels, channel)
			}
		}
	}

	return channels
}
