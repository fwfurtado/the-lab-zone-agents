"""Agente de triage: constrói o Assistant sob demanda (lazy).

O Assistant NÃO é instanciado no import: construí-lo chama get_settings(), que
exige env de runtime (LITELLM_KEY, TOOLHIVE_VMCP_URL). Instanciar no nível de
módulo acoplaria QUALQUER import (inclusive `triage --help`, testes, linters) à
config de produção. O _get_assistant é cacheado — um único Assistant por
processo, criado na primeira chamada a answer(), depois do parse de args.
"""

from collections.abc import Sequence
from functools import lru_cache
from pathlib import Path

from shared.prompts import load_prompt
from shared.runtime import Assistant
from shared.types import ModelMessage, OnDelta

_PROMPT_PATH = Path(__file__).parent / "prompts" / "triage.md"


@lru_cache
def _get_assistant() -> Assistant:
    return Assistant(load_prompt(_PROMPT_PATH))


async def answer(
    question: str,
    history: Sequence[ModelMessage] | None = None,
    on_delta: OnDelta | None = None,
) -> str:
    """Contrato AnswerFn (shared.types) — consumido pela CLI e pela ponte Slack.

    Constrói o Assistant na primeira chamada; import do módulo é barato e não
    exige env.
    """
    return await _get_assistant().answer(question, history, on_delta)
