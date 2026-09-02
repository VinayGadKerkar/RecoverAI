package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	kafkapkg "recoverai/internal/kafka"
)

// Consumer listens to Kafka topics for events to broadcast via WebSocket
type Consumer struct {
	consumer    *kafka.Consumer
	broadcaster *Broadcaster
}

// NewConsumer creates a new WebSocket event consumer
func NewConsumer(kafkaBrokers []string, broadcaster *Broadcaster) (*Consumer, error) {
	brokerString := strings.Join(kafkaBrokers, ",")
	
	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":    brokerString,
		"group.id":             "websocket-broadcaster-group",
		"auto.offset.reset":    "earliest",
		"enable.auto.commit":   false,
		"max.poll.interval.ms": 300000,
	})
	if err != nil {
		return nil, err
	}

	if err := consumer.Subscribe(kafkapkg.TopicWebSocketEvents, nil); err != nil {
		consumer.Close()
		return nil, err
	}

	return &Consumer{
		consumer:    consumer,
		broadcaster: broadcaster,
	}, nil
}

// Run starts consuming events and broadcasting them
func (c *Consumer) Run(ctx context.Context) error {
	slog.Info("websocket consumer starting", "topic", kafkapkg.TopicWebSocketEvents)
	defer c.consumer.Close()

	for {
		select {
		case <-ctx.Done():
			slog.Info("websocket consumer: shutting down")
			return nil
		default:
		}

		msg, err := c.consumer.ReadMessage(100 * time.Millisecond)
		if err != nil {
			if kafkaErr, ok := err.(kafka.Error); ok && kafkaErr.Code() == kafka.ErrTimedOut {
				continue
			}
			slog.Error("websocket consumer: read error", "error", err)
			continue
		}

		if err := c.handleMessage(ctx, msg.Value); err != nil {
			slog.Error("websocket consumer: handle message failed", "error", err)
		}

		c.consumer.CommitMessage(msg)
	}
}

// handleMessage processes a Kafka message and broadcasts it via WebSocket
func (c *Consumer) handleMessage(ctx context.Context, value []byte) error {
	var event WSEvent
	if err := json.Unmarshal(value, &event); err != nil {
		slog.Error("failed to unmarshal websocket event", "error", err)
		return nil // skip malformed messages
	}

	// Broadcast the event based on its type
	switch event.Type {
	case "audit_event":
		// Already in the correct format, broadcast directly
		c.broadcaster.hub.Broadcast(value)

	case "case_status_changed":
		// Already in the correct format, broadcast directly
		c.broadcaster.hub.Broadcast(value)

	case "metric_update":
		// Trigger a fresh metric fetch and broadcast
		c.broadcaster.MetricUpdate()

	case "outage_detected":
		// Already in the correct format, broadcast directly
		c.broadcaster.hub.Broadcast(value)

	default:
		slog.Warn("unknown websocket event type", "type", event.Type)
	}

	return nil
}

// WSEvent represents a generic event structure for routing
type WSEvent struct {
	Type string `json:"type"`
}
