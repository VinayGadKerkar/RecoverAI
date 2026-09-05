package consumers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/confluentinc/confluent-kafka-go/v2/kafka"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
)

// ResultProcessor consumes from "recovery.results" and finalizes recovery cases.
type ResultProcessor struct {
	db  *pgxpool.Pool
	cfg *config.Config
}

func NewResultProcessor(db *pgxpool.Pool, cfg *config.Config) *ResultProcessor {
	return &ResultProcessor{db: db, cfg: cfg}
}

// Run starts the Kafka consumer loop for "recovery.results".
func (rp *ResultProcessor) Run(ctx context.Context) error {
	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers":    rp.cfg.KafkaBrokers,
		"group.id":             "result-processor-group",
		"auto.offset.reset":    "earliest",
		"enable.auto.commit":   false,
		"max.poll.interval.ms": 300000,
	})
	if err != nil {
		return fmt.Errorf("create kafka consumer: %w", err)
	}
	defer consumer.Close()

	if err := consumer.Subscribe("recovery.results", nil); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}

	slog.Info("result processor: started", "topic", "recovery.results")

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msg, err := consumer.ReadMessage(100 * time.Millisecond)
		if err != nil {
			if kafkaErr, ok := err.(kafka.Error); ok && kafkaErr.Code() == kafka.ErrTimedOut {
				continue
			}
			slog.Error("result processor: read error", "error", err)
			continue
		}

		if err := rp.processResult(ctx, msg.Value); err != nil {
			slog.Error("result processor: process failed", "error", err)
		}

		consumer.CommitMessage(msg)
	}
}

// processResult finalizes a recovery case based on execution outcome.
func (rp *ResultProcessor) processResult(ctx context.Context, payload []byte) error {
	var result RecoveryResultMessage
	if err := json.Unmarshal(payload, &result); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	slog.Info("result processor: processing result",
		"case_id", result.CaseID,
		"status", result.Status,
		"amount_recovered", result.AmountRecovered,
	)

	// Load current case state
	var revenueAtRisk int64
	var currentStatus string
	err := rp.db.QueryRow(ctx, `
		SELECT revenue_at_risk, status FROM recovery_cases WHERE id = $1
	`, result.CaseID).Scan(&revenueAtRisk, &currentStatus)
	if err != nil {
		return fmt.Errorf("load case: %w", err)
	}

	// Determine final status based on outcome
	var finalStatus string
	if result.Status == "success" && result.AmountRecovered > 0 {
		if result.AmountRecovered >= revenueAtRisk {
			finalStatus = "recovered"
		} else {
			finalStatus = "partially_recovered"
		}
	} else if result.Status == "success" && result.AmountRecovered == 0 {
		// Payment link generated or action taken, but no money captured yet
		finalStatus = "awaiting_payment"
	} else if result.Status == "blocked" {
		finalStatus = "stopped"
	} else if result.Status == "requires_human" {
		finalStatus = "pending_human_approval"
	} else if result.Status == "failed" {
		finalStatus = "failed"
	}

	// Update recovery_cases
	_, err = rp.db.Exec(ctx, `
		UPDATE recovery_cases
		SET status = $1,
		    amount_recovered = $2,
		    partial_recovery = $3,
		    resolved_at = NOW(),
		    updated_at = NOW()
		WHERE id = $4
	`, finalStatus, result.AmountRecovered, result.PartialRecovery, result.CaseID)
	if err != nil {
		return fmt.Errorf("update case: %w", err)
	}

	// Update customer lifetime_value if payment was recovered
	if result.AmountRecovered > 0 {
		rp.db.Exec(ctx, `
			UPDATE customers
			SET lifetime_value = lifetime_value + $1,
			    successful_payments = successful_payments + 1,
			    updated_at = NOW()
			WHERE id = (SELECT customer_id FROM recovery_cases WHERE id = $2)
		`, result.AmountRecovered, result.CaseID)
	}

	// Final audit log
	rp.auditLog(ctx, result.CaseID, "finalized", fmt.Sprintf("status=%s, recovered=₹%.2f", finalStatus, float64(result.AmountRecovered)/100))

	slog.Info("result processor: case finalized",
		"case_id", result.CaseID,
		"final_status", finalStatus,
		"amount_recovered", result.AmountRecovered,
	)

	return nil
}

func (rp *ResultProcessor) auditLog(ctx context.Context, caseID, action, reason string) {
	metadata, _ := json.Marshal(map[string]interface{}{
		"reason":     reason,
		"action":     action,
		"timestamp":  time.Now().Format(time.RFC3339),
		"service":    "result_processor",
	})
	rp.db.Exec(ctx, `
		INSERT INTO audit_logs (entity_type, entity_id, actor, action, metadata)
		VALUES ('recovery_case', $1, 'result_processor', $2, $3)
	`, caseID, action, metadata)
}
