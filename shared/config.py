from functools import lru_cache
import logging

from pydantic import Field, SecretStr
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

    # Limites RÍGIDOS de runtime do agente. Regra de prompt pede contenção;
    # estes garantem. Defesa contra tool que despeja resultado gigante e
    # contra loop de tools estourando o contexto/custo.
    max_tool_result_chars: int = Field(default=60_000, alias="MAX_TOOL_RESULT_CHARS")
    agent_request_limit: int = Field(default=25, alias="AGENT_REQUEST_LIMIT")
    agent_tool_calls_limit: int = Field(default=40, alias="AGENT_TOOL_CALLS_LIMIT")
    agent_total_tokens_limit: int = Field(default=600_000, alias="AGENT_TOTAL_TOKENS_LIMIT")

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


@lru_cache
def get_settings() -> Settings:
    return Settings() # type: ignore[call-arg]
