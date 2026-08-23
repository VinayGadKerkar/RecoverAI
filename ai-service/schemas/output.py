"""Output schemas for the AI Recovery Service."""

from typing import Optional, Literal
from pydantic import BaseModel, Field


class RiskAssessment(BaseModel):
    """Output of Agent 1 — Risk Analyst."""
    revenue_at_risk_paise: int
    recovery_probability: float = Field(ge=0.0, le=1.0)
    failure_category: Literal["TD", "BD"]
    failure_type: Literal[
        "transient",
        "insufficient_funds",
        "velocity_limit",
        "limit_exceeded",
        "risk_blocked",
        "non_retryable_auto",
        "unknown"
    ]
    timing_penalty_applied: bool
    priority: Literal["low", "medium", "high", "critical"]
    reasoning: str = Field(max_length=120)


class RecoveryStrategy(BaseModel):
    """Output of Agent 2 — Recovery Strategist."""
    strategy: Literal[
        "retry_payment",
        "generate_payment_link",
        "notify_customer",
        "schedule_retry",
        "escalate_to_merchant",
        "stop_recovery"
    ]
    confidence: float = Field(ge=0.0, le=1.0)
    delay_minutes: int = Field(ge=0)
    recovery_window_reason: str = Field(max_length=100)
    message_template: Optional[str] = None
    reasoning: str = Field(max_length=150)


class ExecutorCommand(BaseModel):
    """
    Output of Agent 3 — Executor Command Builder.
    This is the final output sent to the Go Policy Engine.
    The AI NEVER executes these commands — only produces structured JSON.
    """
    action: Literal[
        "RETRY_PAYMENT",
        "GENERATE_PAYMENT_LINK",
        "SEND_NOTIFICATION",
        "ESCALATE",
        "STOP"
    ]
    payment_id: str
    case_id: str
    scheduled_at_minutes: int = Field(ge=0, description="Delay before execution")
    parameters: dict = Field(description="Action-specific parameters")
    
    # Metadata for audit
    risk_assessment_summary: Optional[dict] = None
    strategy_summary: Optional[dict] = None
