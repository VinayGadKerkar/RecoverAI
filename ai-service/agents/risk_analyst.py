"""
Agent 1: Risk Analyst
Analyzes a failed payment and produces a structured risk assessment.
"""

import json
from langchain_core.prompts import ChatPromptTemplate
from langchain_core.output_parsers import JsonOutputParser

from schemas.output import RiskAssessment


SYSTEM_PROMPT = """You are a payment failure analyst for an Indian fintech system. You will receive data about a failed UPI or card payment. Respond ONLY with a valid JSON object matching the schema exactly. Never add text outside the JSON.

UPI errors U30, U28, RB, BT are Technical Declines — temporary, retryable.
UPI errors U16, Z9, Z8, Z7, U68, YG, U69 are Business Declines — need different strategies.

YG (risk threshold) must always result in priority=critical and low recovery_probability.
Z9, Z8 must always result in failure_type=non_retryable_auto.

Timing matters: failures between 19:00-22:00 IST have 30% lower recovery rate — factor this in.

Output JSON schema:
{
  "revenue_at_risk_paise": <int>,
  "recovery_probability": <float 0.0-1.0>,
  "failure_category": "TD|BD",
  "failure_type": "transient|insufficient_funds|velocity_limit|limit_exceeded|risk_blocked|non_retryable_auto|unknown",
  "timing_penalty_applied": <bool>,
  "priority": "low|medium|high|critical",
  "reasoning": "<max 120 chars>"
}"""

USER_TEMPLATE = """Payment ID: {payment_id}
Amount: ₹{amount_inr:.2f} ({amount_paise} paise)
UPI Error Code: {upi_error_code}
UPI Error Category: {upi_error_category}
Failure Type: {failure_type}
Time of Failure: {time_of_failure_hour}:00 IST
Force Payment Link: {force_payment_link}

Customer History:
- Successful Payments: {successful_payments}
- Failed Payments: {failed_payments}
- Lifetime Value: ₹{lifetime_value_inr:.2f}
- Days Since Last Success: {days_since_last_success}

Risk Score: {risk_score:.3f}
Priority: {priority}

Analyze this payment failure and return structured JSON only."""


async def run_risk_analyst(llm, request) -> RiskAssessment:
    """
    Runs the Risk Analyst agent and returns a RiskAssessment.
    Retries once if output doesn't match schema.
    """
    prompt = ChatPromptTemplate.from_messages([
        ("system", SYSTEM_PROMPT),
        ("human", USER_TEMPLATE),
    ])

    chain = prompt | llm | JsonOutputParser()

    for attempt in range(2):
        try:
            result = await chain.ainvoke({
                "payment_id": request.payment_id,
                "amount_paise": request.amount_paise,
                "amount_inr": request.amount_paise / 100,
                "upi_error_code": request.upi_error_code,
                "upi_error_category": request.upi_error_category,
                "failure_type": request.failure_type,
                "time_of_failure_hour": request.time_of_failure_hour,
                "force_payment_link": request.force_payment_link,
                "successful_payments": request.customer_history.successful_payments,
                "failed_payments": request.customer_history.failed_payments,
                "lifetime_value_inr": request.customer_history.lifetime_value_paise / 100,
                "days_since_last_success": request.customer_history.days_since_last_success or "N/A",
                "risk_score": request.risk_score,
                "priority": request.priority,
            })

            # Validate against Pydantic schema
            return RiskAssessment(**result)

        except Exception as e:
            if attempt == 0:
                # Retry once
                continue
            # Second failure — return safe default
            return RiskAssessment(
                revenue_at_risk_paise=request.amount_paise,
                recovery_probability=0.3,
                failure_category="BD",
                failure_type="unknown",
                timing_penalty_applied=False,
                priority="medium",
                reasoning="AI output validation failed, using safe defaults"
            )
