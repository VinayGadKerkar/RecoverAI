"""Input schemas for the AI Recovery Service."""

from datetime import datetime
from typing import Optional
from pydantic import BaseModel, Field


class RiskScoreInput(BaseModel):
    payment_id: str
    score: float = Field(ge=0.0, le=1.0, description="Risk score 0–1")
    level: str = Field(description="low | medium | high | critical")
    recovery_chance: float = Field(ge=0.0, le=1.0)
    upi_error_code: Optional[str] = None
    bank_outage: bool = False
    outage_bank: Optional[str] = None
    factors: list[str] = Field(default_factory=list)
    scored_at: datetime


class RecoveryRequest(BaseModel):
    """
    Input to the AI Recovery Service — produced by the Go Risk Engine
    after Stage 2 scoring and Stage 3 validation.
    """
    event_id: str
    payment_id: str
    merchant_id: str
    amount: int = Field(description="Payment amount in paise")
    currency: str = Field(default="INR")
    error_code: Optional[str] = None
    bank: Optional[str] = None
    vpa: Optional[str] = None
    method: Optional[str] = None
    risk_score: RiskScoreInput
    received_at: datetime

    @property
    def amount_inr(self) -> float:
        return self.amount / 100
