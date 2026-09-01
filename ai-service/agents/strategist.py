"""
Agent 2: Recovery Strategist
Selects the optimal recovery strategy given a risk assessment and merchant policy.
"""

import json
from langchain_core.prompts import ChatPromptTemplate
from langchain_core.output_parsers import JsonOutputParser

from schemas.output import RecoveryStrategy, RiskAssessment


SYSTEM_PROMPT = """You are a payment recovery strategist for an Indian fintech system. You will receive a risk assessment and merchant policy. Respond ONLY with a valid JSON object matching the schema exactly. Never add text outside the JSON.

Decision rules you must follow:
- If failure_type is 'risk_blocked' (YG): strategy must be escalate_to_merchant
- If failure_type is 'non_retryable_auto' (Z9, Z8): strategy must be generate_payment_link or notify_customer, never retry_payment
- If force_payment_link is true: strategy must NOT be retry_payment
- If recovery_probability < 0.3: strategy must be stop_recovery unless customer LTV > 500000 paise
- If time_of_failure_hour is between 19-22: add at least 480 minutes delay for retry (wait until morning)
- For insufficient_funds (U16, Z9): delay must be at least 1440 minutes (24 hours) — customer needs time to top up
- delay_minutes = 0 means retry immediately — only allowed for TD failures outside peak hours
- If amount > 5000000 paise (₹50K): strategy must be escalate_to_merchant or generate_payment_link, never auto-retry

Output JSON schema:
{{
  "strategy": "retry_payment|generate_payment_link|notify_customer|schedule_retry|escalate_to_merchant|stop_recovery",
  "confidence": <float 0.0-1.0>,
  "delay_minutes": <int>,
  "recovery_window_reason": "<max 100 chars>",
  "message_template": "<string or null>",
  "reasoning": "<detailed 3-4 sentence explanation covering why this action, key factors, expected outcome, and risks>",
  "key_factors": ["<factor 1>", "<factor 2>", "<factor 3>"]
}}

Example reasoning: "Recommended retry_payment because U30 error indicates temporary debit timeout with 73% historical recovery rate. Payment is recent (2 min old), first attempt, amount (₹999) below risk threshold. Bank APIs show normal status with no outages detected. Expected 65-75% success within 15 minutes."

VALIDATION: Before returning, check that strategy is in the allowed list. If not, default to generate_payment_link."""

USER_TEMPLATE = """Risk Assessment:
- Revenue at Risk: ₹{revenue_at_risk_inr:.2f}
- Recovery Probability: {recovery_probability:.1%}
- Failure Category: {failure_category}
- Failure Type: {failure_type}
- Timing Penalty Applied: {timing_penalty_applied}
- Priority: {priority}
- Reasoning: {reasoning}

Force Payment Link: {force_payment_link}
Time of Failure Hour: {time_of_failure_hour}:00 IST
Amount: ₹{amount_inr:.2f}
Customer LTV: ₹{customer_ltv_inr:.2f}

Merchant Policy:
- Max Retry Amount: ₹{max_retry_amount_inr:.2f}
- Max Retries: {max_retries}
- Retry Cooldown: {retry_cooldown_minutes} minutes
- Require Human Above: ₹{require_human_above_inr:.2f}
- Allowed Actions: {allowed_actions}

Choose the best recovery strategy and return JSON only."""


async def run_strategist(llm, request, risk_assessment: RiskAssessment) -> RecoveryStrategy:
    """
    Runs the Strategist agent and returns a RecoveryStrategy.
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
                "revenue_at_risk_inr": risk_assessment.revenue_at_risk_paise / 100,
                "recovery_probability": risk_assessment.recovery_probability,
                "failure_category": risk_assessment.failure_category,
                "failure_type": risk_assessment.failure_type,
                "timing_penalty_applied": risk_assessment.timing_penalty_applied,
                "priority": risk_assessment.priority,
                "reasoning": risk_assessment.reasoning,
                "force_payment_link": request.force_payment_link,
                "time_of_failure_hour": request.time_of_failure_hour,
                "amount_inr": request.amount_paise / 100,
                "customer_ltv_inr": request.customer_history.lifetime_value_paise / 100,
                "max_retry_amount_inr": request.merchant_policy.max_retry_amount_paise / 100,
                "max_retries": request.merchant_policy.max_retries,
                "retry_cooldown_minutes": request.merchant_policy.retry_cooldown_minutes,
                "require_human_above_inr": request.merchant_policy.require_human_above_paise / 100,
                "allowed_actions": ", ".join(request.merchant_policy.allowed_actions),
            })

            # DEBUG: Log raw LLM output
            print(f"🔍 DEBUG Strategist [Attempt {attempt + 1}]: Risk probability={risk_assessment.recovery_probability}, Amount=₹{request.amount_paise/100}")
            print(f"🔍 DEBUG Raw LLM Output: {json.dumps(result, indent=2)}")
            
            # Validate strategy is in allowed actions
            strategy_obj = RecoveryStrategy(**result)
            print(f"✅ DEBUG Validation passed: strategy={strategy_obj.strategy}, confidence={strategy_obj.confidence}")
            
            # Map strategy to action name for validation
            strategy_map = {
                "retry_payment": "retry",
                "generate_payment_link": "payment_link",
                "notify_customer": "notify",
                "schedule_retry": "retry",
                "escalate_to_merchant": "escalate",
                "stop_recovery": "stop",
            }
            
            action_name = strategy_map.get(strategy_obj.strategy, "payment_link")
            if action_name not in request.merchant_policy.allowed_actions:
                # Override to payment_link if not allowed
                print(f"⚠️ DEBUG Strategy '{strategy_obj.strategy}' not in allowed actions, overriding to payment_link")
                strategy_obj.strategy = "generate_payment_link"
                strategy_obj.reasoning = f"Original strategy not in allowed actions, defaulting to payment_link"
            
            return strategy_obj

        except Exception as e:
            import traceback
            print(f"❌ DEBUG Strategist validation failed [Attempt {attempt + 1}]: {str(e)}")
            print(f"❌ DEBUG Exception type: {type(e).__name__}")
            print(f"❌ DEBUG Raw result causing error: {result if 'result' in locals() else 'No result'}")
            print(f"❌ DEBUG Full traceback:")
            traceback.print_exc()
            
            if attempt == 0:
                print(f"🔄 DEBUG Retrying strategist")
                continue
            # Fallback
            print(f"⚠️ DEBUG Returning fallback strategy")
            return RecoveryStrategy(
                strategy="generate_payment_link",
                confidence=0.5,
                delay_minutes=0,
                recovery_window_reason="AI output validation failed, using safe default",
                message_template=None,
                reasoning="Fallback strategy due to validation error"
            )
