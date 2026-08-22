package main

import (
	"context"
	"log/slog"
	"os"

	"recoverai/internal/config"
	"recoverai/internal/db"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	pool, err := db.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Seed(context.Background(), pool); err != nil {
		slog.Error("seed failed", "error", err)
		os.Exit(1)
	}

	slog.Info("database seeded successfully")
}
