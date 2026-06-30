FROM python:3.12-slim

ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    PIP_NO_CACHE_DIR=1

WORKDIR /app

COPY pyproject.toml README.md ./
COPY bot ./bot
COPY agent.py config.py log.py main.py metrics.py ./
COPY prompts ./prompts

RUN pip install .

ENV SYSTEM_PROMPT_PATH=/app/prompts/system.md

# cria um usuário não-root e passa a rodar como ele
RUN useradd --uid 10001 --create-home --shell /usr/sbin/nologin appuser
USER 10001

CMD ["slack-bot"]
