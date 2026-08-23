"""
LLM provider abstraction — Groq (primary) with Gemini Flash fallback.
Switch via LLM_PROVIDER=groq|gemini environment variable.
"""

import os
from langchain_groq import ChatGroq
from langchain_google_genai import ChatGoogleGenerativeAI


def get_llm(temperature: float = 0.1):
    """
    Return the configured LLM instance.
    Defaults to Groq llama-3.3-70b-versatile.
    Falls back to Gemini Flash when LLM_PROVIDER=gemini.
    """
    provider = os.getenv('LLM_PROVIDER', 'groq').lower()
    
    if provider == 'groq':
        return ChatGroq(
            model='llama-3.3-70b-versatile',
            api_key=os.getenv('GROQ_API_KEY'),
            temperature=temperature,
            max_tokens=1024,
        )
    elif provider == 'gemini':
        return ChatGoogleGenerativeAI(
            model='gemini-2.0-flash',
            google_api_key=os.getenv('GEMINI_API_KEY'),
            temperature=temperature,
        )
    
    raise ValueError(f'Unknown LLM_PROVIDER: {provider}')
