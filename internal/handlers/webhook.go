package handlers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	"recoverai/internal/kafka"
	"recoverai/internal/models"
	"recoverai/internal/redis"
)

// WebhookHandler handles POST /webhooks/razorpay
type WebhookHandler struct {
	db       *pgxpool.Pool
	redis    *redis.Client
	cfg      *config.Config
	producer *kafka.Producer
}

func NewWebhookHandler(db *pgxpool.Pool, redisClient *redis.Client, cfg *config.Config) *WebhookHandler {
	producer, err := kafka.NewProducer(cfg.KafkaBrokers)
	if err != nil {
		slog.Error("webhook handler: failed to create kafka producer", "error", err)
	}
	return &WebhookHandler{db: db, redis: redisClient, cfg: cfg, producer: producer}
}

func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// 1. Read raw body (needed for HMAC verification)
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MB limit
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// 2. HMAC-SHA256 signature verification
	if !h.verifySignature(body, r.Header.Get("X-Razorpay-Signature")) {
		slog.Warn("webhook: invalid signature", "remote_addr", r.RemoteAddr)
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// 3. Parse payload
	var payload models.RazorpayWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		http.Error(w, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	// 4. Idempotency check — deduplicate by event ID via Redis
	eventID := r.Header.Get("X-Razorpay-Event-Id")
	if eventID == "" {
		// Fall back to a content hash if header absent
		h := sha256.Sum256(body)
		eventID = hex.EncodeToString(h[:])
	}

	isDuplicate, err := h.redis.SetNXWithTTL(r.Context(), "webhook:seen:"+eventID, "1", 24*time.Hour)
	if err != nil {
		slog.Error("webhook: redis idempotency check failed", "error", err)
		// Continue processing — fail open to avoid dropping real events
	}
	if isDuplicate {
		slog.Info("webhook: duplicate event skipped", "event_id", eventID)
		w.WriteHeader(http.StatusOK)
		return
	}

	// 5. Persist raw event to webhook_events for audit
	if err := h.persistEvent(r.Context(), eventID, &payload, body); err != nil {
		slog.Error("webhook: failed to persist event", "error", err, "event_id", eventID)
		// Non-fatal — still publish to Kafka
	}

	// 6. Build and publish Kafka event
	if payload.Payload.Payment != nil {
		kafkaEvent := h.buildKafkaEvent(eventID, &payload)
		if err := h.producer.PublishPaymentEvent(r.Context(), kafkaEvent); err != nil {
			slog.Error("webhook: failed to publish kafka event", "error", err, "payment_id", kafkaEvent.PaymentID)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		slog.Info("webhook: event published", "event_id", eventID, "payment_id", kafkaEvent.PaymentID, "event_type", payload.Event)
	}

	// 7. Acknowledge immediately — Razorpay retries on non-2xx
	w.WriteHeader(http.StatusOK)
}

func (h *WebhookHandler) verifySignature(body []byte, signature string) bool {
	if h.cfg.RazorpayWebhookSecret == "" {
		slog.Warn("webhook: RAZORPAY_WEBHOOK_SECRET not set, skipping verification")
		return true
	}
	mac := hmac.New(sha256.New, []byte(h.cfg.RazorpayWebhookSecret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}

func (h *WebhookHandler) persistEvent(ctx interface{ Done() <-chan struct{} }, eventID string, payload *models.RazorpayWebhookPayload, raw []byte) error {
	// TODO: use sqlc generated queries
	return nil
}

func (h *WebhookHandler) buildKafkaEvent(eventID string, payload *models.RazorpayWebhookPayload) *models.KafkaPaymentEvent {
	p := payload.Payload.Payment.Entity
	return &models.KafkaPaymentEvent{
		EventID:    eventID,
		EventType:  payload.Event,
		PaymentID:  p.ID,
		MerchantID: p.MerchantID,
		Amount:     p.Amount,
		Currency:   p.Currency,
		Status:     p.Status,
		Method:     p.Method,
		ErrorCode:  p.ErrorCode,
		Bank:       p.Bank,
		VPA:        p.VPA,
		ReceivedAt: time.Now(),
	}
}
