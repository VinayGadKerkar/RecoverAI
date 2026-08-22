package kafka

// Topic constants — single source of truth for all Kafka topic names.
const (
	// Stage 1 → Stage 2: Raw payment events from webhook ingestion.
	TopicPaymentEvents = "payment.events"

	// Stage 2 → Stage 3/4: Risk-scored payment events.
	TopicRiskScored = "payment.risk_scored"

	// Stage 3 → Stage 4: Payments that passed the pre-recovery validator.
	TopicValidatedForAI = "payment.validated_for_ai"

	// Stage 4 → Stage 5: AI commands awaiting policy review.
	TopicAICommands = "payment.ai_commands"

	// Stage 5 → Audit: Execution results.
	TopicExecutionResults = "payment.execution_results"

	// Dead-letter queue for unrecoverable events.
	TopicDeadLetter = "payment.dead_letter"
)
