package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lugondev/go-alert-web3-bnb/internal/config"
	"github.com/lugondev/go-alert-web3-bnb/internal/handler"
	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/internal/redis"
	"github.com/lugondev/go-alert-web3-bnb/internal/telegram"
	"github.com/lugondev/go-alert-web3-bnb/internal/websocket"
	"github.com/lugondev/go-alert-web3-bnb/pkg/models"
)

// Application holds all application dependencies
type Application struct {
	config       *config.Config
	log          logger.Logger
	redisClient  *redis.Client
	deduplicator *redis.Deduplicator
	rateLimiter  *redis.RateLimiter
	pubsub       *redis.PubSub
	notifier     *telegram.Notifier
	eventHandler *handler.EventHandler
	wsClient     *websocket.Client
}

func main() {
	// Parse command line flags
	configPath := flag.String("config", "configs/config.yaml", "Path to configuration file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatal("failed to load configuration", logger.F("error", err))
	}

	// Initialize logger
	log := initLogger(cfg)
	log.Info("starting application",
		logger.F("app", cfg.App.Name),
		logger.F("env", cfg.App.Environment),
	)

	// Create context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Initialize application
	app, err := initApplication(ctx, cfg, log)
	if err != nil {
		log.Fatal("failed to initialize application", logger.F("error", err))
	}

	// Start the application
	if err := app.Start(ctx); err != nil {
		log.Fatal("failed to start application", logger.F("error", err))
	}

	// Wait for shutdown signal
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	sig := <-shutdown
	log.Info("shutdown signal received", logger.F("signal", sig.String()))

	// Graceful shutdown
	app.Shutdown(ctx, cancel)
}

// initApplication initializes all application components
func initApplication(ctx context.Context, cfg *config.Config, log logger.Logger) (*Application, error) {
	app := &Application{
		config: cfg,
		log:    log,
	}

	// Initialize Redis client
	redisClient, err := redis.NewClient(redis.Config{
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
		return nil, err
	}
	app.redisClient = redisClient

	// Initialize deduplicator
	app.deduplicator = redis.NewDeduplicator(redisClient, log, redis.DeduplicatorConfig{
		LockTTL:      cfg.Redis.LockTTL,
		ProcessedTTL: cfg.Redis.ProcessedTTL,
	})

	// Initialize rate limiter
	app.rateLimiter = redis.NewRateLimiter(redisClient, log)

	// Initialize pub/sub
	app.pubsub = redis.NewPubSub(redisClient, app.deduplicator.GetInstanceID(), log)

	// Subscribe to Redis channels
	if err := app.pubsub.Subscribe(ctx, redis.GetEventChannel(), redis.GetHeartbeatChannel()); err != nil {
		log.Warn("failed to subscribe to redis channels", logger.F("error", err))
	}

	// Register pub/sub handlers
	app.pubsub.RegisterHandler(redis.GetEventChannel(), func(msg *redis.PubSubMessage) {
		log.Debug("received event from another instance",
			logger.F("type", msg.Type),
			logger.F("from_instance", msg.InstanceID),
		)
	})

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
		log,
		handler.Config{
			QueueSize:       100,
			WorkerCount:     3,
			EnableDedup:     true, // Enable Redis deduplication
			RateLimitKey:    "telegram:notify",
			RateLimitMax:    cfg.Redis.RateLimitMax,
			RateLimitWindow: cfg.Redis.RateLimitWindow,
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
	}, log)

	// Set the event handler
	app.wsClient.SetHandler(app.eventHandler.Handle)

	log.Info("application initialized",
		logger.F("instance_id", app.deduplicator.GetInstanceID()),
		logger.F("redis_host", cfg.Redis.Host),
		logger.F("dedup_enabled", true),
	)

	return app, nil
}

