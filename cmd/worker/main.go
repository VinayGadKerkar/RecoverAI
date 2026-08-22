package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"recoverai/internal/config"
	"recoverai/internal/db"
	"recoverai/internal/kafka"
	"recoverai/internal/outage"
	"recoverai/internal/redis"
	"recoverai/internal/services"
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

	outageDetector := outage.NewDetector(redisClient)

	riskSvc := services.NewRiskService(dbPool, redisClient, outageDetector)
	recoverySvc := services.NewRecoveryService(dbPool, redisClient, cfg)

	var wg sync.WaitGroup

	// Stage 2: Risk Engine consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		consumer, err := kafka.NewConsumer(cfg.KafkaBrokers, kafka.TopicPaymentEvents, "risk-engine-group")
		if err != nil {
			slog.Error("failed to create risk engine consumer", "error", err)
			return
		}
		defer consumer.Close()
		kafka.RunRiskEngineConsumer(ctx, consumer, riskSvc, dbPool)
	}()

	// Stage 3 + 4 + 5: Pre-Recovery Validator → AI → Policy Engine consumer
	wg.Add(1)
	go func() {
		defer wg.Done()
		consumer, err := kafka.NewConsumer(cfg.KafkaBrokers, kafka.TopicRiskScored, "recovery-group")
		if err != nil {
			slog.Error("failed to create recovery consumer", "error", err)
			return
		}
		defer consumer.Close()
		kafka.RunRecoveryConsumer(ctx, consumer, recoverySvc, dbPool, redisClient, cfg)
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
