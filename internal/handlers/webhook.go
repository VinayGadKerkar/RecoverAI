package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"recoverai/internal/config"
	"recoverai/internal/kafka"
	"recoverai/internal/redis"
)

// ─── Razorpay Webhook Payload ─────────────────────────────────────────────────

// RazorpayWebhookPayload is the root structure of every Razorpay webhook event.
// Reference: https://razorpay.com/docs/webhooks/payloads/
type RazorpayWebhookPayload struct {
	Entity    string               `json:"entity"`     // always "event"
	AccountID string               `json:"account_id"` // merchant's Razorpay account ID
	Event     string               `json:"event"`      // e.g. "payment.failed", "payment.captured"
	Contains  []string             `json:"contains"`   // e.g. ["payment"]
	Payload   RazorpayEventPayload `json:"payload"`
	CreatedAt int64                `json:"created_at"` // Unix timestamp
}

type RazorpayEventPayload struct {
	Payment *RazorpayPaymentEntity `json:"payment,omitempty"`
	Order   *RazorpayOrderEntity   `json:"order,omitempty"`
}

type RazorpayPaymentEntity struct {
	Entity RazorpayPayment `json:"entity"`
}

type RazorpayPayment struct {
	ID               string  `json:"id"`
	Amount           int64   `json:"amount"` // paise
	Currency         string  `json:"currency"`
	Status           string  `json:"status"` // created | authorized | captured | failed | refunded
	OrderID          string  `json:"order_id"`
	Method           string  `json:"method"` // upi | card | netbanking | wallet
	Description      string  `json:"description"`
	Email            string  `json:"email"`
	Contact          string  `json:"contact"`
	VPA              string  `json:"vpa"`
	Bank             string  `json:"bank"`
	CardID           string  `json:"card_id"`
	ErrorCode        *string `json:"error_code"`        // present only on failure
	ErrorDescription *string `json:"error_description"`
	ErrorSource      *string `json:"error_source"`
	ErrorStep        *string `json:"error_step"`
	ErrorReason      *string `json:"error_reason"`
	CreatedAt        int64   `json:"created_at"`
}

type RazorpayOrderEntity struct {
	Entity RazorpayOrder `json:"entity"`
}

