package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"recoverai/internal/config"
	"recoverai/internal/consumers"
	"recoverai/internal/db"
	"recoverai/internal/kafka"
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	dbPool, err := db.NewPool(ctx, cfg.DatabaseURL)
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

	producer, err := kafka.NewProducer(cfg.KafkaBrokers)
	if err != nil {
		slog.Error("failed to create kafka producer", "error", err)
		os.Exit(1)
	}
	defer producer.Close()

	var wg sync.WaitGroup

	// ─── Stage 2: Risk Processor (payment.events → payment.risk_scored) ─────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		riskProcessor := consumers.NewRiskProcessor(dbPool, redisClient, producer, cfg)
		if err := riskProcessor.Run(ctx); err != nil {
			slog.Error("risk processor stopped with error", "error", err)
		}
	}()

	// ─── Stage 3-4: Validator + AI (payment.risk_scored → payment.ai_commands) ────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		validatorConsumer := consumers.NewValidatorConsumer(dbPool, redisClient, producer, cfg)
		if err := validatorConsumer.Run(ctx); err != nil {
			slog.Error("validator consumer stopped with error", "error", err)
		}
	}()

	// ─── Stage 5: Execution Worker (payment.ai_commands → payment.execution_results) ────
	wg.Add(1)
	go func() {
		defer wg.Done()
		executionWorker := consumers.NewExecutionWorker(dbPool, redisClient, producer, cfg)
		if err := executionWorker.Run(ctx); err != nil {
			slog.Error("execution worker stopped with error", "error", err)
		}
	}()

	// ─── Stage 6: Result Processor (recovery.results → finalize) ─────────────
	wg.Add(1)
	go func() {
		defer wg.Done()
		resultProcessor := consumers.NewResultProcessor(dbPool, cfg)
		if err := resultProcessor.Run(ctx); err != nil {
			slog.Error("result processor stopped with error", "error", err)
		}
	}()

	slog.Info("workers started")

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down workers...")
	cancel()
	wg.Wait()
	slog.Info("workers stopped")
}
