"""Testes do carregamento de system prompt (shared.prompts.load_prompt).

O override via SYSTEM_PROMPT_PATH é o que permite iterar no prompt via
ConfigMap sem rebuildar a imagem — se quebrar, o prompt do cluster para de
ter efeito silenciosamente. Vale travar.
"""

from pathlib import Path

import pytest

from shared.prompts import load_prompt


def test_reads_default_path_and_strips(tmp_path: Path):
    p = tmp_path / "default.md"
    p.write_text("  conteúdo do prompt\n\n", encoding="utf-8")
    assert load_prompt(p) == "conteúdo do prompt"


def test_env_override_wins_over_default(tmp_path: Path, monkeypatch):
    default = tmp_path / "default.md"
    default.write_text("DEFAULT", encoding="utf-8")
    override = tmp_path / "override.md"
    override.write_text("OVERRIDE", encoding="utf-8")

    monkeypatch.setenv("SYSTEM_PROMPT_PATH", str(override))
    # Mesmo passando o default, o env deve vencer (contrato do ConfigMap).
    assert load_prompt(default) == "OVERRIDE"


def test_missing_file_raises(tmp_path: Path):
    # Falha explícita é melhor que prompt vazio silencioso — um prompt
    # ausente deve quebrar o boot, não degradar o agente sem aviso.
    with pytest.raises(FileNotFoundError):
        load_prompt(tmp_path / "nao-existe.md")


def test_utf8_content_preserved(tmp_path: Path):
    # Os prompts têm acentuação (PT-BR) e emoji; o encoding não pode corromper.
    p = tmp_path / "acentos.md"
    p.write_text("investigação de incidentes 🔍 é ótima", encoding="utf-8")
    assert load_prompt(p) == "investigação de incidentes 🔍 é ótima"
