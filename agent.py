import logging
import os
from collections.abc import Sequence
from pathlib import Path

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

def _load_system_prompt() -> str:
    """Carrega o system prompt de um arquivo.

    Por padrao le prompts/system.md ao lado deste modulo (vai dentro da imagem).
    SYSTEM_PROMPT_PATH sobrescreve o caminho — permite montar via ConfigMap no
    cluster e iterar no prompt sem rebuildar a imagem.
    """
    default_path = Path(__file__).parent / "prompts" / "system.md"
    path = Path(os.environ.get("SYSTEM_PROMPT_PATH", default_path))
    return path.read_text(encoding="utf-8").strip()


SYSTEM_PROMPT = _load_system_prompt()

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

        async with agent.iter(
            question,
            message_history=message_history,
        ) as run:
            async for node in run:
                if agent.is_model_request_node(node):
                    async with node.stream(run.ctx) as stream:
                        async for delta in stream.stream_text(delta=True):
                            if delta:
                                await on_delta(delta)
            return run.result.output if run.result else ""
