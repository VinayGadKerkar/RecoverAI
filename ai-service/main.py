"""
RecoverAI — Python AI Service
FastAPI entry point. This service is INTERNAL ONLY — it never receives
direct external requests. All traffic comes from the Go worker.

The AI produces structured JSON commands. It NEVER executes financial
operations directly.
"""

import logging
import os
from contextlib import asynccontextmanager

from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware

from llm import get_llm
from graph.recovery_graph import build_recovery_graph
from schemas.input import RecoveryRequest
from schemas.output import RecoveryCommand

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s %(levelname)s %(name)s %(message)s",
)
logger = logging.getLogger(__name__)

# Build the LangGraph recovery graph at startup
recovery_graph = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global recovery_graph
    logger.info("Initialising LLM and recovery graph...")
    llm = get_llm()
    recovery_graph = build_recovery_graph(llm)
    logger.info("Recovery graph ready")
    yield
    logger.info("Shutting down AI service")


app = FastAPI(
    title="RecoverAI — AI Recovery Service",
    version="1.0.0",
    description="Internal AI service for payment recovery decisions. Never call externally.",
    lifespan=lifespan,
)

app.add_middleware(
    CORSMiddleware,
    allow_origins=["http://localhost:8080"],  # Go API only
    allow_methods=["POST"],
    allow_headers=["Content-Type"],
)


@app.get("/health")
async def health():
    return {"status": "ok", "llm_provider": os.getenv("LLM_PROVIDER", "groq")}


@app.post("/api/v1/recover", response_model=RecoveryCommand)
async def recover(request: RecoveryRequest) -> RecoveryCommand:
    """
    Analyse a risk-scored failed payment and produce a structured recovery command.

    The command is returned to the Go Policy Engine, which decides whether
    to execute it. The AI never executes financial operations directly.
    """
    if recovery_graph is None:
        raise HTTPException(status_code=503, detail="Recovery graph not initialised")

    logger.info(
        "Processing recovery request",
        extra={"payment_id": request.payment_id, "risk_score": request.risk_score.score},
    )

    try:
        result = await recovery_graph.ainvoke({"request": request})
        command: RecoveryCommand = result["command"]
        logger.info(
            "Recovery command generated",
            extra={
                "payment_id": command.payment_id,
                "action": command.recommended_action,
                "confidence": command.confidence,
            },
        )
        return command
    except Exception as exc:
        logger.error("Recovery graph error: %s", exc, exc_info=True)
        raise HTTPException(status_code=500, detail=f"Recovery graph error: {exc}")
