"""Observabilidade do núcleo: OTel traces + Pyroscope profiles.

A instrumentação vive na APLICAÇÃO, não no gateway LiteLLM: o núcleo conhece o
domínio (agent run, cada model request, cada tool call, e o conteúdo) que o
gateway não vê. A instrumentação base é o patch 1; o BaggageSpanProcessor (leva
os atributos de domínio da borda Go a cada span) e o hook de custo efetivo do
LiteLLM entram no patch 2b.

Setup ÚNICO por processo, chamado no boot de cada transporte (CLI, server HTTP,
bot Slack) ANTES de qualquer run de agente. Idempotente e com gate por env
(OTEL_ENABLED/PYROSCOPE_ENABLED) para não exigir backends em runs locais/CI.

O endpoint e o protocolo do exporter vêm das envs PADRÃO do OTel
(OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_PROTOCOL), lidas direto pelo SDK
— não reimplementamos essa lógica aqui. Aponte OTEL_EXPORTER_OTLP_ENDPOINT para o
Collector (ex.: http://otel-collector.observability.svc.cluster.local:4318; note
:4318 para http/protobuf, não :4317 que é gRPC).
"""

from __future__ import annotations

import atexit
import logging
from typing import TYPE_CHECKING

from shared.config import get_settings

if TYPE_CHECKING:
    import httpx

logger = logging.getLogger("the_lab_zone.observability")

_configured = False


def _domain_baggage_key(key: str) -> bool:
    """Predicado do BaggageSpanProcessor: copia para atributo de span apenas a
    baggage de domínio (langfuse.*, thelabzone.*), não qualquer baggage que
    porventura apareça no contexto.
    """
    return key.startswith("langfuse.") or key.startswith("thelabzone.")


def _configure_pyroscope() -> None:
    settings = get_settings()
    if not settings.pyroscope_enabled:
        logger.info("pyroscope desabilitado (PYROSCOPE_ENABLED=false); sem profiling")
        return

    # Import adiado pelo mesmo motivo dos imports OTel: testes/lint não devem
    # exigir o SDK se profiling estiver desligado no ambiente local.
    import pyroscope

    pyroscope.configure(
        application_name=settings.otel_service_name,
        server_address=settings.pyroscope_server_address,
        sample_rate=settings.pyroscope_sample_rate,
        tags={
            "service_name": settings.otel_service_name,
            "service_namespace": settings.otel_namespace,
            "deployment_environment": settings.otel_environment,
        },
    )
    logger.info(
        "pyroscope configurado: service=%s namespace=%s server=%s sample_rate=%d",
        settings.otel_service_name,
        settings.otel_namespace,
        settings.pyroscope_server_address,
        settings.pyroscope_sample_rate,
    )


def configure_observability() -> None:
    """Configura profiling, TracerProvider global e instrumenta todos os Agents.

    No-op se já configurado; dois transportes no mesmo processo não acontece hoje,
    mas a idempotência é barata.
    """
    global _configured
    if _configured:
        return

    _configure_pyroscope()

    settings = get_settings()
    if not settings.otel_enabled:
        logger.info("otel desabilitado (OTEL_ENABLED=false); sem tracing")
        _configured = True
        return

    # Imports pesados adiados: importar este módulo (em teste, lint) não deve
    # exigir o stack OTel nem custar tempo — só quem chama configure() paga.
    from opentelemetry import trace
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter
    from opentelemetry.sdk.resources import Resource
    from opentelemetry.sdk.trace import TracerProvider
    from opentelemetry.sdk.trace.export import BatchSpanProcessor
    from pydantic_ai import Agent, InstrumentationSettings

    # service.name distingue os domínios no projeto único do Langfuse. O MESMO
    # código serve triagem e QA; cada Deployment seta o seu OTEL_SERVICE_NAME
    # (triage-agent / qa-bot), assim como já faz com TOOLHIVE_VMCP_URL.
    resource = Resource.create(
        {
            "service.name": settings.otel_service_name,
            "service.namespace": settings.otel_namespace,
            "deployment.environment": settings.otel_environment,
        }
    )
    provider = TracerProvider(resource=resource)

    # BaggageSpanProcessor: copia a baggage de domínio (langfuse.*, thelabzone.*)
    # que a borda Go propaga via header W3C para ATRIBUTOS em cada span do agente
    # (agent run, model request, tool calls). É o que faz session, trace-name e
    # alertname/namespace aparecerem no Langfuse por observação.
    from opentelemetry.processor.baggage import BaggageSpanProcessor

    provider.add_span_processor(BaggageSpanProcessor(_domain_baggage_key))

    # OTLPSpanExporter sem endpoint explícito: lê OTEL_EXPORTER_OTLP_ENDPOINT/
    # _PROTOCOL do ambiente (comportamento padrão do SDK).
    provider.add_span_processor(BatchSpanProcessor(OTLPSpanExporter()))
    trace.set_tracer_provider(provider)

    # Flush no fim do processo: o one-shot da CLI sairia antes de o Batch drenar
    # os spans sem isto. shutdown() força o flush.
    atexit.register(provider.shutdown)

    # version e include_content FIXOS (explícitos mesmo batendo com o default de
    # 2.1.0): pinar é justamente não depender do default, que muda entre releases
    # (versões 2-4 do semconv são compat deprecado; 5 é o atual). include_content
    # =True captura prompts/completions/tool args+results — decisão consciente; o
    # dado de cluster passa a fluir pro Langfuse (redação, se preciso, no Collector).
    instrumentation = InstrumentationSettings(
        version=settings.otel_semconv_version,
        include_content=True,
    )
    Agent.instrument_all(instrumentation)

    _configured = True
    logger.info(
        "otel configurado: service=%s namespace=%s semconv=v%d include_content=True",
        settings.otel_service_name,
        settings.otel_namespace,
        settings.otel_semconv_version,
    )


def litellm_cost_http_client() -> httpx.AsyncClient:
    """httpx.AsyncClient que captura o custo EFETIVO do LiteLLM.

    Lê o header `x-litellm-response-cost` da resposta e o anexa como
    `gen_ai.usage.cost` no span de generation corrente — o custo autoritativo do
    gateway (inclui fallback/retry), não a estimativa que o Langfuse infere.

    Detalhes:
    - Só `gen_ai.usage.cost` é lido pelo Langfuse como custo FORNECIDO; o
      `langfuse.observation.cost_details` tem bug conhecido na ingestão OTLP.
    - O header vem em respostas NÃO-streaming. O `agent.run` da triagem é
      não-streaming, então funciona; no caminho streaming (QA/Slack via
      run_stream) o LiteLLM ainda não emite o header.
    - Passado ao OpenAIProvider como http_client; um cliente por Agent.
    """
    import httpx
    from opentelemetry import trace

    async def _capture_cost(response: httpx.Response) -> None:
        raw = response.headers.get("x-litellm-response-cost")
        if not raw:
            return
        try:
            cost = float(raw)
        except ValueError:
            return
        span = trace.get_current_span()
        if span.is_recording():
            span.set_attribute("gen_ai.usage.cost", cost)

    return httpx.AsyncClient(event_hooks={"response": [_capture_cost]})
