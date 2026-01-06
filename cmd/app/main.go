package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/lugondev/go-alert-web3-bnb/internal/config"
	"github.com/lugondev/go-alert-web3-bnb/internal/handler"
	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	intRedis "github.com/lugondev/go-alert-web3-bnb/internal/redis"
	"github.com/lugondev/go-alert-web3-bnb/internal/settings"
	"github.com/lugondev/go-alert-web3-bnb/internal/telegram"
	"github.com/lugondev/go-alert-web3-bnb/internal/web"
	"github.com/lugondev/go-alert-web3-bnb/internal/websocket"
)

// Flags
var (
	configPath   = flag.String("config", "configs/config.yaml", "Path to configuration file")
	enableUI     = flag.Bool("ui", false, "Enable settings web UI")
	enableServer = flag.Bool("server", false, "Enable WebSocket alert server")
	webPort      = flag.Int("port", 8080, "Web UI port")
	templatesDir = flag.String("templates", "internal/web/templates", "Path to templates directory")
	staticDir    = flag.String("static", "internal/web/static", "Path to static files directory")
	resetRedis   = flag.Bool("reset", false, "Reset all settings in Redis before starting")
	debug        = flag.Bool("debug", false, "Enable debug mode")
)

// Application holds all application components
type Application struct {
	cfg          *config.Config
	log          logger.Logger
	rdb          *redis.Client
	redisClient  *intRedis.Client
	deduplicator *intRedis.Deduplicator
	rateLimiter  *intRedis.RateLimiter
	pubsub       *intRedis.PubSub
	tickerCache  *intRedis.TickerCache
	notifier     *telegram.Notifier
	eventHandler *handler.EventHandler
	wsClient     *websocket.Client
	webServer    *web.Server
	settingsSvc  *settings.Service
	subManager   *websocket.SubscriptionManager

	enableUI     bool
	enableServer bool
}

func main() {
	flag.Parse()

	// Override from environment variables
	overrideFromEnv()

	// If no mode specified, enable both by default
	if !*enableUI && !*enableServer {
		*enableUI = true
		*enableServer = true
	}

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("failed to load configuration", logger.F("error", err))
	}

	// Initialize logger
	log := initLogger(cfg)
	log.Info("starting go-alert-web3-bnb",
		logger.F("app", cfg.App.Name),
		logger.F("env", cfg.App.Environment),
		logger.F("ui_enabled", *enableUI),
		logger.F("server_enabled", *enableServer),
	)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize application
	app := &Application{
		cfg:          cfg,
		log:          log,
		enableUI:     *enableUI,
		enableServer: *enableServer,
	}

	if err := app.initialize(ctx); err != nil {
		log.Fatal("failed to initialize application", logger.F("error", err))
	}

	// Start the application
	if err := app.start(ctx); err != nil {
		log.Fatal("failed to start application", logger.F("error", err))
	}

	// Wait for shutdown signal
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	sig := <-shutdown
	log.Info("shutdown signal received", logger.F("signal", sig.String()))

	// Graceful shutdown
	app.shutdown(ctx, cancel)
}

