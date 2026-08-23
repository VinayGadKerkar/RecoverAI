package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"recoverai/internal/config"
	"recoverai/internal/db"
	"recoverai/internal/handlers"
	custommiddleware "recoverai/internal/middleware"
	"recoverai/internal/redis"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	dbPool, err := db.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	redisClient, err := redis.NewClient(cfg.RedisURL)
	if err != nil {
		slog.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(custommiddleware.StructuredLogger(logger))
	r.Use(custommiddleware.RateLimit(redisClient))

	// ─── Health check (no auth) ───────────────────────────────────────────────
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"status":"ok"}`)
	})

	// ─── Razorpay webhook endpoint (public, HMAC-verified) ────────────────────
	// CRITICAL: Must respond within 5 seconds or Razorpay retries.
	// CRITICAL: At-least-once delivery — same event can arrive multiple times.
	// CRITICAL: Out-of-order delivery — payment.captured may arrive before payment.failed.
	webhookHandler := handlers.NewWebhookHandler(dbPool, redisClient, cfg)
	r.Post("/webhooks/razorpay", webhookHandler.Handle)

	// ─── Authenticated API routes (JWT required) ──────────────────────────────
	r.Group(func(r chi.Router) {
		r.Use(custommiddleware.JWTAuth(cfg.JWTSecret))
		r.Route("/api/v1", func(r chi.Router) {
			handlers.RegisterRecoveryRoutes(r, dbPool, redisClient, cfg)
			handlers.RegisterMerchantRoutes(r, dbPool, cfg)
			handlers.RegisterAnalyticsRoutes(r, dbPool, cfg)
		})
	})

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%s", cfg.Port),
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		slog.Info("API server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced shutdown", "error", err)
	}
}
