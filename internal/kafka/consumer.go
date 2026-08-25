package kafka

// Consumer wraps the Confluent Kafka consumer.
// The actual pipeline consumers (Risk Engine, Validator, Execution Worker,
// Result Processor) live in internal/consumers/ and use the confluent-kafka-go
// library directly. This file is kept for the Consumer type definition used by
// integration tests and any future shared consumer utilities.

import (
	"fmt"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
)

// Consumer wraps a confluent kafka.Consumer.
type Consumer struct {
	c *kafka.Consumer
}

// NewConsumer creates a new Kafka consumer subscribed to the given topic.
func NewConsumer(brokers, topic, groupID string) (*Consumer, error) {
	c, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":     brokers,
		"group.id":              groupID,
		"auto.offset.reset":     "earliest",
		"enable.auto.commit":    false, // manual commit for at-least-once delivery
		"max.poll.interval.ms":  300000,
		"session.timeout.ms":    30000,
		"heartbeat.interval.ms": 3000,
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

// Close shuts down the consumer cleanly.
func (c *Consumer) Close() error {
	return c.c.Close()
}
