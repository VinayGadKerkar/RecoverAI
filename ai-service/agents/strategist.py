"""
Strategist Agent
Selects the optimal recovery action given a diagnosis and payment context.
Outputs a structured strategy dict that executor_cmd serialises into a RecoveryCommand.
"""

import json
from langchain_core.language_models import BaseChatModel
from langchain_core.prompts import ChatPromptTemplate
from langchain_core.output_parsers import JsonOutputParser

from schemas.input import RecoveryRequest


SYSTEM_PROMPT = """You are a payment recovery strategist for an Indian fintech platform.
Given a diagnosis of a failed payment, choose the best recovery action.

Available actions and when to use them:
- retry:           Transient error (outage, timeout). Retry after a cooldown. Max 3 attempts.
- payment_link:    Customer has a different payment method or insufficient UPI balance (U16).
- notify_customer: Inform customer to retry manually. Use for U30 (try again later).
- escalate:        Human review needed. Use for YG, Z9, very high amounts, or low confidence.
- abort:           Unrecoverable. Card blocked, fraud flag, RBI non-compliant.
- wait:            Bank outage in progress. Wait for resolution before acting.

RBI Compliance Rules (HARD — never violate):
- Never retry a payment older than 24 hours.
- Never auto-retry more than 3 times for the same payment.
- Payments flagged YG always require human approval.
- High-value payments (> ₹50,000) always require human approval.

Output valid JSON only. No explanation outside the JSON block.

JSON schema:
{
  "recommended_action": "<action>",
  "wait_minutes": <int, 0 if not waiting>,
  "confidence": <float 0.0-1.0>,
  "rationale": "<1-2 sentence explanation>",
  "alternate_action": "<action or null>",
  "notify_customer": <bool>,
  "message_template": "<short SMS/email message or null>",
  "requires_approval": <bool>
}
"""

USER_TEMPLATE = """Payment ID: {payment_id}
Amount: ₹{amount_inr:.2f}
UPI Error Code: {error_code}
Bank: {bank}
Bank Outage: {bank_outage}
Recovery Chance: {recovery_chance:.0%}
Previous Attempts: {attempt_count}

Diagnosis:
{diagnosis}

Choose the best recovery action and return JSON."""


async def run_strategist(
    request: RecoveryRequest,
    diagnosis: str,
    attempt_count: int,
    llm: BaseChatModel,
) -> dict:
    """
    Runs the Strategist agent and returns a strategy dict.
    """
    prompt = ChatPromptTemplate.from_messages([
        ("system", SYSTEM_PROMPT),
        ("human", USER_TEMPLATE),
    ])

    chain = prompt | llm | JsonOutputParser()

    strategy = await chain.ainvoke({
        "payment_id": request.payment_id,
        "amount_inr": request.amount_inr,
        "error_code": request.error_code or "unknown",
        "bank": request.bank or "unknown",
        "bank_outage": request.risk_score.bank_outage,
        "recovery_chance": request.risk_score.recovery_chance,
        "attempt_count": attempt_count,
        "diagnosis": diagnosis,
    })

    return strategy
