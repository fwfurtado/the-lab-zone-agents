"""Observabilidade OTel do núcleo: TracerProvider + export OTLP + instrumentação
do Pydantic AI.

A instrumentação vive na APLICAÇÃO, não no gateway LiteLLM: o núcleo conhece o
domínio (agent run, cada model request, cada tool call, e o conteúdo) que o
gateway não vê. Este é o patch 1 (base) da inversão da observabilidade de IA —
ainda SEM baggage e SEM custo, que vêm no patch 2.

Setup ÚNICO por processo, chamado no boot de cada transporte (CLI, server HTTP,
bot Slack) ANTES de qualquer run de agente. Idempotente e com gate por env
(OTEL_ENABLED) para não exigir um Collector em runs locais/CI.

O endpoint e o protocolo do exporter vêm das envs PADRÃO do OTel
(OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_PROTOCOL), lidas direto pelo SDK
— não reimplementamos essa lógica aqui. Aponte OTEL_EXPORTER_OTLP_ENDPOINT para o
Collector (ex.: http://otel-collector.observability.svc.cluster.local:4318; note
:4318 para http/protobuf, não :4317 que é gRPC).
"""

from __future__ import annotations

import atexit
import logging

from shared.config import get_settings

logger = logging.getLogger("the_lab_zone.observability")

_configured = False


def configure_observability() -> None:
    """Configura o TracerProvider global e instrumenta todos os Agents.

    No-op se já configurado (dois transportes no mesmo processo não acontece hoje,
    mas a idempotência é barata) ou se OTEL_ENABLED=false.
    """
    global _configured
    if _configured:
        return

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
            "service.namespace": "the-lab-zone",
            "deployment.environment": settings.otel_environment,
        }
    )
    provider = TracerProvider(resource=resource)
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
        "otel configurado: service=%s namespace=the-lab-zone semconv=v%d include_content=True",
        settings.otel_service_name,
        settings.otel_semconv_version,
    )