// initialize initializes all application components
func (app *Application) initialize(ctx context.Context) error {
	cfg := app.cfg
	log := app.log

	// Initialize raw Redis client for settings
	app.rdb = redis.NewClient(&redis.Options{
		Addr:         fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
		DialTimeout:  cfg.Redis.DialTimeout,
		ReadTimeout:  cfg.Redis.ReadTimeout,
		WriteTimeout: cfg.Redis.WriteTimeout,
	})

	// Test Redis connection
	if err := app.rdb.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("failed to connect to redis: %w", err)
	}
	log.Info("connected to Redis",
		logger.F("host", cfg.Redis.Host),
		logger.F("port", cfg.Redis.Port),
	)

	// Initialize settings service
	settingsRepo := settings.NewRepository(app.rdb, log)
	app.settingsSvc = settings.NewService(settingsRepo, log)

	// Handle --reset flag
	if *resetRedis {
		log.Info("resetting all settings in Redis...")
		if err := app.settingsSvc.ClearAll(ctx); err != nil {
			return fmt.Errorf("failed to reset settings: %w", err)
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

	if err := app.settingsSvc.Initialize(ctx, telegramSettings, tokenSettings); err != nil {
		log.Warn("failed to initialize settings from config", logger.F("error", err))
	}

	// Initialize server components if enabled
	if app.enableServer {
		if err := app.initializeServer(ctx); err != nil {
			return err
		}
	}

	// Initialize web UI if enabled
	if app.enableUI {
		app.webServer = web.NewServer(web.Config{
			Port:         *webPort,
			TemplatesDir: *templatesDir,
			StaticDir:    *staticDir,
			Debug:        *debug,
		}, app.settingsSvc, log)
	}

	return nil
}

// initializeServer initializes WebSocket server components
func (app *Application) initializeServer(ctx context.Context) error {
	cfg := app.cfg
	log := app.log

	// Initialize wrapped Redis client
	redisClient, err := intRedis.NewClient(intRedis.Config{
		Host:         cfg.Redis.Host,
		Port:         cfg.Redis.Port,
		Password:     cfg.Redis.Password,
		DB:           cfg.Redis.DB,
		PoolSize:     cfg.Redis.PoolSize,
		MinIdleConns: cfg.Redis.MinIdleConns,
		DialTimeout:  cfg.Redis.DialTimeout,
		ReadTimeout:  cfg.Redis.ReadTimeout,
		WriteTimeout: cfg.Redis.WriteTimeout,
	}, log)
	if err != nil {
		return fmt.Errorf("failed to create redis client: %w", err)
	}
	app.redisClient = redisClient

	// Initialize deduplicator
	app.deduplicator = intRedis.NewDeduplicator(redisClient, log, intRedis.DeduplicatorConfig{
		LockTTL:      cfg.Redis.LockTTL,
		ProcessedTTL: cfg.Redis.ProcessedTTL,
	})

	// Initialize rate limiter
	app.rateLimiter = intRedis.NewRateLimiter(redisClient, log)

	// Initialize pub/sub
	app.pubsub = intRedis.NewPubSub(redisClient, app.deduplicator.GetInstanceID(), log)

	// Subscribe to Redis channels
	if err := app.pubsub.Subscribe(ctx, intRedis.GetEventChannel(), intRedis.GetHeartbeatChannel()); err != nil {
		log.Warn("failed to subscribe to redis channels", logger.F("error", err))
	}

	// Register pub/sub handlers
	app.pubsub.RegisterHandler(intRedis.GetEventChannel(), func(msg *intRedis.PubSubMessage) {
		log.Debug("received event from another instance",
			logger.F("type", msg.Type),
			logger.F("from_instance", msg.InstanceID),
		)
	})

	// Initialize ticker cache
	app.tickerCache = intRedis.NewTickerCache(redisClient.GetClient(), log)

	// Initialize Telegram notifier
	app.notifier = telegram.NewNotifier(telegram.Config{
		BotToken:   cfg.Telegram.BotToken,
		ChatID:     cfg.Telegram.ChatID,
		RateLimit:  cfg.Telegram.RateLimit,
		Timeout:    cfg.Telegram.Timeout,
		RetryCount: cfg.Telegram.RetryCount,
	}, log)

	// Log telegram status
	if cfg.IsTelegramEnabled() {
		log.Info("telegram notifications enabled",
			logger.F("chat_id", cfg.Telegram.ChatID),
		)
	} else {
		log.Warn("telegram notifications disabled: missing bot token or chat ID")
	}

	// Initialize event handler with Redis deduplication
	app.eventHandler = handler.NewEventHandler(
		app.notifier,
		app.deduplicator,
		app.rateLimiter,
		app.pubsub,
		app.tickerCache,
		app.settingsSvc,
		log,
		handler.Config{
			QueueSize:       100,
			WorkerCount:     3,
			EnableDedup:     true,
			RateLimitKey:    "telegram:notify",
			RateLimitMax:    cfg.Redis.RateLimitMax,
			RateLimitWindow: cfg.Redis.RateLimitWindow,
			DebugStreams:    cfg.Logger.DebugStreams,
		},
	)

	// Initialize WebSocket client
	app.wsClient = websocket.NewClient(websocket.Config{
		URL:               cfg.WebSocket.URL,
		ReconnectInterval: cfg.WebSocket.ReconnectInterval,
		MaxRetries:        cfg.WebSocket.MaxRetries,
		PingInterval:      cfg.WebSocket.PingInterval,
		PongTimeout:       cfg.WebSocket.PongTimeout,
		WriteTimeout:      cfg.WebSocket.WriteTimeout,
		ReadTimeout:       cfg.WebSocket.ReadTimeout,
		DebugStreams:      cfg.Logger.DebugStreams,
	}, log)

	// Set the event handler
	app.wsClient.SetHandler(app.eventHandler.Handle)

	// Initialize subscription manager
	app.subManager = websocket.NewSubscriptionManager(app.wsClient, app.settingsSvc, log)

	log.Info("server components initialized",
		logger.F("instance_id", app.deduplicator.GetInstanceID()),
	)

	return nil
}

// start starts all enabled components
func (app *Application) start(ctx context.Context) error {
	var wg sync.WaitGroup

	// Start server if enabled
	if app.enableServer {
		if err := app.startServer(ctx); err != nil {
			return err
		}

		// Set subscription manager on web server if both are enabled
		if app.enableUI && app.webServer != nil && app.subManager != nil {
			app.webServer.SetSubscriptionSyncer(app.subManager)
			app.log.Info("subscription syncer connected to web server")
		}
	}

	// Start web UI if enabled
	if app.enableUI {
		wg.Add(1)
		go func() {
			defer wg.Done()
			app.log.Info("starting settings web UI",
				logger.F("port", *webPort),
				logger.F("url", fmt.Sprintf("http://localhost:%d", *webPort)),
			)
			if err := app.webServer.Start(); err != nil {
				app.log.Error("web server error", logger.F("error", err))
			}
		}()
	}

	app.log.Info("application started successfully",
		logger.F("ui_enabled", app.enableUI),
		logger.F("server_enabled", app.enableServer),
	)

	return nil
}

// startServer starts WebSocket alert server
func (app *Application) startServer(ctx context.Context) error {
	// Start pub/sub listener
	app.pubsub.Start(ctx)

	// Start event handler workers
	app.eventHandler.Start(ctx, 3)

	// Start WebSocket client
	if err := app.wsClient.Start(ctx); err != nil {
		return err
	}

	// Get subscriptions from settings (Redis)
	subscribeChannels, err := app.settingsSvc.GetActiveSubscriptions(ctx)
	if err != nil {
		app.log.Warn("failed to get subscriptions from settings, using config",
			logger.F("error", err))
		subscribeChannels = app.cfg.BuildSubscribeChannels()
	}

	if len(subscribeChannels) == 0 {
		app.log.Warn("no tokens configured for subscription")
	} else {
		// Subscribe to channels using the Subscribe method (tracks channels for unsubscribe)
		if err := app.wsClient.Subscribe(ctx, subscribeChannels); err != nil {
			app.log.Error("failed to subscribe to channels", logger.F("error", err))
		}
		app.log.Info("subscribed to channels",
			logger.F("channels", subscribeChannels),
			logger.F("count", len(subscribeChannels)),
		)
	}

	// Send startup notification
	if app.cfg.IsTelegramEnabled() {
		formatter := telegram.NewFormatter()
		instanceID := app.deduplicator.GetInstanceID()
		if len(instanceID) > 8 {
			instanceID = instanceID[:8]
		}
		startupMsg := formatter.FormatAlert("success", "Application Started",
			fmt.Sprintf("Web3 Alert Bot started.\nInstance: %s...\nChannels: %d",
				instanceID, len(subscribeChannels)))
		if err := app.notifier.SendMarkdown(ctx, startupMsg); err != nil {
			app.log.Warn("failed to send startup notification", logger.F("error", err))
		}
	}

	// Start heartbeat goroutine
	go app.heartbeatLoop(ctx)

	return nil
}

// heartbeatLoop sends periodic heartbeats to Redis
func (app *Application) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if app.pubsub != nil {
				if err := app.pubsub.PublishHeartbeat(ctx); err != nil {
					app.log.Warn("failed to publish heartbeat", logger.F("error", err))
				}
			}
		}
	}
}

