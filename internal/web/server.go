package web

import (
	"context"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	fiberlogger "github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/template/html/v2"

	"github.com/lugondev/go-alert-web3-bnb/internal/logger"
	"github.com/lugondev/go-alert-web3-bnb/internal/settings"
	"github.com/lugondev/go-alert-web3-bnb/internal/web/handlers"
)

// Server represents the HTTP server
type Server struct {
	app             *fiber.App
	service         *settings.Service
	log             logger.Logger
	port            int
	settingsHandler *handlers.SettingsHandler
}

// Config holds server configuration
type Config struct {
	Port         int
	TemplatesDir string
	StaticDir    string
	Debug        bool
}

// NewServer creates a new HTTP server
func NewServer(cfg Config, service *settings.Service, log logger.Logger) *Server {
	// Initialize template engine
	engine := html.New(cfg.TemplatesDir, ".html")

	// Add template functions
	engine.AddFunc("contains", func(slice []string, item string) bool {
		for _, s := range slice {
			if s == item {
				return true
			}
		}
		return false
	})

	engine.AddFunc("eq", func(a, b interface{}) bool {
		return a == b
	})

	// Enable reload in debug mode
	if cfg.Debug {
		engine.Reload(true)
		engine.Debug(true)
	}

	// Create Fiber app
	app := fiber.New(fiber.Config{
		Views:       engine,
		ViewsLayout: "layouts/main",
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}

			log.Error("HTTP error", logger.F("error", err), logger.F("code", code))

			return c.Status(code).Render("error", fiber.Map{
				"Title":   "Error",
				"Code":    code,
				"Message": err.Error(),
			})
		},
	})

	// Middleware
	app.Use(recover.New())
	app.Use(cors.New())
	app.Use(fiberlogger.New(fiberlogger.Config{
		Format: "[${time}] ${status} - ${method} ${path} ${latency}\n",
	}))

	// Static files
	app.Static("/static", cfg.StaticDir)

	// Create server
	server := &Server{
		app:     app,
		service: service,
		log:     log.With(logger.F("component", "web-server")),
		port:    cfg.Port,
	}

	// Register routes
	settingsHandler := handlers.NewSettingsHandler(service, log)
	settingsHandler.RegisterRoutes(app)
	server.settingsHandler = settingsHandler

	// Health check
	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status": "healthy",
		})
	})

	return server
}

// Start starts the HTTP server
func (s *Server) Start() error {
	addr := fmt.Sprintf(":%d", s.port)
	s.log.Info("starting web server", logger.F("port", s.port))
	return s.app.Listen(addr)
}

// SetSubscriptionSyncer sets the subscription syncer for the settings handler
func (s *Server) SetSubscriptionSyncer(syncer handlers.SubscriptionSyncer) {
	if s.settingsHandler != nil {
		s.settingsHandler.SetSubscriptionSyncer(syncer)
	}
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("shutting down web server")
	return s.app.ShutdownWithContext(ctx)
}
