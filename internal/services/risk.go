package services

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/models"
	"recoverai/internal/outage"
	redisclient "recoverai/internal/redis"
)

// RiskService implements Stage 2: scoring a payment failure and detecting outages.
type RiskService struct {
	db      *pgxpool.Pool
	redis   *redisclient.Client
	outage  *outage.Detector
	producer interface{ PublishRiskScoredEvent(context.Context, *models.KafkaRiskScoredEvent) error }
}

func NewRiskService(db *pgxpool.Pool, r *redisclient.Client, od *outage.Detector) *RiskService {
	return &RiskService{db: db, redis: r, outage: od}
}

// SetProducer injects the Kafka producer (avoids circular dependency at construction).
func (s *RiskService) SetProducer(p interface{ PublishRiskScoredEvent(context.Context, *models.KafkaRiskScoredEvent) error }) {
	s.producer = p
}

// Score processes a KafkaPaymentEvent, computes a RiskScore, persists it,
// records the bank failure, and publishes a KafkaRiskScoredEvent.
func (s *RiskService) Score(ctx context.Context, event *models.KafkaPaymentEvent) error {
	// Only score failed payments
	if event.Status != models.PaymentStatusFailed {
		return nil
	}

	// Record bank failure for outage detection
	if event.Bank != "" {
		if err := s.outage.RecordFailure(ctx, event.Bank); err != nil {
			slog.Warn("risk: failed to record bank failure", "bank", event.Bank, "error", err)
		}
	}

	// Check for active bank outage
	bankOutage, err := s.outage.IsInOutage(ctx, event.Bank)
	if err != nil {
		slog.Warn("risk: outage check error", "bank", event.Bank, "error", err)
	}

	score := s.computeScore(event, bankOutage)

	// Persist risk score
	_, err = s.db.Exec(ctx, `
		INSERT INTO risk_scores (payment_id, score, level, recovery_chance, upi_error_code, bank_outage, outage_bank, factors)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`,
		event.PaymentID,
		score.Score,
		string(score.Level),
		score.RecoveryChance,
		string(score.UPIErrorCode),
		score.BankOutage,
		score.OutageBank,
		score.Factors,
	)
	if err != nil {
		return fmt.Errorf("persist risk score: %w", err)
	}

	slog.Info("risk scored",
		"payment_id", event.PaymentID,
		"score", score.Score,
		"level", score.Level,
		"recovery_chance", score.RecoveryChance,
		"bank_outage", bankOutage,
	)

	// Publish scored event downstream
	if s.producer != nil {
		scoredEvent := &models.KafkaRiskScoredEvent{
			EventID:    uuid.New().String(),
			PaymentID:  event.PaymentID,
			MerchantID: event.MerchantID,
			RiskScore:  *score,
			ScoredAt:   time.Now(),
		}
		if err := s.producer.PublishRiskScoredEvent(ctx, scoredEvent); err != nil {
			return fmt.Errorf("publish risk scored event: %w", err)
		}
	}

	return nil
}

// computeScore applies deterministic scoring rules based on UPI error codes
// and contextual signals (amount, bank outage, customer history).
func (s *RiskService) computeScore(event *models.KafkaPaymentEvent, bankOutage bool) *models.RiskScore {
	score := &models.RiskScore{
		PaymentID:    event.PaymentID,
		BankOutage:   bankOutage,
		ScoredAt:     time.Now(),
		Factors:      []string{},
	}

	if bankOutage {
		score.OutageBank = event.Bank
		score.Factors = append(score.Factors, fmt.Sprintf("bank_outage:%s", event.Bank))
	}

	// Base scoring from UPI error code taxonomy
	upiCode := models.UPIErrorCode(event.ErrorCode)
	score.UPIErrorCode = upiCode

	switch upiCode {
	case models.UPIErrorU16: // Insufficient balance — high confidence recovery via payment link
		score.Score = 0.75
		score.RecoveryChance = 0.70
		score.Level = models.RiskLevelMedium
		score.Factors = append(score.Factors, "upi_u16_insufficient_balance")

	case models.UPIErrorU30: // Payer account issue — medium, retry after cooldown
		score.Score = 0.55
		score.RecoveryChance = 0.55
		score.Level = models.RiskLevelMedium
		score.Factors = append(score.Factors, "upi_u30_payer_account_issue")

	case models.UPIErrorZ9: // Transaction declined — low recovery chance
		score.Score = 0.35
		score.RecoveryChance = 0.25
		score.Level = models.RiskLevelLow
		score.Factors = append(score.Factors, "upi_z9_declined")

	case models.UPIErrorU68: // Not permitted — alternate payment link needed
		score.Score = 0.60
		score.RecoveryChance = 0.60
		score.Level = models.RiskLevelMedium
		score.Factors = append(score.Factors, "upi_u68_not_permitted")

	case models.UPIErrorRB: // Bank blocked — high if outage, wait and retry
		score.Score = 0.80
		score.RecoveryChance = 0.75
		score.Level = models.RiskLevelHigh
		score.Factors = append(score.Factors, "upi_rb_bank_blocked")

	case models.UPIErrorYG: // Risk threshold — requires human review
		score.Score = 0.90
		score.RecoveryChance = 0.20
		score.Level = models.RiskLevelCritical
		score.Factors = append(score.Factors, "upi_yg_risk_threshold")

	default:
		score.Score = 0.50
		score.RecoveryChance = 0.40
		score.Level = models.RiskLevelMedium
		score.Factors = append(score.Factors, fmt.Sprintf("unknown_error_code:%s", event.ErrorCode))
	}

	// Boost recovery chance if bank outage detected (transient issue)
	if bankOutage {
		score.RecoveryChance = min(score.RecoveryChance+0.15, 0.95)
		score.Factors = append(score.Factors, "recovery_chance_boosted_by_outage")
	}

	// High-value payments get elevated risk level
	if event.Amount >= 5000000 { // ₹50,000
		score.Level = models.RiskLevelCritical
		score.Factors = append(score.Factors, "high_value_payment")
	}

	return score
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
