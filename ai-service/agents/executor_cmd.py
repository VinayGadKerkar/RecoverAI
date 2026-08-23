"""
Agent 3: Executor Command Builder
Builds a structured command for the Go Policy Engine.
This agent NEVER calls Razorpay APIs — it only produces JSON.
"""

from schemas.output import ExecutorCommand, RiskAssessment, RecoveryStrategy


SYSTEM_PROMPT = """You are a command builder for a payment recovery system. Respond ONLY with a valid JSON object. Your output will be validated by a deterministic policy engine before any real action is taken. Never suggest actions not in the allowed list.

Output JSON schema:
{
  "action": "RETRY_PAYMENT|GENERATE_PAYMENT_LINK|SEND_NOTIFICATION|ESCALATE|STOP",
  "payment_id": "<str>",
  "case_id": "<str>",
  "scheduled_at_minutes": <int>,
  "parameters": {
    // RETRY_PAYMENT: {}
    // GENERATE_PAYMENT_LINK: { "expire_by_minutes": 1440 }
    // SEND_NOTIFICATION: { "channel": "sms|email", "template_key": "<str>" }
    // ESCALATE: { "reason": "<str>" }
    // STOP: { "reason": "<str>" }
  }
}"""


def build_executor_command(
    request,
    risk_assessment: RiskAssessment,
    strategy: RecoveryStrategy
) -> ExecutorCommand:
    """
    Builds an ExecutorCommand from the strategy output.
    This is deterministic — no LLM call needed for Agent 3.
    """
    
    # Map strategy to action
    action_map = {
        "retry_payment": "RETRY_PAYMENT",
        "generate_payment_link": "GENERATE_PAYMENT_LINK",
        "notify_customer": "SEND_NOTIFICATION",
        "schedule_retry": "RETRY_PAYMENT",
        "escalate_to_merchant": "ESCALATE",
        "stop_recovery": "STOP",
    }
    
    action = action_map.get(strategy.strategy, "STOP")
    
    # Build parameters based on action
    parameters = {}
    
    if action == "RETRY_PAYMENT":
        parameters = {}
    
    elif action == "GENERATE_PAYMENT_LINK":
        parameters = {
            "expire_by_minutes": 1440  # 24 hours
        }
    
    elif action == "SEND_NOTIFICATION":
        parameters = {
            "channel": "sms",
            "template_key": strategy.message_template or "default_retry_notification"
        }
    
    elif action == "ESCALATE":
        parameters = {
            "reason": strategy.reasoning
        }
    
    elif action == "STOP":
        parameters = {
            "reason": strategy.reasoning
        }
    
    return ExecutorCommand(
        action=action,
        payment_id=request.payment_id,
        case_id=request.case_id,
        scheduled_at_minutes=strategy.delay_minutes,
        parameters=parameters,
        risk_assessment_summary={
            "recovery_probability": risk_assessment.recovery_probability,
            "failure_type": risk_assessment.failure_type,
            "priority": risk_assessment.priority,
        },
        strategy_summary={
            "strategy": strategy.strategy,
            "confidence": strategy.confidence,
            "reasoning": strategy.reasoning,
        }
    )