type RazorpayOrder struct {
	ID       string `json:"id"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Receipt  string `json:"receipt"`
	Status   string `json:"status"`
}

// ─── Kafka Event ──────────────────────────────────────────────────────────────

// KafkaPaymentEvent is published to "payment.events" after webhook ingestion.
type KafkaPaymentEvent struct {
	EventID          string                 `json:"event_id"`
	EventType        string                 `json:"event_type"`
	RazorpayEventID  string                 `json:"razorpay_event_id"`
	PaymentID        string                 `json:"payment_id"`
	OrderID          string                 `json:"order_id,omitempty"`
	Amount           int64                  `json:"amount"`
	Currency         string                 `json:"currency"`
	Status           string                 `json:"status"`
	Method           string                 `json:"method"`
	ErrorCode        string                 `json:"error_code,omitempty"`
	ErrorDescription string                 `json:"error_description,omitempty"`
	Bank             string                 `json:"bank,omitempty"`
	VPA              string                 `json:"vpa,omitempty"`
	Email            string                 `json:"email,omitempty"`
	Contact          string                 `json:"contact,omitempty"`
	RawPayload       map[string]interface{} `json:"raw_payload"` // full webhook body for audit
	ReceivedAt       time.Time              `json:"received_at"`
}

// ─── Handler ──────────────────────────────────────────────────────────────────

// WebhookHandler handles POST /webhooks/razorpay.
// CRITICAL: Razorpay uses at-least-once delivery. The same event can arrive multiple times.
// CRITICAL: Webhooks can arrive OUT OF ORDER (payment.captured before payment.failed).
// CRITICAL: payment.failed then payment.captured for same payment_id is valid (manual retry).
// CRITICAL: Must respond with 200 within 5 seconds or Razorpay retries exponentially.
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
		// Allow handler to be created anyway — requests will fail loudly if producer is nil
	}
	return &WebhookHandler{
		db:       db,
		redis:    redisClient,
		cfg:      cfg,
		producer: producer,
	}
}

func (h *WebhookHandler) Handle(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	// ─── Step 1: Read raw body ────────────────────────────────────────────────
	// Must preserve raw bytes for HMAC verification — never use parsed body for signature check.
	body, err := io.ReadAll(io.LimitReader(r.Body, 2<<20)) // 2 MB limit
	if err != nil {
		slog.Warn("webhook: failed to read body", "error", err, "remote_addr", r.RemoteAddr)
		http.Error(w, `{"error":"failed to read body"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// ─── Step 2: HMAC-SHA256 signature verification ───────────────────────────
	// Razorpay signs the raw body with your webhook secret.
	signature := r.Header.Get("X-Razorpay-Signature")
	if !h.verifySignature(body, signature) {
		slog.Warn("webhook: invalid signature",
			"remote_addr", r.RemoteAddr,
			"content_length", len(body),
		)
		http.Error(w, `{"error":"invalid signature"}`, http.StatusUnauthorized)
		return
	}

	// ─── Step 3: Idempotency check (Redis SETNX) ──────────────────────────────
	// Razorpay sends X-Razorpay-Event-Id for deduplication.
	// Use Redis SETNX with 24h TTL — if key exists, event already processed.
	razorpayEventID := r.Header.Get("X-Razorpay-Event-Id")
	if razorpayEventID == "" {
		// Fallback: hash the body (should never happen with real Razorpay events)
		h := sha256.Sum256(body)
		razorpayEventID = "hash_" + hex.EncodeToString(h[:16])
		slog.Warn("webhook: missing X-Razorpay-Event-Id header, using body hash", "event_id", razorpayEventID)
	}

	idempotencyKey := "webhook:idempotency:" + razorpayEventID
	alreadyProcessed, err := h.redis.SetNXWithTTL(r.Context(), idempotencyKey, "1", 24*time.Hour)
	if err != nil {
		slog.Error("webhook: redis SETNX failed", "error", err, "event_id", razorpayEventID)
		// Fail OPEN — process the event anyway to avoid dropping real payments.
		// Risk: duplicate processing if Redis is down. Downstream consumers must be idempotent.
	} else if alreadyProcessed {
		// Key already existed — event was processed before.
		slog.Info("webhook: duplicate event (already processed)", "event_id", razorpayEventID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","duplicate":true}`))
		return
	}

	// ─── Step 4: Parse payload ────────────────────────────────────────────────
	var payload RazorpayWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		slog.Warn("webhook: invalid JSON payload", "error", err, "event_id", razorpayEventID)
		http.Error(w, `{"error":"invalid JSON payload"}`, http.StatusBadRequest)
		return
	}

	// Only process supported event types
	if !h.isSupportedEvent(payload.Event) {
		slog.Info("webhook: unsupported event type (ignoring)", "event_type", payload.Event, "event_id", razorpayEventID)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok","ignored":true}`))
		return
	}

	// ─── Step 5: Async persist to webhook_events table ───────────────────────
	// Non-blocking goroutine — never let DB slowness delay the 200 OK response.
	go h.persistEventAsync(razorpayEventID, payload, body)

	// ─── Step 6: Publish to Kafka (synchronous — must succeed for 200) ───────
	// This is the critical path. Kafka failure means payment is lost.
	if payload.Payload.Payment != nil {
		kafkaEvent := h.buildKafkaEvent(razorpayEventID, payload)
		if h.producer == nil {
			slog.Error("webhook: kafka producer not initialized", "event_id", razorpayEventID)
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}

		// Publish with a 3-second timeout (Razorpay expects 200 within 5s total)
		publishCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
		defer cancel()

		if err := h.producer.PublishPaymentEvent(publishCtx, kafkaEvent); err != nil {
			slog.Error("webhook: kafka publish failed",
				"error", err,
				"event_id", razorpayEventID,
				"payment_id", kafkaEvent.PaymentID,
			)
			http.Error(w, `{"error":"failed to publish event"}`, http.StatusInternalServerError)
			return
		}

		slog.Info("webhook: event processed",
			"event_id", razorpayEventID,
			"event_type", payload.Event,
			"payment_id", kafkaEvent.PaymentID,
			"status", kafkaEvent.Status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}

	// ─── Step 7: Return 200 immediately ───────────────────────────────────────
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

// verifySignature checks HMAC-SHA256(secret, body) == X-Razorpay-Signature.
// Returns true if signature is valid OR if webhook secret is not configured (dev mode).
func (h *WebhookHandler) verifySignature(body []byte, signature string) bool {
	secret := h.cfg.RazorpayWebhookSecret
	if secret == "" {
		slog.Warn("webhook: RAZORPAY_WEBHOOK_SECRET not set — skipping signature verification (INSECURE)")
		return true
	}

	if signature == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(expected), []byte(signature))
}

// isSupportedEvent filters events we care about.
// Supported: payment.failed, payment.authorized, payment.captured, order.paid
func (h *WebhookHandler) isSupportedEvent(eventType string) bool {
	switch eventType {
	case "payment.failed",
		"payment.authorized",
		"payment.captured",
		"order.paid":
		return true
	default:
		return false
	}
}

// persistEventAsync writes the webhook event to the webhook_events table.
// Runs in a background goroutine — failures are logged but do not block the HTTP response.
func (h *WebhookHandler) persistEventAsync(razorpayEventID string, payload RazorpayWebhookPayload, rawBody []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	payloadJSON, _ := json.Marshal(payload)

	_, err := h.db.Exec(ctx, `
		INSERT INTO webhook_events (razorpay_event_id, event_type, payload, processed, created_at)
		VALUES ($1, $2, $3, TRUE, NOW())
		ON CONFLICT (razorpay_event_id) DO NOTHING
	`, razorpayEventID, payload.Event, payloadJSON)

	if err != nil {
		slog.Error("webhook: failed to persist event to DB",
			"error", err,
			"event_id", razorpayEventID,
		)
	}
}

// buildKafkaEvent constructs a KafkaPaymentEvent from the Razorpay webhook payload.
func (h *WebhookHandler) buildKafkaEvent(razorpayEventID string, payload RazorpayWebhookPayload) *KafkaPaymentEvent {
	p := payload.Payload.Payment.Entity

	event := &KafkaPaymentEvent{
		EventID:         uuid.New().String(),
		RazorpayEventID: razorpayEventID,
		EventType:       payload.Event,
		PaymentID:       p.ID,
		OrderID:         p.OrderID,
		Amount:          p.Amount,
		Currency:        p.Currency,
		Status:          p.Status,
		Method:          p.Method,
		Bank:            p.Bank,
		VPA:             p.VPA,
		Email:           p.Email,
		Contact:         p.Contact,
		ReceivedAt:      time.Now(),
	}

	// Error fields are pointers — only set if present
	if p.ErrorCode != nil {
		event.ErrorCode = *p.ErrorCode
	}
	if p.ErrorDescription != nil {
		event.ErrorDescription = *p.ErrorDescription
	}

	// Embed full payload as JSON for audit trail
	var rawMap map[string]interface{}
	json.Unmarshal([]byte(payload.Event), &rawMap)
	event.RawPayload = rawMap

	return event
}
