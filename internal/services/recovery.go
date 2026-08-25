package services

// RecoveryService is intentionally empty.
//
// The five-stage recovery pipeline is implemented directly in the Kafka consumers:
//
//   Stage 1 — Webhook ingestion:    internal/handlers/webhook.go
//   Stage 2 — Risk Engine:          internal/consumers/risk_processor.go
//   Stage 3 — Pre-Recovery Validator: internal/validator/pre_recovery.go
//   Stage 4 — AI Recovery Service:  internal/services/ai_client.go  ← AIClient
//   Stage 5 — Policy + Execution:   internal/consumers/execution_worker.go
//                                   internal/policy/engine.go
//
// The AIClient in this package is the only cross-cutting service used by
// multiple consumers. All other orchestration lives in the consumer layer.
