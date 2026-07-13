"""O artefato de confirmação: schema e serialização (ADR-0014).

Uma confirmação é FEEDBACK HUMANO sobre um diagnóstico já triado — se o mundo
validou (`confirmed`) ou desmentiu (`refuted`) o `verdict` do classificador
(ADR-0013). Vive num `.md` irmão no Garage, sob o prefixo `confirmations/`,
mesma chave que `triage/` e `conclusions/` espelham (`{ns}/{alert}/{fired_at}__
{dedup_key}.md`).

Terceiro prefixo, não o mesmo artefato de `conclusions/`: aquele é regenerável
por LLM a qualquer `--reclassify` (PUT cego, ADR-0013); confirmação é feedback
humano, caro de obter, que uma reclassificação automática NUNCA pode apagar.
Prefixo próprio é isolamento por construção — o glob do classificador não varre
`confirmations/`, não é preciso confiar em convenção.

O escritor daqui é o listener de feedback do Slack (`shared/slack/
triage_feedback.py`), não o classificador — por isso este módulo vive no repo
`the-lab-zone-agents`, não no `the-lab-zone-dockerfiles` (onde `conclusions.py`
mora). MAS o `.md` emitido precisa ser lido pelo MESMO parser restrito do
indexer (`triage_indexer._common.parse_document`, sem PyYAML) — por isso o
front-matter carrega só valores que NUNCA precisam de escaping (enum, ISO
timestamp, Slack user id — todos ASCII, sem aspas/dois-pontos/quebra de linha)
e o texto livre (`note`, digitado por humano, pode ter qualquer caractere) fica
SÓ no corpo, como o `rationale` de `conclusions.py`. Essa divisão evita
duplicar `_yaml_quote`/`_unescape` neste repo — e evita reeditar o bug de
ordem de substituição que já mordeu o `verdict` do classificador uma vez.
"""

from __future__ import annotations

from datetime import UTC, datetime
from typing import Literal

from pydantic import BaseModel, Field

SCHEMA_VERSION = 1

CONFIRMATIONS_PREFIX = "confirmations"

_NOTE_HEADING = "## Motivo"

# Teto do campo de texto do modal. Aplicado pela própria UI do Slack
# (`plain_text_input.max_length`) — o Slack recusa o submit acima disso, sem
# round-trip: diferente do `verdict` do classificador, aqui não há LLM
# tentando de novo, então não há custo de retry a proteger. O valor aqui é
# só a mesma constante refletida, para o schema documentar o contrato.
NOTE_MAX_CHARS = 500


class Confirmation(BaseModel):
    """Feedback humano sobre um diagnóstico. `via` é o gancho de extensão: os
    sinais futuros da B.2 (recorrência de alerta, correlação com PR) escrevem
    o MESMO artefato com `via` diferente — quem lê (indexer, agente) não
    precisa saber a origem, só o valor de `confirmation`."""

    # Padrões (não só docstring) garantem que estes dois campos NUNCA precisam
    # de escaping YAML — é o que permite front-matter sem aspas e sem duplicar
    # `_yaml_quote`/`_unescape` neste repo. Constranger a FORMA, não confiar em
    # promessa narrativa (a mesma lição do teto do verdict, ADR-0013).
    dedup_key: str = Field(pattern=r"^[0-9a-f]{8,64}$")
    confirmation: Literal["confirmed", "refuted"]
    confirmed_by: str = Field(
        pattern=r"^[UW][A-Z0-9]{2,20}$",
        description="Slack user id (ex.: U0123ABC). Não resolvido a nome aqui "
        "— resolver na leitura evita ficar desatualizado se a pessoa renomear.",
    )
    confirmed_at: datetime
    note: str | None = Field(default=None, max_length=NOTE_MAX_CHARS)
    via: Literal["slack_modal"] = "slack_modal"


def _bare(value: str) -> str:
    """Emite um escalar SEM aspas — só para valores que o chamador garante
    serem seguros (enum, id alfanumérico, timestamp ISO). Nunca usar para
    texto livre; é isso que `note` evita precisar."""
    return value


def confirmation_path_for(triage_key: str) -> str:
    """`triage_key` é o sufixo que a borda Go calcula (`reportSuffix`) e o
    botão do Slack carrega no `value` — mesma chave que `conclusion_path_for`
    espelha no indexer (ADR-0013), aqui reaplicada ao terceiro prefixo. Não
    reconstrói a chave a partir de partes soltas; recebe o sufixo pronto.
    """
    if not triage_key or triage_key.startswith("/"):
        raise ValueError(f"triage_key inválido: {triage_key!r}")
    return f"{CONFIRMATIONS_PREFIX}/{triage_key}"


def to_markdown(c: Confirmation) -> str:
    """Serializa no MESMO YAML restrito que `triage_indexer._common.
    parse_document` lê (sem PyYAML). Todo campo do front-matter é um escalar
    seguro sem aspas — ver docstring do módulo. `note` vai só no corpo.
    """
    lines = [
        "---",
        f"schema: {SCHEMA_VERSION}",
        f"dedup_key: {_bare(c.dedup_key)}",
        f"confirmation: {c.confirmation}",
        f"confirmed_by: {_bare(c.confirmed_by)}",
        f"confirmed_at: {c.confirmed_at.astimezone(UTC).strftime('%Y-%m-%dT%H:%M:%SZ')}",
        f"via: {c.via}",
        "---",
        "",
        _NOTE_HEADING,
        "",
        (c.note or "").strip(),
        "",
    ]
    return "\n".join(lines)
