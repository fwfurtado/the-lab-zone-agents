package publish

import (
	"strings"
	"testing"
	"time"
)

func sampleReport() Report {
	return Report{
		DedupKey:   "a3f9c1d2e4b6a8c0",
		GroupKey:   `{}:{alertname="VeleroBackupStale"}`,
		Summary:    "VeleroBackupStale em velero",
		Diagnosis:  "# Triagem\n\n## Sintoma\nBackup travado.\n",
		Alertnames: []string{"VeleroBackupStale"},
		Namespace:  "velero",
		FiredAt:    time.Date(2026, 7, 5, 1, 16, 20, 0, time.UTC),
		TriagedAt:  time.Date(2026, 7, 5, 1, 39, 0, 0, time.UTC),
	}
}

func TestObjectKey(t *testing.T) {
	got := objectKey(sampleReport())
	want := "triage/velero/VeleroBackupStale/20260705T011620Z__a3f9c1d2e4b6a8c0.md"
	if got != want {
		t.Errorf("objectKey:\n got %q\nwant %q", got, want)
	}
}

func TestObjectKeyIdempotentePorDedup(t *testing.T) {
	// Mesmo incidente (mesmo dedupKey + firedAt) → mesma chave → PUT sobrescreve.
	r := sampleReport()
	// Incidente novo (dedupKey diferente) → chave diferente.
	r2 := sampleReport()
	r2.DedupKey = "ffffffffffffffff"
	if objectKey(r) == objectKey(r2) {
		t.Error("dedupKeys diferentes deveriam gerar chaves diferentes")
	}
}

func TestObjectKeySanitizaSegmentos(t *testing.T) {
	r := sampleReport()
	r.Namespace = "kube system/weird"
	r.Alertnames = []string{"Target Down!"}
	got := objectKey(r)
	if strings.Contains(got, " ") || strings.Contains(got, "!") {
		t.Errorf("segmentos não sanitizados: %q", got)
	}
	// A barra do namespace não deve criar um nível extra de "diretório".
	if strings.Count(got, "/") != 3 { // triage / ns / alertname / arquivo
		t.Errorf("estrutura de chave inesperada: %q", got)
	}
}

func TestObjectKeyCamposAusentes(t *testing.T) {
	r := Report{DedupKey: "abc123"} // sem namespace, alertname, firedAt
	got := objectKey(r)
	want := "triage/no-namespace/no-alertname/no-firedat__abc123.md"
	if got != want {
		t.Errorf("objectKey com campos ausentes:\n got %q\nwant %q", got, want)
	}
}

func TestBuildDocumentFrontMatter(t *testing.T) {
	doc := buildDocument(sampleReport())

	// Estrutura: front-matter delimitado, corpo depois.
	if !strings.HasPrefix(doc, "---\n") {
		t.Fatal("documento deve começar com abertura de front-matter")
	}
	if strings.Count(doc, "---\n") < 2 {
		t.Fatal("front-matter deve ter abertura e fechamento")
	}

	mustContain := []string{
		"schema: 1",
		`incident_key: "a3f9c1d2e4b6a8c0"`,
		`dedup_key: "a3f9c1d2e4b6a8c0"`,
		`namespace: "velero"`,
		`fired_at: "2026-07-05T01:16:20Z"`,
		`triaged_at: "2026-07-05T01:39:00Z"`,
		"confirmation: unverified", // perecível, nasce vazio
		"- \"VeleroBackupStale\"",
	}
	for _, s := range mustContain {
		if !strings.Contains(doc, s) {
			t.Errorf("front-matter não contém %q\n---\n%s", s, doc)
		}
	}

	// O corpo íntegro é preservado após o front-matter.
	if !strings.Contains(doc, "## Sintoma\nBackup travado.") {
		t.Error("corpo do diagnóstico não preservado")
	}
}

func TestBuildDocumentSemVerdictConfidence(t *testing.T) {
	// Decisão da Fase D: v1 NÃO persiste verdict/confidence (entram no job de
	// embed, via classificador). O front-matter não deve mencioná-los.
	doc := buildDocument(sampleReport())
	for _, forbidden := range []string{"verdict:", "confidence:"} {
		if strings.Contains(doc, forbidden) {
			t.Errorf("front-matter não deveria conter %q na v1", forbidden)
		}
	}
}

func TestBuildDocumentTimestampZeroVazio(t *testing.T) {
	r := sampleReport()
	r.FiredAt = time.Time{} // zero
	doc := buildDocument(r)
	if strings.Contains(doc, "0001-01-01") {
		t.Error("timestamp zero deveria virar string vazia, não 0001-01-01")
	}
	if !strings.Contains(doc, `fired_at: ""`) {
		t.Error("fired_at zero deveria ser aspas vazias")
	}
}

func TestBuildDocumentAlertnamesVazio(t *testing.T) {
	r := sampleReport()
	r.Alertnames = nil
	doc := buildDocument(r)
	if !strings.Contains(doc, "alertnames: []") {
		t.Error("alertnames vazio deveria ser lista YAML vazia")
	}
}

func TestYAMLStringEscapa(t *testing.T) {
	// group_key do Alertmanager tem aspas e chaves — não pode quebrar o YAML.
	r := sampleReport()
	doc := buildDocument(r)
	// A aspa dupla dentro do group_key deve estar escapada.
	if !strings.Contains(doc, `\"VeleroBackupStale\"`) {
		t.Errorf("aspas no group_key não escapadas:\n%s", doc)
	}
}
