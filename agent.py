import logging
from collections.abc import Sequence

from pydantic_ai import Agent
from pydantic_ai.mcp import MCPToolset
from pydantic_ai.messages import ModelMessage
from pydantic_ai.models.openai import OpenAIChatModel
from pydantic_ai.providers.openai import OpenAIProvider

from bot.types import OnDelta
from config import get_settings

logger = logging.getLogger("slack_qa_bot.agent")

settings = get_settings()

toolhive = MCPToolset(settings.toolhive_vmcp_url)

model = OpenAIChatModel(
    settings.model_name,
    provider=OpenAIProvider(
        base_url=settings.litellm_base_url,
        api_key=settings.litellm_key.get_secret_value(),
    ),
)

SYSTEM_PROMPT = (
    "Você é o assistente de infra do the-lab-zone. Responde perguntas sobre o "
    "estado vivo do cluster cruzando três fontes: estado k8s (read-only), "
    "métricas (VictoriaMetrics/MetricsQL) e o design documentado (Qdrant).\n"
    "\n"
    "Regras:\n"
    "- Você é READ-ONLY. Nunca propõe mutação direta no cluster; mudanças vão "
    "por PR no repo GitOps (ADR-0015).\n"
    "- Cite a fonte de cada afirmação factual: diga qual ferramenta trouxe o "
    "dado (ex. 'segundo o estado k8s…', 'a métrica X em [janela]…', 'a doc Y…').\n"
    "- Se as fontes divergirem (ex. o que está deployado != o que a doc diz), "
    "aponte a divergência em vez de escolher uma calado.\n"
    "- Se não houver dado pra responder, diga isso - não invente.\n"
    "- Responde em PT-BR, direto e conciso."
)

agent = Agent(
    model,
    toolsets=[toolhive],
    system_prompt=SYSTEM_PROMPT,
)


async def answer(
    question: str,
    history: Sequence[ModelMessage] | None = None,
    on_delta: OnDelta | None = None,
) -> str:
    message_history = history or []

    async with agent:
        if on_delta is None:
            result = await agent.run(question, message_history=message_history)
            return result.output

        async with agent.run_stream(
            question,
            message_history=message_history,
        ) as stream:
            emitted = 0
            async for full_text in stream.stream_text():
                delta = full_text[emitted:]
                if delta:
                    await on_delta(delta)
                    emitted = len(full_text)
            return await stream.get_output()
