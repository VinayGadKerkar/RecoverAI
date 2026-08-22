"""
LLM provider abstraction — Groq (primary) with Gemini Flash fallback.
Switch via LLM_PROVIDER=groq|gemini environment variable.
"""

import os
import logging
from langchain_core.language_models import BaseChatModel

logger = logging.getLogger(__name__)


def get_llm() -> BaseChatModel:
    """
    Return the configured LLM instance.
    Defaults to Groq llama-3.3-70b-versatile.
    Falls back to Gemini Flash when LLM_PROVIDER=gemini.
    """
    provider = os.getenv("LLM_PROVIDER", "groq").lower()

    if provider == "gemini":
        return _get_gemini()

    return _get_groq()


def _get_groq() -> BaseChatModel:
    from langchain_groq import ChatGroq

    api_key = os.getenv("GROQ_API_KEY")
    if not api_key:
        raise RuntimeError("GROQ_API_KEY environment variable is not set")

    logger.info("Using Groq llama-3.3-70b-versatile")
    return ChatGroq(
        model="llama-3.3-70b-versatile",
        api_key=api_key,
        temperature=0.1,      # Low temperature for consistent financial decisions
        max_tokens=1024,
        timeout=25,
        max_retries=2,
    )


def _get_gemini() -> BaseChatModel:
    from langchain_google_genai import ChatGoogleGenerativeAI

    api_key = os.getenv("GEMINI_API_KEY")
    if not api_key:
        raise RuntimeError("GEMINI_API_KEY environment variable is not set")

    logger.info("Using Gemini Flash (fallback)")
    return ChatGoogleGenerativeAI(
        model="gemini-1.5-flash",
        google_api_key=api_key,
        temperature=0.1,
        max_output_tokens=1024,
    )
