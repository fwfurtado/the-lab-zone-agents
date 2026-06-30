import time


_STREAM_THROTTLE_S = 0.8


class SlackMessageUpdater:
    """
    Atualiza uma mensagem do Slack incrementalmente, com throttle.
    """

    def __init__(self, logger, client, channel: str, ts: str):
        self._logger = logger
        self._client = client
        self._channel = channel
        self._ts = ts
        self._buffer = ""
        self._last_update = 0.0
        self._dirty = False

    async def push(self, delta: str) -> None:
        self._buffer += delta
        self._dirty = True
        now = time.monotonic()
        if now - self._last_update >= _STREAM_THROTTLE_S:
            await self._flush()

    async def _flush(self, markdown: bool = False) -> None:
        if not self._dirty or not self._buffer:
            return

        try:
            kwargs = {"channel": self._channel, "ts": self._ts, "text": self._buffer}
            if markdown:
                # markdown block: o Slack renderiza Markdown padrao (negrito,
                # headers, tabelas, code blocks) sem conversao manual. So no
                # final, pois durante o stream o Markdown parcial pisca.
                # O text= acima fica como fallback (notificacoes/clientes velhos).
                kwargs["blocks"] = [{"type": "markdown", "text": self._buffer}]
            await self._client.chat_update(**kwargs)
            self._last_update = time.monotonic()
            self._dirty = False
        except Exception:
            self._logger.debug(
                "chat_update intermediario falhou; tentando no flush final.",
                exc_info=True,
            )

    async def finalize(self, text: str | None = None) -> None:
        if text is not None:
            self._buffer = text
            self._dirty = True

        self._last_update = 0.0
        await self._flush(markdown=True)