// Start starts all application components
func (app *Application) Start(ctx context.Context) error {
	// Start pub/sub listener
	app.pubsub.Start(ctx)

	// Start event handler workers
	app.eventHandler.Start(ctx, 3)

	// Start WebSocket client
	if err := app.wsClient.Start(ctx); err != nil {
		return err
	}

	// Subscribe to W3W channels
	// Format: w3w@<contract_address>@<chain_type>@<stream_type>
	// Streams: ticker24h, holders
	// Transaction: tx@<chain_id>_<contract_address>
	// Kline: kl@<chain_id>@<contract_address>@<interval>
	subscribeChannels := []string{
		// SOL ticker 24h
		"w3w@So11111111111111111111111111111111111111111@CT_501@ticker24h",
		// Example token ticker 24h
		"w3w@pumpCmXqMfrsAkQ5r49WcJnRayYRqmXz6ae8H7H9Dfn@CT_501@ticker24h",
		// Holders stream
		"w3w@pumpCmXqMfrsAkQ5r49WcJnRayYRqmXz6ae8H7H9Dfn@CT_501@holders",
		// Transaction streams
		"tx@16_pumpCmXqMfrsAkQ5r49WcJnRayYRqmXz6ae8H7H9Dfn",
	}

	// Build subscribe message with custom format
	subscribeMsg := map[string]interface{}{
		"id":     "3",
		"method": "SUBSCRIBE",
		"params": subscribeChannels,
	}
	if err := app.wsClient.Send(ctx, subscribeMsg); err != nil {
		app.log.Error("failed to subscribe to channels", logger.F("error", err))
	}

	app.log.Info("application started successfully",
		logger.F("instance_id", app.deduplicator.GetInstanceID()),
		logger.F("websocket_url", app.config.WebSocket.URL),
		logger.F("subscribed_channels", subscribeChannels),
	)

	// Send startup notification
	formatter := telegram.NewFormatter()
	startupMsg := formatter.FormatAlert("success", "Application Started",
		"Web3 Alert Bot instance started.\nInstance ID: "+app.deduplicator.GetInstanceID()[:8]+"...")
	if err := app.notifier.SendMarkdown(ctx, startupMsg); err != nil {
		app.log.Warn("failed to send startup notification", logger.F("error", err))
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
			if err := app.pubsub.PublishHeartbeat(ctx); err != nil {
				app.log.Warn("failed to publish heartbeat", logger.F("error", err))
			}
		}
	}
}

// Shutdown performs graceful shutdown of all components
func (app *Application) Shutdown(ctx context.Context, cancel context.CancelFunc) {
	app.log.Info("starting graceful shutdown",
		logger.F("instance_id", app.deduplicator.GetInstanceID()),
	)

	// Create shutdown context with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()

	// Send shutdown notification
	formatter := telegram.NewFormatter()
	shutdownMsg := formatter.FormatAlert("warning", "Application Stopping",
		"Web3 Alert Bot instance shutting down.\nInstance ID: "+app.deduplicator.GetInstanceID()[:8]+"...")
	if err := app.notifier.SendMarkdown(shutdownCtx, shutdownMsg); err != nil {
		app.log.Warn("failed to send shutdown notification", logger.F("error", err))
	}

	// Cancel main context
	cancel()

	// Close WebSocket connection
	if err := app.wsClient.Close(); err != nil {
		app.log.Error("error closing websocket", logger.F("error", err))
	}

	// Stop event handler
	app.eventHandler.Stop()

	// Close pub/sub
	if err := app.pubsub.Close(); err != nil {
		app.log.Error("error closing pubsub", logger.F("error", err))
	}

	// Close Redis client
	if err := app.redisClient.Close(); err != nil {
		app.log.Error("error closing redis", logger.F("error", err))
	}

	app.log.Info("graceful shutdown completed")
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

// Example of how to use the handler for custom events
func customEventHandler(event *models.Event) {
	switch event.Type {
	case models.EventTypeTransaction:
		// Handle transaction
	case models.EventTypePrice:
		// Handle price update
	default:
		// Handle other events
	}
}
