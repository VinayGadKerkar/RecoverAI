package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	"recoverai/internal/models"
	redisclient "recoverai/internal/redis"
	"recoverai/internal/services"
)

// Consumer wraps the Confluent Kafka consumer.
type Consumer struct {
	c *kafka.Consumer
}

// NewConsumer creates a new Kafka consumer for the given topic and group.
func NewConsumer(brokers, topic, groupID string) (*Consumer, error) {
	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":        brokers,
		"group.id":                 groupID,
		"auto.offset.reset":        "earliest",
		"enable.auto.commit":       false, // manual commit for at-least-once
		"max.poll.interval.ms":     300000,
		"session.timeout.ms":       30000,
		"heartbeat.interval.ms":    3000,
	})
	if err != nil {
		return nil, fmt.Errorf("create consumer: %w", err)
	}

	if err := c.Subscribe(topic, nil); err != nil {
		c.Close()
		return nil, fmt.Errorf("subscribe to %s: %w", topic, err)
	}

	return &Consumer{c: c}, nil
}

// Close shuts down the consumer.
func (c *Consumer) Close() error {
	return c.c.Close()
}

// ─── Stage 2: Risk Engine Consumer ───────────────────────────────────────────

// RunRiskEngineConsumer reads from TopicPaymentEvents and calls the risk service.
func RunRiskEngineConsumer(ctx context.Context, consumer *Consumer, riskSvc *services.RiskService, db *pgxpool.Pool) {
	slog.Info("risk engine consumer started")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := consumer.c.ReadMessage(100)
		if err != nil {
			if kafkaErr, ok := err.(kafka.Error); ok && kafkaErr.Code() == kafka.ErrTimedOut {
				continue
			}
			slog.Error("risk engine: read error", "error", err)
			continue
		}

		var event models.KafkaPaymentEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			slog.Error("risk engine: unmarshal error", "error", err)
			consumer.c.CommitMessage(msg)
			continue
		}

		if err := riskSvc.Score(ctx, &event); err != nil {
			slog.Error("risk engine: scoring failed", "payment_id", event.PaymentID, "error", err)
			// TODO: publish to dead-letter topic after max retries
		}

		consumer.c.CommitMessage(msg)
	}
}

// ─── Stage 3-5: Recovery Consumer ────────────────────────────────────────────

// RunRecoveryConsumer reads from TopicRiskScored and drives the full recovery pipeline.
func RunRecoveryConsumer(ctx context.Context, consumer *Consumer, recoverySvc *services.RecoveryService, db *pgxpool.Pool, r *redisclient.Client, cfg *config.Config) {
	slog.Info("recovery consumer started")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		msg, err := consumer.c.ReadMessage(100)
		if err != nil {
			if kafkaErr, ok := err.(kafka.Error); ok && kafkaErr.Code() == kafka.ErrTimedOut {
				continue
			}
			slog.Error("recovery consumer: read error", "error", err)
			continue
		}

		var event models.KafkaRiskScoredEvent
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			slog.Error("recovery consumer: unmarshal error", "error", err)
			consumer.c.CommitMessage(msg)
			continue
		}

		if err := recoverySvc.Process(ctx, &event); err != nil {
			slog.Error("recovery consumer: process failed", "payment_id", event.PaymentID, "error", err)
		}

		consumer.c.CommitMessage(msg)
	}
}
