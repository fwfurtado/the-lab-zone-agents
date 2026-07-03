"""Servidor HTTP do núcleo de triagem — o transporte do sidecar Go (Fase B).

Embrulha agents.triage.agent.answer numa rota POST /triage. É o MESMO núcleo
consumido pela CLI e pela ponte Slack (contrato AnswerFn); só muda o
transporte. aiohttp porque já é dependência direta do monorepo — delta zero.

Decisões de fronteira (ADR-0017 no the-lab-zone):
- Bind em 127.0.0.1 por padrão: no pod sidecar, só o container Go alcança o
  núcleo. Não há Service nem CNP para esta porta — o núcleo não existe na
  rede do cluster.
- Métricas em 0.0.0.0 (start_http_server do prometheus_client): o vmagent
  scrapa via VMPodScrape e o kubelet usa /metrics como liveness — ambos batem
  no IP do pod, então este é o ÚNICO bind não-loopback do processo.
- Semáforo local de concorrência: o worker pool do Go já limita o
  paralelismo; o semáforo é defesa em profundidade para o núcleo não aceitar
  mais do que aguenta mesmo se a borda errar. Os caps de runtime do agente
  (CappedToolset + UsageLimits) continuam sendo o piso — nada aqui os afrouxa.

Config por env — transporte, não agente; de propósito fora de shared.config
(o piso de segurança de lá não deve acumular campos de transporte):
    TRIAGE_BIND_HOST        (default 127.0.0.1)
    TRIAGE_BIND_PORT        (default 8081)
    TRIAGE_METRICS_PORT     (default 9091)
    TRIAGE_MAX_CONCURRENCY  (default 4)
    TRIAGE_SHUTDOWN_TIMEOUT (default 600 — segundos p/ drenar triagem em curso)
"""

import asyncio
import json
import logging
import os

from aiohttp import web

from agents.triage.agent import answer
from shared.config import get_settings
from shared.log import configure_logging
from shared.metrics import (
    answer_errors_total,
    answer_latency,
    questions_total,
    start_metrics_server,
)

logger = logging.getLogger("the_lab_zone_triage.server")

_SEMAPHORE_KEY = web.AppKey("semaphore", asyncio.Semaphore)


def _env_int(name: str, default: int) -> int:
    raw = os.environ.get(name, "")
    if not raw:
        return default
    try:
        return int(raw)
    except ValueError as exc:  # config inválida deve abortar o boot, não virar default
        raise SystemExit(f"{name} inválido: {raw!r}") from exc


async def _handle_healthz(_: web.Request) -> web.Response:
    return web.Response(text="ok")


async def _handle_triage(request: web.Request) -> web.Response:
    try:
        payload = await request.json()
    except json.JSONDecodeError:
        return web.json_response({"error": "corpo não é JSON válido"}, status=400)

    context = str(payload.get("context") or "").strip()
    if not context:
        return web.json_response(
            {"error": "campo 'context' ausente ou vazio"}, status=400
        )

    questions_total.inc()
    semaphore = request.app[_SEMAPHORE_KEY]
    async with semaphore:
        with answer_latency.time():
            try:
                report = await answer(context)
            except Exception:
                answer_errors_total.inc()
                logger.exception("triagem falhou")
                return web.json_response(
                    {"error": "triagem falhou; ver logs do núcleo"}, status=500
                )

    return web.json_response({"report": report})


def _build_app(max_concurrency: int) -> web.Application:
    app = web.Application()
    app[_SEMAPHORE_KEY] = asyncio.Semaphore(max_concurrency)
    app.router.add_get("/healthz", _handle_healthz)
    app.router.add_post("/triage", _handle_triage)
    return app


def main() -> None:
    # Falha cedo se o env de runtime (LITELLM_KEY, TOOLHIVE_VMCP_URL) faltar —
    # melhor crash-loop com mensagem clara do que 500 na primeira triagem.
    settings = get_settings()
    configure_logging(settings.log_level)

    host = os.environ.get("TRIAGE_BIND_HOST", "127.0.0.1")
    port = _env_int("TRIAGE_BIND_PORT", 8081)
    metrics_port = _env_int("TRIAGE_METRICS_PORT", 9091)
    max_concurrency = _env_int("TRIAGE_MAX_CONCURRENCY", 4)
    shutdown_timeout = _env_int("TRIAGE_SHUTDOWN_TIMEOUT", 600)

    start_metrics_server(metrics_port)
    logger.info(
        "núcleo de triagem escutando em %s:%d (métricas em :%d, concorrência máx %d)",
        host,
        port,
        metrics_port,
        max_concurrency,
    )

    # run_app trata SIGTERM: para de aceitar conexões e espera os handlers em
    # curso por shutdown_timeout — a triagem em andamento termina. Deve ser
    # coerente com o TRIAGE_TIMEOUT da borda Go e MENOR que o
    # terminationGracePeriodSeconds do pod.
    web.run_app(
        _build_app(max_concurrency),
        host=host,
        port=port,
        shutdown_timeout=shutdown_timeout,
        print=None,  # logs via logging, não print
    )


if __name__ == "__main__":
    main()
