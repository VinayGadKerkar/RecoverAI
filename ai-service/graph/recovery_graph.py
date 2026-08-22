"""
LangGraph Recovery Graph
Pipeline: risk_analyst → strategist → executor_cmd

State flows through three nodes:
  1. risk_analyst  — produces a diagnosis string
  2. strategist    — produces a strategy dict (JSON)
  3. executor_cmd  — serialises into a validated RecoveryCommand

The AI NEVER executes financial operations — it only produces the command.
"""

from typing import TypedDict, Any

from langgraph.graph import StateGraph, END
from langchain_core.language_models import BaseChatModel

from schemas.input import RecoveryRequest
from schemas.output import RecoveryCommand
from agents.risk_analyst import run_risk_analyst
from agents.strategist import run_strategist
from agents.executor_cmd import build_command


class RecoveryState(TypedDict):
    request: RecoveryRequest
    diagnosis: str
    strategy: dict[str, Any]
    command: RecoveryCommand
    attempt_count: int


def build_recovery_graph(llm: BaseChatModel) -> StateGraph:
    """
    Builds and compiles the LangGraph recovery graph.
    Returns a compiled graph ready for ainvoke().
    """

    async def risk_analyst_node(state: RecoveryState) -> RecoveryState:
        diagnosis = await run_risk_analyst(state["request"], llm)
        return {**state, "diagnosis": diagnosis}

    async def strategist_node(state: RecoveryState) -> RecoveryState:
        strategy = await run_strategist(
            state["request"],
            state["diagnosis"],
            state.get("attempt_count", 0),
            llm,
        )
        return {**state, "strategy": strategy}

    def executor_cmd_node(state: RecoveryState) -> RecoveryState:
        command = build_command(
            state["request"],
            state["diagnosis"],
            state["strategy"],
        )
        return {**state, "command": command}

    graph = StateGraph(RecoveryState)
    graph.add_node("risk_analyst", risk_analyst_node)
    graph.add_node("strategist", strategist_node)
    graph.add_node("executor_cmd", executor_cmd_node)

    graph.set_entry_point("risk_analyst")
    graph.add_edge("risk_analyst", "strategist")
    graph.add_edge("strategist", "executor_cmd")
    graph.add_edge("executor_cmd", END)

    return graph.compile()
