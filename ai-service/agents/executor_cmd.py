"""
Executor Command Agent
Serialises the strategy dict into a validated RecoveryCommand.
This is the final step of the AI pipeline — it produces a command JSON only.
It NEVER calls any external API or executes any financial operation.
"""

from datetime import datetime

from schemas.input import RecoveryRequest
from schemas.output import RecoveryCommand

# Valid action values — must match Go models.RecoveryAction constants
VALID_ACTIONS = {"retry", "payment_link", "notify_customer", "escalate", "abort", "wait"}


def build_command(request: RecoveryRequest, diagnosis: str, strategy: dict) -> RecoveryCommand:
    """
    Validates the strategy dict and returns a RecoveryCommand.
    Falls back to a safe default if any field is missing or invalid.
    """
    action = strategy.get("recommended_action", "notify_customer")
    if action not in VALID_ACTIONS:
        action = "notify_customer"

    alternate = strategy.get("alternate_action")
    if alternate and alternate not in VALID_ACTIONS:
        alternate = None

    confidence = float(strategy.get("confidence", 0.5))
    confidence = max(0.0, min(1.0, confidence))  # clamp to [0, 1]

    wait_minutes = int(strategy.get("wait_minutes", 0))
    requires_approval = bool(strategy.get("requires_approval", False))

    # Always require approval for high-value payments (₹50,000+)
    if request.amount >= 5_000_000:
        requires_approval = True

    # Always require approval for YG error code
    if request.error_code == "YG":
        requires_approval = True
        action = "escalate"

    return RecoveryCommand(
        payment_id=request.payment_id,
        recommended_action=action,
        wait_minutes=wait_minutes,
        rationale=strategy.get("rationale", "AI-generated recommendation"),
        confidence=confidence,
        alternate_action=alternate,
        notify_customer=bool(strategy.get("notify_customer", False)),
        message_template=strategy.get("message_template"),
        requires_approval=requires_approval,
        diagnosis=diagnosis,
        generated_at=datetime.utcnow(),
    )
