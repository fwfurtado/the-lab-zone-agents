import time

_STREAM_THROTTLE_S = 0.8
# Teto seguro por mensagem. O markdown block aceita 12k, mas mensagens menores
# leem melhor no Slack e dao folga pro overhead do bloco. ~3500 e confortavel.
_MAX_CHARS = 3500


def _split_markdown(text: str, limit: int = _MAX_CHARS) -> list[str]:
    """
    Fatia um texto Markdown em pedacos <= limit, sem quebrar formatacao.

    Regras de corte, em ordem de preferencia:
    - nunca corta dentro de um code fence (``` aberto): o pedaco vai ate o
      fechamento do fence, mesmo que passe um pouco do limite;
    - prefere cortar em linha em branco (fronteira de paragrafo);
    - senao, corta em quebra de linha.
    """
    if len(text) <= limit:
        return [text]

    lines = text.split("\n")
    chunks: list[str] = []
    current: list[str] = []
    current_len = 0
    in_fence = False

    def flush_current() -> None:
        nonlocal current, current_len
        if current:
            chunks.append("\n".join(current))
            current = []
            current_len = 0

    for line in lines:
        # +1 pelo \n que sera reinserido
        line_len = len(line) + 1
        is_fence = line.lstrip().startswith("```")

        # Se adicionar a linha estoura o limite E nao estamos dentro de um
        # fence, fecha o pedaco atual antes (corte em fronteira segura).
        if current_len + line_len > limit and not in_fence and current:
            flush_current()

        current.append(line)
        current_len += line_len

        if is_fence:
            in_fence = not in_fence

    flush_current()
    return chunks


class SlackMessageUpdater:
    """
    Atualiza uma mensagem do Slack incrementalmente, com throttle.

    Durante o stream usa text= (rapido, sem piscar com Markdown parcial).
    No finalize, renderiza Markdown via markdown block e, se a resposta passar
    do limite do Slack, fatia em multiplas mensagens no mesmo thread.
    """

    def __init__(self, logger, client, channel: str, ts: str, thread_ts: str):
        self._logger = logger
        self._client = client
        self._channel = channel
        self._ts = ts
        self._thread_ts = thread_ts
        self._buffer = ""
        self._last_update = 0.0
        self._dirty = False

    async def push(self, delta: str) -> None:
        self._buffer += delta
        self._dirty = True
        now = time.monotonic()
        if now - self._last_update >= _STREAM_THROTTLE_S:
            await self._flush()

    async def _flush(self) -> None:
        """Flush intermediario (streaming): text= cru, truncado ao teto do text."""
        if not self._dirty or not self._buffer:
            return

        try:
            # Limite do text= e ~40k; durante o stream mandamos so o final
            # do buffer pra nunca estourar e manter a "digitacao" visivel.
            preview = self._buffer[-_MAX_CHARS:]
            await self._client.chat_update(
                channel=self._channel,
                ts=self._ts,
                text=preview,
            )
            self._last_update = time.monotonic()
            self._dirty = False
        except Exception:
            self._logger.debug(
                "chat_update intermediario falhou; tentando no flush final.",
                exc_info=True,
            )

    async def finalize(self, text: str | None = None) -> None:
        """
        Render final: fatia a resposta e renderiza cada pedaco como markdown
        block. Primeiro pedaco edita a placeholder; os demais sao novas
        mensagens no mesmo thread.
        """
        if text is not None:
            self._buffer = text

        if not self._buffer:
            return

        chunks = _split_markdown(self._buffer)

        # Primeiro pedaco: edita a mensagem-placeholder.
        try:
            await self._client.chat_update(
                channel=self._channel,
                ts=self._ts,
                text=chunks[0],
                blocks=[{"type": "markdown", "text": chunks[0]}],
            )
        except Exception:
            self._logger.exception("Falha ao editar a mensagem final (chunk 0).")

        # Pedacos seguintes: novas mensagens no mesmo thread.
        for chunk in chunks[1:]:
            try:
                await self._client.chat_postMessage(
                    channel=self._channel,
                    thread_ts=self._thread_ts,
                    text=chunk,
                    blocks=[{"type": "markdown", "text": chunk}],
                )
            except Exception:
                self._logger.exception("Falha ao postar continuacao no thread.")
