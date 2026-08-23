package kafka

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"

	"recoverai/internal/models"
)

// Producer wraps the Confluent Kafka producer with typed publish methods.
type Producer struct {
	p *kafka.Producer
}

// NewProducer creates a new Kafka producer connected to the given broker list.
func NewProducer(brokers string) (*Producer, error) {
	p, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers":  brokers,
		"acks":               "all",       // strongest durability guarantee
		"enable.idempotence": true,
		"retries":            5,
		"retry.backoff.ms":   200,
		"compression.type":   "snappy",
	})
	if err != nil {
		return nil, fmt.Errorf("create kafka producer: %w", err)
	}

	// Start delivery report goroutine
	go func() {
		for e := range p.Events() {
			switch ev := e.(type) {
			case *kafka.Message:
				if ev.TopicPartition.Error != nil {
					// TODO: structured log
					_ = ev.TopicPartition.Error
				}
			}
		}
	}()

	return &Producer{p: p}, nil
}

// PublishPaymentEvent publishes a KafkaPaymentEvent to TopicPaymentEvents.
func (p *Producer) PublishPaymentEvent(ctx context.Context, event *models.KafkaPaymentEvent) error {
	return p.publish(ctx, TopicPaymentEvents, event.PaymentID, event)
}

// PublishRiskScoredEvent publishes a KafkaRiskScoredEvent to TopicRiskScored.
func (p *Producer) PublishRiskScoredEvent(ctx context.Context, event *models.KafkaRiskScoredEvent) error {
	return p.publish(ctx, TopicRiskScored, event.PaymentID, event)
}

// PublishAICommand publishes an AICommand to TopicAICommands.
func (p *Producer) PublishAICommand(ctx context.Context, cmd *models.AICommand) error {
	return p.publish(ctx, TopicAICommands, cmd.PaymentID, cmd)
}

// Publish publishes an arbitrary payload to a topic with a given key.
// Use this for custom events not covered by the typed methods above.
func (p *Producer) Publish(ctx context.Context, topic, key string, payload []byte) error {
	deliveryChan := make(chan kafka.Event, 1)
	err := p.p.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            []byte(key),
		Value:          payload,
	}, deliveryChan)
	if err != nil {
		return fmt.Errorf("produce message: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case e := <-deliveryChan:
		m := e.(*kafka.Message)
		if m.TopicPartition.Error != nil {
			return fmt.Errorf("delivery failed: %w", m.TopicPartition.Error)
		}
	}
	return nil
}

// publish serialises v as JSON and produces it to the given topic.
// paymentID is used as the message key to ensure ordering within a partition.
func (p *Producer) publish(ctx context.Context, topic, key string, v any) error {
	payload, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	deliveryChan := make(chan kafka.Event, 1)
	err = p.p.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &topic, Partition: kafka.PartitionAny},
		Key:            []byte(key),
		Value:          payload,
	}, deliveryChan)
	if err != nil {
		return fmt.Errorf("produce message: %w", err)
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case e := <-deliveryChan:
		m := e.(*kafka.Message)
		if m.TopicPartition.Error != nil {
			return fmt.Errorf("delivery failed: %w", m.TopicPartition.Error)
		}
	}
	return nil
}

// Close flushes pending messages and shuts down the producer.
func (p *Producer) Close() {
	p.p.Flush(5000)
	p.p.Close()
}
