"""Output schemas for the AI Recovery Service."""

from datetime import datetime
from typing import Optional
from pydantic import BaseModel, Field


class RecoveryCommand(BaseModel):
    """
    Structured JSON command produced by the AI.
    This is ALL the AI produces — it NEVER executes financial operations.
    The Go Policy Engine decides whether and how to execute this command.
    """
    payment_id: str
    recommended_action: str = Field(
        description="retry | payment_link | notify_customer | escalate | abort | wait"
    )
    wait_minutes: int = Field(default=0, ge=0, description="Minutes to wait before action (for wait/retry actions)")
    rationale: str = Field(description="Human-readable explanation of the recommendation")
    confidence: float = Field(ge=0.0, le=1.0, description="AI confidence in the recommendation")
    alternate_action: Optional[str] = Field(default=None, description="Fallback action if primary fails")
    notify_customer: bool = Field(default=False)
    message_template: Optional[str] = Field(default=None, description="Customer notification message template")
    requires_approval: bool = Field(default=False, description="Flag high-risk commands for human review")
    diagnosis: str = Field(description="Root cause diagnosis of the payment failure")
    generated_at: datetime = Field(default_factory=datetime.utcnow)
