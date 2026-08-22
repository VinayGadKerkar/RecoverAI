"""
Risk Analyst Agent
Diagnoses the root cause of a payment failure using UPI error taxonomy
and bank outage signals.
"""

from langchain_core.language_models import BaseChatModel
from langchain_core.prompts import ChatPromptTemplate
from langchain_core.output_parsers import StrOutputParser

from schemas.input import RecoveryRequest


SYSTEM_PROMPT = """You are a payments risk analyst specialising in Indian payment systems (UPI, NPCI, RBI).
Your job is to diagnose WHY a payment failed based on error codes and context.

UPI Error Taxonomy:
- U16: Debit failed – insufficient balance. Customer does not have funds.
- U30: Debit failed – payer account issue. Temporary account restriction.
- Z9:  Transaction declined by bank. Could be fraud flag or bank policy.
- U68: Transaction not permitted. VPA/account type restriction.
- RB:  Request blocked by bank. Often part of a wider bank outage.
- YG:  Risk threshold exceeded. Flagged by NPCI risk engine.

Bank outage signals:
- If bank_outage=true, the failure is likely systemic (not customer fault).
- Outage payments have high recovery chance once the bank is back online.

Output a concise diagnosis (2-3 sentences) explaining the root cause.
Be specific: name the error code, what it means, and whether it is recoverable.
"""

USER_TEMPLATE = """Payment ID: {payment_id}
Amount: ₹{amount_inr:.2f}
Method: {method}
UPI Error Code: {error_code}
Bank: {bank}
Bank Outage Detected: {bank_outage}
Risk Score: {risk_score} ({risk_level})
Recovery Chance: {recovery_chance:.0%}
Risk Factors: {factors}

Diagnose this payment failure."""


async def run_risk_analyst(request: RecoveryRequest, llm: BaseChatModel) -> str:
    """
    Runs the Risk Analyst agent and returns a diagnosis string.
    """
    prompt = ChatPromptTemplate.from_messages([
        ("system", SYSTEM_PROMPT),
        ("human", USER_TEMPLATE),
    ])

    chain = prompt | llm | StrOutputParser()

    diagnosis = await chain.ainvoke({
        "payment_id": request.payment_id,
        "amount_inr": request.amount_inr,
        "method": request.method or "upi",
        "error_code": request.error_code or "unknown",
        "bank": request.bank or "unknown",
        "bank_outage": request.risk_score.bank_outage,
        "risk_score": request.risk_score.score,
        "risk_level": request.risk_score.level,
        "recovery_chance": request.risk_score.recovery_chance,
        "factors": ", ".join(request.risk_score.factors) or "none",
    })

    return diagnosis.strip()
