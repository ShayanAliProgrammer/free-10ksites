package app

import (
	"context"
	"html/template"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"golang.org/x/time/rate"

	"10ksites/internal/db"
)

// App holds all application state: database, WebSocket hub, cache,
// session store, rate limiter, templates, and config.
type App struct {
	db        *db.DB
	hub       *Hub
	cache     *Cache
	sessions  *SessionStore
	limiter   *rateLimiter
	auth      *AuthConfig
	tmpl      *template.Template
	config    *Config
}

// Config holds all application configuration from environment variables.
type Config struct {
	Port          string
	TursoURL      string
	TursoToken    string
	LocalDBPath   string
	CookieSecure  bool
}

func loadConfig() *Config {
	cfg := &Config{
		Port:        envOr("PORT", "3000"),
		TursoURL:    getEnv("TURSO_DATABASE_URL"),
		TursoToken:  getEnv("TURSO_AUTH_TOKEN"),
		LocalDBPath: getEnv("DB_PATH"),
		CookieSecure: getEnv("COOKIE_SECURE") == "true" ||
			getEnv("FORCE_HTTPS") == "true",
	}
	return cfg
}

// getEnv is a thin wrapper over os.Getenv (separated for testability).
func getEnv(key string) string { return os.Getenv(key) }

// Run initializes and starts the server. This is called from cmd/server/main.go.
func Run() error {
	cfg := loadConfig()

	// Initialize database (Turso if configured, else local SQLite)
	database, err := db.Open(db.Config{
		TursoURL:    cfg.TursoURL,
		TursoToken:  cfg.TursoToken,
		LocalPath:   cfg.LocalDBPath,
	})
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.Init(); err != nil {
		return err
	}

	// Initialize auth (bcrypt hash from env)
	auth, err := NewAuthConfig()
	if err != nil {
		return err
	}

	// Load embedded templates
	tmpl, err := loadTemplates()
	if err != nil {
		return err
	}

	app := &App{
		db:       database,
		hub:      NewHub(),
		cache:    NewCache(),
		sessions: NewSessionStore(),
		limiter:  newRateLimiter(),
		auth:     auth,
		tmpl:     tmpl,
		config:   cfg,
	}

	// Seed demo data if DB is empty
	app.ensureSeedData()

	// Start WebSocket hub
	go app.hub.Run()

	// Build router
	r := app.buildRouter()

	// HTTP server with timeouts (prevents slowloris attacks)
	server := &http.Server{
		Addr:         "0.0.0.0:" + cfg.Port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB max header
	}

	// Graceful shutdown
	go func() {
		log.Printf("🚀 10K Sites server listening on http://0.0.0.0:%s", cfg.Port)
		if cfg.TursoURL != "" {
			log.Printf("   Database: Turso (%s)", cfg.TursoURL)
		}
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Server error: %v", err)
		}
	}()

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server…")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(ctx)
}

// buildRouter sets up all routes with security middleware.
func (app *App) buildRouter() http.Handler {
	r := chi.NewRouter()

	// Core middleware (applied to all routes)
	r.Use(middleware.RealIP)          // Trust X-Forwarded-For from Caddy
	r.Use(middleware.Recoverer)       // Panic recovery
	r.Use(securityHeaders)            // CSP, HSTS, X-Frame-Options, etc.
	r.Use(csrfCheck)                  // Origin check on POST/PUT/PATCH/DELETE
	r.Use(app.requestLogger())        // Structured logging (no sensitive data)

	// Static assets (embedded — served from memory, no filesystem reads)
	r.Get("/static/tailwind.css", app.serveCSS)
	r.Get("/static/htmx.min.js", app.serveHTMX)
	r.Get("/static/js/app.js", app.serveAppJS)
	r.Get("/static/js/security.js", app.serveSecurityJS)

	// WebSocket endpoint (rate limited to prevent connection flooding)
	r.With(app.rateLimit(rate.Every(time.Second), 5)).Get("/ws", app.HandleWS)

	// Public routes (generous rate limit — 30 req/s per IP)
	r.With(app.rateLimit(rate.Limit(30), 60)).Group(func(r chi.Router) {
		r.Get("/", app.indexHandler)
		r.Post("/track", app.trackSearchHandler)
		r.Get("/track/{id}", app.trackHandler)
		r.Get("/reset", app.resetHandler)
		r.Get("/stats", app.statsHandler)
	})

	// Admin login (strict rate limit — 1 req/s, burst 5, prevents brute force)
	r.With(app.rateLimit(rate.Limit(1), 5)).Group(func(r chi.Router) {
		r.Get("/admin", app.adminLoginHandler)
		r.Post("/admin/login", app.adminLoginPostHandler)
	})

	// Admin logout
	r.Post("/admin/logout", app.adminLogoutHandler)

	// Admin panel (auth required + rate limited)
	r.With(
		app.rateLimit(rate.Limit(10), 20),
		app.requireAdmin,
	).Group(func(r chi.Router) {
		r.Get("/admin/panel", app.adminPanelHandler)
		r.Get("/admin/requests", app.adminListHandler)
		r.Get("/admin/requests/new", app.adminNewFormHandler)
		r.Post("/admin/requests", app.adminCreateHandler)
		r.Get("/admin/requests/{id}/edit", app.adminEditFormHandler)
		r.Post("/admin/requests/{id}", app.adminUpdateHandler)
		r.Post("/admin/requests/{id}/delete", app.adminDeleteHandler)
	})

	// JSON API (rate limited)
	r.With(app.rateLimit(rate.Limit(20), 40)).Group(func(r chi.Router) {
		r.Get("/api/track/{id}", app.apiTrackHandler)
		r.Get("/api/stats", app.apiStatsHandler)
	})

	// Admin JSON API (auth required)
	r.With(
		app.rateLimit(rate.Limit(10), 20),
		app.requireAdmin,
	).Group(func(r chi.Router) {
		r.Get("/api/requests", app.apiListRequestsHandler)
		r.Post("/api/requests", app.apiCreateRequestHandler)
		r.Patch("/api/requests/{id}", app.apiUpdateRequestHandler)
		r.Delete("/api/requests/{id}", app.apiDeleteRequestHandler)
	})

	// Seed endpoint (auth required — dangerous if public)
	r.With(app.requireAdmin).Post("/api/seed", app.apiSeedHandler)

	// 404 handler
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`<html><body style="font-family:sans-serif;text-align:center;padding:4rem"><h1>404</h1><p>Page not found.</p><p><a href="/">Go home →</a></p></body></html>`))
	})

	return r
}

// requestLogger logs requests without sensitive data (no passwords, no body).
func (app *App) requestLogger() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			log.Printf("%s %s %s %d %dms",
				r.Method, r.URL.Path, clientIP(r),
				ww.Status(), time.Since(start).Milliseconds())
		})
	}
}

// ============================================================================
// Static asset handlers (serve embedded data — no filesystem reads)
// ============================================================================

func (app *App) serveCSS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(staticCSS)
}

func (app *App) serveHTMX(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Write(staticHTMX)
}

func (app *App) serveAppJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(staticAppJS)
}

func (app *App) serveSecurityJS(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(staticSecurityJS)
}
