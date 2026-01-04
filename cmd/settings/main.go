package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/lugondev/go-alert-web3-bnb/internal/config"
	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/internal/settings"
	"github.com/lugondev/go-alert-web3-bnb/internal/web"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "configs/config.yaml", "Path to configuration file")
	port := flag.Int("port", 8080, "HTTP server port")
	templatesDir := flag.String("templates", "internal/web/templates", "Path to templates directory")
	staticDir := flag.String("static", "internal/web/static", "Path to static files directory")
	reset := flag.Bool("reset", false, "Reset all settings in Redis")
	debug := flag.Bool("debug", false, "Enable debug mode")
	flag.Parse()

	// Override from environment variables
	if envPort := os.Getenv("WEB_PORT"); envPort != "" {
		if p, err := parsePort(envPort); err == nil {
			*port = p
		}
	}
	if envTemplates := os.Getenv("TEMPLATES_DIR"); envTemplates != "" {
		*templatesDir = envTemplates
	}
	if envStatic := os.Getenv("STATIC_DIR"); envStatic != "" {
		*staticDir = envStatic
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("failed to load configuration", logger.F("error", err))
	}

	// Initialize logger
	log := initLogger(cfg)
	log.Info("starting settings web UI",
		logger.F("port", *port),
		logger.F("debug", *debug),
	)

	// Initialize Redis client
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.Host + ":" + itoa(cfg.Redis.Port),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
		DialTimeout:  cfg.Redis.DialTimeout,
		ReadTimeout:  cfg.Redis.ReadTimeout,
		WriteTimeout: cfg.Redis.WriteTimeout,
	})

	// Test Redis connection
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatal("failed to connect to Redis", logger.F("error", err))
	}
	log.Info("connected to Redis",
		logger.F("host", cfg.Redis.Host),
		logger.F("port", cfg.Redis.Port),
	)

	// Initialize settings repository and service
	repo := settings.NewRepository(rdb, log)
	service := settings.NewService(repo, log)

	// Handle --reset flag
	if *reset {
		log.Info("resetting all settings...")
		if err := service.ClearAll(ctx); err != nil {
			log.Fatal("failed to reset settings", logger.F("error", err))
		}
		log.Info("settings reset complete")
	}

	// Initialize settings from config
	telegramSettings := settings.TelegramSettings{
		BotToken:  cfg.Telegram.BotToken,
		ChatID:    cfg.Telegram.ChatID,
		RateLimit: cfg.Telegram.RateLimit,
		Enabled:   cfg.IsTelegramEnabled(),
	}

	tokenSettings := convertTokenConfigs(cfg.Tokens)

	if err := service.Initialize(ctx, telegramSettings, tokenSettings); err != nil {
		log.Warn("failed to initialize settings", logger.F("error", err))
	}

	// Create and start web server
	server := web.NewServer(web.Config{
		Port:         *port,
		TemplatesDir: *templatesDir,
		StaticDir:    *staticDir,
		Debug:        *debug,
	}, service, log)

	// Start server in goroutine
	go func() {
		if err := server.Start(); err != nil {
			log.Fatal("server error", logger.F("error", err))
		}
	}()

	log.Info("settings web UI started",
		logger.F("url", "http://localhost:"+itoa(*port)),
	)

	// Wait for shutdown signal
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	sig := <-shutdown
	log.Info("shutdown signal received", logger.F("signal", sig.String()))

	// Graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Error("server shutdown error", logger.F("error", err))
	}

	if err := rdb.Close(); err != nil {
		log.Error("redis close error", logger.F("error", err))
	}

	log.Info("shutdown complete")
}

// initLogger initializes the logger based on configuration
func initLogger(cfg *config.Config) logger.Logger {
	var output = os.Stdout
	if cfg.Logger.Output == "stderr" {
		output = os.Stderr
	}

	log := logger.New(logger.Config{
		Level:      logger.ParseLevel(cfg.Logger.Level),
		Format:     cfg.Logger.Format,
		Output:     output,
		TimeFormat: cfg.Logger.TimeFormat,
		AppName:    "settings-ui",
	})

	logger.SetGlobal(log)
	return log
}

// convertTokenConfigs converts config.TokenConfig to settings.TokenSettings
func convertTokenConfigs(tokens []config.TokenConfig) []settings.TokenSettings {
	result := make([]settings.TokenSettings, len(tokens))
	for i, t := range tokens {
		streams := make([]settings.StreamType, len(t.Streams))
		for j, s := range t.Streams {
			streams[j] = settings.StreamType(s)
		}
		result[i] = settings.TokenSettings{
			Address:       t.Address,
			ChainType:     settings.ChainType(t.ChainType),
			Streams:       streams,
			NotifyEnabled: true,
		}
	}
	return result
}

// parsePort parses port from string
func parsePort(s string) (int, error) {
	var port int
	_, err := os.Stderr.WriteString("")
	if err != nil {
		return 0, err
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, os.ErrInvalid
		}
		port = port*10 + int(c-'0')
	}
	return port, nil
}

// itoa converts int to string
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n%10) + '0'
		n /= 10
	}
	return string(buf[i:])
}
