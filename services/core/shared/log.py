import json
import logging
from datetime import UTC, datetime

# API OTel (leve; sempre disponível — é dep transitiva do -sdk, que já está no
# pyproject). Diferente do observability.py, aqui não há import pesado a adiar:
# só lemos o span ativo. Sem provider (OTEL_ENABLED=false) o contexto é inválido.
from opentelemetry import trace


class JsonFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        payload = {
            "timestamp": datetime.fromtimestamp(record.created, UTC).isoformat(),
            "level": record.levelname,
            "logger": record.name,
            "message": record.getMessage(),
        }

        # Correlação log<->trace: injeta trace_id/span_id do span ativo quando há
        # um. Fora de um span (boot, ou OTEL_ENABLED=false) o SpanContext é
        # inválido e nada é adicionado — seguro sem tracing. Formato hex padrão
        # OTel (32/16 chars), que é o que o Tempo casa no tracesToLogs e o derived
        # field do datasource VictoriaLogs (campo `trace_id`) usa pro link.
        span_context = trace.get_current_span().get_span_context()
        if span_context.is_valid:
            payload["trace_id"] = trace.format_trace_id(span_context.trace_id)
            payload["span_id"] = trace.format_span_id(span_context.span_id)

        if record.exc_info:
            payload["exc_info"] = self.formatException(record.exc_info)

        return json.dumps(payload, ensure_ascii=True)


def configure_logging(level: int) -> None:
    handler = logging.StreamHandler()
    handler.setFormatter(JsonFormatter())

    root_logger = logging.getLogger()
    root_logger.handlers.clear()
    root_logger.setLevel(level)
    root_logger.addHandler(handler)
