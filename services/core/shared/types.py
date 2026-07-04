from collections.abc import Awaitable, Callable, Sequence
from typing import Protocol

from pydantic_ai.messages import ModelMessage

OnDelta = Callable[[str], Awaitable[None]]


class AnswerFn(Protocol):
    """Contrato do agente consumido pela ponte do Slack.

    Mantém a fronteira ponte↔agente explícita: o responder depende deste
    Protocol, não da implementação concreta em agent.py. Na Opção 2 (agente
    como API HTTP), só a implementação injetada muda — o contrato fica.
    """

    def __call__(
        self,
        question: str,
        history: Sequence[ModelMessage] | None = None,
        on_delta: OnDelta | None = None,
    ) -> Awaitable[str]: ...
