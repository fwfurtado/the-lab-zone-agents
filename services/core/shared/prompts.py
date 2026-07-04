import os
from pathlib import Path


def load_prompt(default_path: Path) -> str:
    """Carrega um system prompt de arquivo.

    SYSTEM_PROMPT_PATH (env) sobrescreve o default — permite montar via
    ConfigMap no cluster e iterar no prompt sem rebuildar a imagem. O default
    aponta para o .md ao lado do agente (vai como package-data na imagem).
    """
    path = Path(os.environ.get("SYSTEM_PROMPT_PATH", default_path))
    return path.read_text(encoding="utf-8").strip()