// shutdown performs graceful shutdown of all components
func (app *Application) shutdown(ctx context.Context, cancel context.CancelFunc) {
	app.log.Info("starting graceful shutdown")

	// Cancel main context first to stop all goroutines
	cancel()

	// Create shutdown context with short timeout (5 seconds)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	// Use WaitGroup to track shutdown tasks
	var wg sync.WaitGroup

	// Send shutdown notification async (non-blocking)
	if app.enableServer && app.cfg.IsTelegramEnabled() && app.notifier != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			notifyCtx, notifyCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer notifyCancel()

			formatter := telegram.NewFormatter()
			instanceID := ""
			if app.deduplicator != nil {
				instanceID = app.deduplicator.GetInstanceID()
				if len(instanceID) > 8 {
					instanceID = instanceID[:8]
				}
			}
			shutdownMsg := formatter.FormatAlert("warning", "Application Stopping",
				fmt.Sprintf("Web3 Alert Bot shutting down.\nInstance: %s...", instanceID))
			if err := app.notifier.SendMarkdown(notifyCtx, shutdownMsg); err != nil {
				app.log.Warn("failed to send shutdown notification", logger.F("error", err))
			}
		}()
	}

	// Shutdown web server
	if app.webServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := app.webServer.Shutdown(shutdownCtx); err != nil {
				app.log.Error("error shutting down web server", logger.F("error", err))
			}
		}()
	}

	// Shutdown server components
	if app.enableServer {
		// Close WebSocket first (stops incoming events)
		if app.wsClient != nil {
			if err := app.wsClient.Close(); err != nil {
				app.log.Error("error closing websocket", logger.F("error", err))
			}
		}

		// Stop event handler (drain queue)
		if app.eventHandler != nil {
			app.eventHandler.Stop()
		}

		// Close pubsub
		if app.pubsub != nil {
			if err := app.pubsub.Close(); err != nil {
				app.log.Error("error closing pubsub", logger.F("error", err))
			}
		}

		// Close Redis client
		if app.redisClient != nil {
			if err := app.redisClient.Close(); err != nil {
				app.log.Error("error closing redis client", logger.F("error", err))
			}
		}
	}

	// Close raw Redis client
	if app.rdb != nil {
		if err := app.rdb.Close(); err != nil {
			app.log.Error("error closing redis", logger.F("error", err))
		}
	}

	// Wait for async tasks with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		app.log.Info("graceful shutdown completed")
	case <-time.After(3 * time.Second):
		app.log.Warn("shutdown timeout, forcing exit")
	}
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
		AppName:    cfg.App.Name,
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

// overrideFromEnv overrides flags from environment variables
func overrideFromEnv() {
	if v := os.Getenv("CONFIG_PATH"); v != "" {
		*configPath = v
	}
	if v := os.Getenv("ENABLE_UI"); v == "true" || v == "1" {
		*enableUI = true
	}
	if v := os.Getenv("ENABLE_SERVER"); v == "true" || v == "1" {
		*enableServer = true
	}
	if v := os.Getenv("WEB_PORT"); v != "" {
		if p, err := parsePort(v); err == nil {
			*webPort = p
		}
	}
	if v := os.Getenv("TEMPLATES_DIR"); v != "" {
		*templatesDir = v
	}
	if v := os.Getenv("STATIC_DIR"); v != "" {
		*staticDir = v
	}
	if v := os.Getenv("DEBUG"); v == "true" || v == "1" {
		*debug = true
	}
}

// parsePort parses port from string
func parsePort(s string) (int, error) {
	port := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid port: %s", s)
		}
		port = port*10 + int(c-'0')
	}
	if port <= 0 || port > 65535 {
		return 0, fmt.Errorf("port out of range: %d", port)
	}
	return port, nil
}
