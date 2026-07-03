import logging
from collections.abc import Sequence
from dataclasses import dataclass
from typing import Any

from pydantic_ai import Agent
from pydantic_ai.mcp import MCPToolset
from pydantic_ai.messages import ModelMessage
from pydantic_ai.models.openai import OpenAIChatModel
from pydantic_ai.providers.openai import OpenAIProvider
from pydantic_ai.toolsets import WrapperToolset
from pydantic_ai.usage import UsageLimits

from shared.config import get_settings
from shared.types import OnDelta

logger = logging.getLogger("the_lab_zone.runtime")


_TRUNCATION_NOTE = (
    "\n\n[...truncado: o resultado da tool `{name}` excedeu {max} chars e foi "
    "cortado antes de entrar no contexto. Refine a chamada (janela de _time, "
    "limite de linhas, stream selector, namespace) para ver menos e melhor.]"
)


def _cap_tool_result(result: Any, name: str, max_chars: int) -> Any:
    """Trunca o resultado de uma tool se passar de max_chars.

    Limite RÍGIDO, aplicado ANTES do resultado entrar no contexto do modelo —
    independe do que o modelo pediu. É o cinturão de segurança que a regra de
    prompt ("restrinja logs") não garante: uma tool que despeja MBs de log não
    consegue mais, sozinha, estourar a janela de contexto.
    """
    if isinstance(result, str):
        if len(result) <= max_chars:
            return result
        return result[:max_chars] + _TRUNCATION_NOTE.format(name=name, max=max_chars)

    # Conteúdo não-string (estruturado): mede a forma serializada. Se couber,
    # devolve intacto (preserva a estrutura); se não, degrada para string
    # truncada — perder estrutura é melhor que estourar o contexto.
    try:
        text = str(result)
    except Exception:  # pragma: no cover
        return result
    if len(text) <= max_chars:
        return result
    return text[:max_chars] + _TRUNCATION_NOTE.format(name=name, max=max_chars)


@dataclass
class CappedToolset(WrapperToolset):
    """Envelopa um toolset e trunca o resultado de qualquer tool acima de
    `max_chars`, antes de o resultado entrar no contexto do modelo.

    WrapperToolset é dataclass (campo `wrapped`); herdamos e adicionamos
    `max_chars`. O `replace(self, wrapped=...)` que o for_run do pai faz
    preserva `max_chars`.
    """

    max_chars: int = 60_000

    async def call_tool(
        self,
        name: str,
        tool_args: dict[str, Any],
        ctx: Any,
        tool: Any,
    ) -> Any:
        result = await super().call_tool(name, tool_args, ctx, tool)
        return _cap_tool_result(result, name, self.max_chars)


def build_agent(system_prompt: str) -> Agent:
    """Constrói um Agent Pydantic AI ligado ao vMCP e ao modelo via LiteLLM.

    vMCP, LiteLLM e modelo vêm de env (get_settings), então o MESMO código
    serve QA e triagem — cada processo aponta o TOOLHIVE_VMCP_URL pro seu vMCP.
    O toolset do vMCP é envelopado em CappedToolset: nenhum resultado de tool
    passa de max_tool_result_chars.
    """
    settings = get_settings()
    toolhive = MCPToolset(settings.toolhive_vmcp_url)
    capped = CappedToolset(wrapped=toolhive, max_chars=settings.max_tool_result_chars)
    model = OpenAIChatModel(
        settings.model_name,
        provider=OpenAIProvider(
            base_url=settings.litellm_base_url,
            api_key=settings.litellm_key.get_secret_value(),
        ),
    )
    return Agent(model, toolsets=[capped], system_prompt=system_prompt)


def _usage_limits() -> UsageLimits:
    """Circuit breaker do agente: aborta cedo e barato em vez de estourar
    contexto/custo lá no provedor.

    - request_limit: teto de chamadas ao modelo (mata loop de tools).
    - tool_calls_limit: teto de chamadas de tool na run.
    - total_tokens_limit: backstop acumulado, bem abaixo da janela do provedor.

    Ao exceder, Pydantic AI levanta UsageLimitExceeded (a CLI trata como erro).
    """
    settings = get_settings()
    return UsageLimits(
        request_limit=settings.agent_request_limit,
        tool_calls_limit=settings.agent_tool_calls_limit,
        total_tokens_limit=settings.agent_total_tokens_limit,
    )


class Assistant:
    """Um agente + a lógica de streaming, agnóstico ao transporte.

    Consumido via `.answer` (contrato shared.types.AnswerFn) — pela ponte Slack
    (com on_delta) ou pela CLI (on_delta=None, retorna o texto final). O que
    diferencia QA de triagem é só o system prompt injetado no construtor.
    """

    def __init__(self, system_prompt: str) -> None:
        self._agent = build_agent(system_prompt)

    async def answer(
        self,
        question: str,
        history: Sequence[ModelMessage] | None = None,
        on_delta: OnDelta | None = None,
    ) -> str:
        agent = self._agent
        message_history = history or []
        limits = _usage_limits()

        async with agent:
            if on_delta is None:
                result = await agent.run(
                    question,
                    message_history=message_history,
                    usage_limits=limits,
                )
                return result.output

            # Streaming via agent.iter iterando nós — evita o cancelamento de
            # tool call no meio que run_stream+stream_text causava. Não trocar.
            async with agent.iter(
                question,
                message_history=message_history,
                usage_limits=limits,
            ) as run:
                async for node in run:
                    if agent.is_model_request_node(node):
                        async with node.stream(run.ctx) as stream:
                            async for delta in stream.stream_text(delta=True):
                                if delta:
                                    await on_delta(delta)
                return run.result.output if run.result else ""
