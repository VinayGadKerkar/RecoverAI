"""Input schemas for the AI Recovery Service."""

from typing import Optional
from pydantic import BaseModel, Field


class CustomerHistory(BaseModel):
    successful_payments: int
    failed_payments: int
    lifetime_value_paise: int
    last_successful_payment_at: Optional[str] = None
    days_since_last_success: Optional[int] = None


class MerchantPolicy(BaseModel):
    max_retry_amount_paise: int
    max_retries: int
    retry_cooldown_minutes: int
    require_human_above_paise: int
    allowed_actions: list[str]


class AnalyzeRequest(BaseModel):
    """Complete input to the AI service — sent by Go validator consumer."""
    payment_id: str
    case_id: str
    amount_paise: int
    upi_error_code: str
    upi_error_category: str  # "TD" | "BD" | "unknown"
    failure_type: str
    failure_reason: str
    time_of_failure_hour: int = Field(ge=0, le=23, description="IST hour (0-23)")
    force_payment_link: bool = Field(description="Set by validator for YG/Z8 errors")
    customer_history: CustomerHistory
    risk_score: float
    priority: str
    merchant_policy: MerchantPolicy
