package publish

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Este arquivo é a lógica PURA da persistência da Fase D: montar o documento
// (front-matter YAML + corpo) e a chave do objeto a partir de um Report. É
// stdlib-only e sem I/O de propósito — o GaragePublisher (garage.go) cuida do
// transporte S3; aqui fica o que tem regra e merece teste determinístico.

// frontMatterSchema versiona o FORMATO do front-matter. Sobe quando os campos
// mudam, para o job de embed (Fase D, próximo passo) saber ler versões antigas.
const frontMatterSchema = 1

// keySanitize troca por '-' tudo que não seja seguro num segmento de chave S3
// (o esquema é triage/{namespace}/{alertname}/{ts}__{dedup}.md). Alertnames e
// namespaces do k8s já são conservadores, mas um label inesperado não deve
// quebrar a chave nem criar um "diretório" acidental.
var keySanitize = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)

func sanitizeSegment(s string) string {
	s = keySanitize.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "unknown"
	}
	return s
}

// objectKey deriva a chave do objeto no bucket.
//
//	triage/{namespace}/{alertname}/{firedAt}__{dedupKey}.md
//
// Decisões (deliberação da Fase D):
//   - prefixo {namespace}/{alertname} torna o Tier 0 um list-objects por
//     prefixo ("já triei VeleroBackupStale em velero?") sem tocar no Qdrant;
//   - {firedAt} ordena temporalmente dentro do prefixo (recência = sinal);
//   - {dedupKey} no sufixo é a identidade estável do incidente: reenvio
//     idêntico → mesma chave → PUT idempotente sobrescreve (não polui o corpus
//     com quase-duplicatas); incidente novo → dedupKey diferente → objeto novo.
//
// alertname/namespace representantes vêm de r.Alertnames[0]/r.Namespace; um
// grupo multi-alerta usa o primeiro alertname como eixo navegável (a unicidade
// real é do dedupKey, não do prefixo).
func objectKey(r Report) string {
	ns := "no-namespace"
	if r.Namespace != "" {
		ns = sanitizeSegment(r.Namespace)
	}
	alertname := "no-alertname"
	if len(r.Alertnames) > 0 && r.Alertnames[0] != "" {
		alertname = sanitizeSegment(r.Alertnames[0])
	}
	// Timestamp compacto e ordenável (sem ':' que é ruído em nome de objeto).
	ts := r.FiredAt.UTC().Format("20060102T150405Z")
	if r.FiredAt.IsZero() {
		ts = "no-firedat"
	}
	return fmt.Sprintf("triage/%s/%s/%s__%s.md", ns, alertname, ts, r.DedupKey)
}

// buildDocument monta o .md persistido: front-matter YAML + corpo íntegro.
//
// O front-matter carrega SÓ fatos de alerta (verbatim, autoritativos) mais o
// gancho perecível `confirmation`. Sem verdict/confidence: essas conclusões do
// modelo entram quando o job de embed existir (extração via classificador, ADR
// à parte) — o corpo íntegro abaixo é a fonte da qual serão regeneradas.
//
// `confirmation: unverified` nasce em TODO relatório: é o gancho da qualidade
// de memória (Fase D, decisão 4). "Esse diagnóstico estava certo?" é
// conhecimento que decai rápido; o campo tem que existir desde o primeiro
// relatório, mesmo sem mecanismo de escrita ainda.
func buildDocument(r Report) string {
	var sb strings.Builder
	sb.WriteString("---\n")
	fmt.Fprintf(&sb, "schema: %d\n", frontMatterSchema)
	fmt.Fprintf(&sb, "incident_key: %s\n", yamlString(r.DedupKey))
	fmt.Fprintf(&sb, "dedup_key: %s\n", yamlString(r.DedupKey))
	fmt.Fprintf(&sb, "group_key: %s\n", yamlString(r.GroupKey))
	sb.WriteString("alertnames:")
	if len(r.Alertnames) == 0 {
		sb.WriteString(" []\n")
	} else {
		sb.WriteString("\n")
		names := append([]string(nil), r.Alertnames...)
		sort.Strings(names)
		for _, n := range names {
			fmt.Fprintf(&sb, "  - %s\n", yamlString(n))
		}
	}
	fmt.Fprintf(&sb, "namespace: %s\n", yamlString(r.Namespace))
	fmt.Fprintf(&sb, "fired_at: %s\n", yamlTimestamp(r.FiredAt))
	fmt.Fprintf(&sb, "triaged_at: %s\n", yamlTimestamp(r.TriagedAt))
	fmt.Fprintf(&sb, "summary: %s\n", yamlString(r.Summary))
	// Perecível, nasce vazio — gancho da decisão 4 (qualidade de memória).
	sb.WriteString("confirmation: unverified\n")
	sb.WriteString("---\n\n")
	sb.WriteString(r.Diagnosis)
	if !strings.HasSuffix(r.Diagnosis, "\n") {
		sb.WriteString("\n")
	}
	return sb.String()
}

// yamlString serializa uma string como escalar YAML seguro. Sempre entre aspas
// duplas com escape mínimo: evita que ':' , '#', '-' líderes ou strings vazias
// virem YAML ambíguo. Não é um encoder YAML geral — cobre o que o front-matter
// precisa (strings de uma linha), de forma determinística e sem dependência.
func yamlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// yamlTimestamp serializa um instante como string ISO-8601 UTC entre aspas, ou
// "" (aspas vazias) quando zero — nunca o "0001-01-01" do time.Time zero, que
// poluiria consultas por data.
func yamlTimestamp(t time.Time) string {
	if t.IsZero() {
		return `""`
	}
	return yamlString(t.UTC().Format(time.RFC3339))
}
