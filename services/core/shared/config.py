import logging
from functools import lru_cache
from typing import Annotated, Literal

from pydantic import BeforeValidator, Field, SecretStr
from pydantic.functional_validators import field_validator
from pydantic_settings import BaseSettings, SettingsConfigDict


class Settings(BaseSettings):
    model_config = SettingsConfigDict(
        env_prefix="",
        env_file=".env",
        env_file_encoding="utf-8",
        extra="ignore",
    )

    # Opcionais: só o modo bot (QA) precisa. A CLI de triagem não fala com o
    # Slack, então não deve exigir esses tokens. start_bot valida a presença.
    slack_bot_token: SecretStr | None = Field(default=None, alias="SLACK_BOT_TOKEN")
    slack_app_token: SecretStr | None = Field(default=None, alias="SLACK_APP_TOKEN")

    litellm_base_url: str = Field(
        default="http://litellm.ai.svc.cluster.local:4000/v1",
        alias="LITELLM_BASE_URL",
    )
    litellm_key: SecretStr = Field(alias="LITELLM_KEY")
    model_name: str = Field(default="qwen3-coder-30b-local", alias="MODEL_NAME")

    toolhive_vmcp_url: str = Field(alias="TOOLHIVE_VMCP_URL")

    # --- Garage (feedback humano de triagem, ADR-0014) ---
    # Opcionais: só o processo que registra o listener de feedback precisa.
    # Mesma nomenclatura de env que a borda Go já usa (GaragePublisher,
    # services/triage-webhook/internal/publish/garage.go) — um vocabulário só
    # entre as duas linguagens que falam com o mesmo bucket.
    garage_endpoint: str | None = Field(default=None, alias="GARAGE_ENDPOINT")
    garage_access_key: SecretStr | None = Field(default=None, alias="GARAGE_ACCESS_KEY")
    garage_secret_key: SecretStr | None = Field(default=None, alias="GARAGE_SECRET_KEY")
    garage_bucket: str = Field(default="triage-reports", alias="GARAGE_BUCKET")
    garage_region: str = Field(default="garage", alias="GARAGE_REGION")
    garage_use_ssl: bool = Field(default=False, alias="GARAGE_USE_SSL")

    # Limites RÍGIDOS de runtime do agente. Regra de prompt pede contenção;
    # estes garantem. Defesa contra tool que despeja resultado gigante e
    # contra loop de tools estourando o contexto/custo.
    max_tool_result_chars: int = Field(default=60_000, alias="MAX_TOOL_RESULT_CHARS")
    agent_request_limit: int = Field(default=25, alias="AGENT_REQUEST_LIMIT")
    agent_tool_calls_limit: int = Field(default=40, alias="AGENT_TOOL_CALLS_LIMIT")
    agent_total_tokens_limit: int = Field(default=600_000, alias="AGENT_TOTAL_TOKENS_LIMIT")
    agent_max_concurrency: int | None = Field(default=None, alias="AGENT_MAX_CONCURRENCY")
    history_compress_enabled: bool = Field(default=False, alias="HISTORY_COMPRESS_ENABLED")
    history_keep_recent_tool_results: int = Field(
        default=8,
        alias="HISTORY_KEEP_RECENT_TOOL_RESULTS",
    )

    # --- Observabilidade OTel (inversão: instrumentação na app, não no gateway
    # LiteLLM). O endpoint/protocolo do exporter vêm das envs padrão do OTel
    # (OTEL_EXPORTER_OTLP_ENDPOINT etc.), lidas direto pelo SDK. ---
    # Gate: permite rodar CLI/CI local sem exigir um Collector no ar.
    otel_enabled: bool = Field(default=True, alias="OTEL_ENABLED")
    # service.name distingue os domínios no projeto único do Langfuse. MESMO
    # código serve triagem e QA; cada Deployment seta o seu (triage-agent/qa-bot).
    otel_service_name: str = Field(default="the-lab-zone-agent", alias="OTEL_SERVICE_NAME")
    otel_environment: str = Field(default="prod", alias="OTEL_ENVIRONMENT")
    # version FIXA do semconv GenAI do pydantic-ai: não depender do default, que
    # muda entre releases (2-4 são compat deprecado; 5 é o atual em 2.1.0).
    # Literal para casar com o param `version` do InstrumentationSettings (mypy);
    # BeforeValidator(int) coage o env string e rejeita valor fora de {2,3,4,5}
    # no boot (parsing estrito da casa).
    otel_semconv_version: Annotated[Literal[2, 3, 4, 5], BeforeValidator(int)] = Field(
        default=5, alias="OTEL_SEMCONV_VERSION"
    )

    log_level: int = Field(default=logging.INFO, alias="LOG_LEVEL")

    @field_validator("log_level", mode="before")
    @classmethod
    def _normalize_log_level(cls, value: str | int) -> int:
        if isinstance(value, int):
            return value

        mapping = logging.getLevelNamesMapping()
        normalized = mapping.get(value.upper())
        if isinstance(normalized, int):
            return normalized

        raise ValueError(f"invalid log level: {value}")

    @field_validator("history_keep_recent_tool_results")
    @classmethod
    def _validate_history_keep_recent_tool_results(cls, value: int) -> int:
        if value < 0:
            raise ValueError("history_keep_recent_tool_results must be >= 0")
        return value

    @field_validator("agent_max_concurrency")
    @classmethod
    def _validate_agent_max_concurrency(cls, value: int | None) -> int | None:
        if value is not None and value <= 0:
            raise ValueError("agent_max_concurrency must be > 0")
        return value


@lru_cache
def get_settings() -> Settings:
    return Settings()  # type: ignore[call-arg]
