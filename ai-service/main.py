"""
RecoverAI — Python AI Service
FastAPI entry point. This service is INTERNAL ONLY — it never receives
direct external requests. All traffic comes from the Go validator consumer.

The AI produces structured JSON commands. It NEVER executes financial
operations directly.
"""

import logging
import os

from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware

from llm import get_llm
from agents.risk_analyst import run_risk_analyst
from agents.strategist import run_strategist
from agents.executor_cmd import build_executor_command
from schemas.input import AnalyzeRequest
from schemas.output import ExecutorCommand

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger(__name__)

app = FastAPI(
    title="RecoverAI — AI Recovery Service",
    version="1.0.0",
    description="Internal AI service for payment recovery decisions. Never call externally.",
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["http://localhost:8080"],  # Go API only
    allow_methods=["POST"],
    allow_headers=["Content-Type"],
)

# Initialize LLM at startup
llm = None

@app.on_event("startup")
async def startup():
    global llm
    logger.info("Initializing LLM...")
    llm = get_llm(temperature=0.1)
    logger.info(f"LLM ready: {os.getenv('LLM_PROVIDER', 'groq')}")


@app.get("/health")
async def health():
    return {
        "status": "ok",
        "llm_provider": os.getenv("LLM_PROVIDER", "groq")
    }


@app.post("/analyze", response_model=ExecutorCommand)
async def analyze(request: AnalyzeRequest) -> ExecutorCommand:
    """
    Analyze a failed payment and produce a structured recovery command.
    
    Flow: Risk Analyst → Strategist → Executor Command Builder (sequential)
    
    The command is returned to the Go Policy Engine, which decides whether
    to execute it. The AI never executes financial operations directly.
    """
    if llm is None:
        raise HTTPException(status_code=503, detail="LLM not initialized")

    logger.info(
        f"Processing recovery request: payment_id={request.payment_id}, "
        f"amount=₹{request.amount_paise/100:.2f}, error={request.upi_error_code}"
    )

    try:
        # ─── Agent 1: Risk Analyst ────────────────────────────────────────────
        risk_assessment = await run_risk_analyst(llm, request)
        logger.info(
            f"Risk assessment complete: probability={risk_assessment.recovery_probability:.2f}, "
            f"priority={risk_assessment.priority}"
        )

        # ─── Agent 2: Recovery Strategist ─────────────────────────────────────
        strategy = await run_strategist(llm, request, risk_assessment)
        logger.info(
            f"Strategy selected: {strategy.strategy}, "
            f"confidence={strategy.confidence:.2f}, "
            f"delay={strategy.delay_minutes}min"
        )

        # ─── Agent 3: Executor Command Builder ───────────────────────────────
        command = build_executor_command(request, risk_assessment, strategy)
        logger.info(
            f"Command generated: action={command.action}, "
            f"scheduled_at={command.scheduled_at_minutes}min"
        )

        return command

    except Exception as exc:
        logger.error(f"Recovery analysis failed: {exc}", exc_info=True)
        # Return safe STOP command on error
        return ExecutorCommand(
            action="STOP",
            payment_id=request.payment_id,
            case_id=request.case_id,
            scheduled_at_minutes=0,
            parameters={"reason": f"AI pipeline error: {str(exc)}"},
            risk_assessment_summary=None,
            strategy_summary=None,
        )
