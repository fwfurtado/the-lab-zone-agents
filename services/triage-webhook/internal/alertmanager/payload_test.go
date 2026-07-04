package alertmanager

import (
	"strings"
	"testing"
	"time"
)

const sample = `{
  "version": "4",
  "groupKey": "{}/{triage=\"true\"}:{namespace=\"ai\"}",
  "status": "firing",
  "receiver": "triage-webhook",
  "groupLabels": {"namespace": "ai"},
  "commonLabels": {"namespace": "ai", "severity": "warning"},
  "commonAnnotations": {},
  "externalURL": "http://alertmanager",
  "alerts": [
    {
      "status": "firing",
      "labels": {"alertname": "KubePodCrashLooping", "namespace": "ai", "pod": "open-webui-0"},
      "annotations": {"summary": "Pod em crashloop", "description": "open-webui-0 reiniciou 7x"},
      "startsAt": "2026-07-03T18:00:00Z",
      "endsAt": "0001-01-01T00:00:00Z",
      "fingerprint": "abc123"
    },
    {
      "status": "resolved",
      "labels": {"alertname": "KubePodNotReady", "namespace": "ai"},
      "annotations": {},
      "startsAt": "2026-07-03T17:00:00Z",
      "endsAt": "2026-07-03T17:30:00Z",
      "fingerprint": "def456"
    }
  ]
}`

func parseSample(t *testing.T) *Payload {
	t.Helper()
	p, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatalf("parse falhou: %v", err)
	}
	return p
}

func TestParseAndFiring(t *testing.T) {
	p := parseSample(t)
	if got := len(p.Firing()); got != 1 {
		t.Fatalf("esperava 1 firing, veio %d", got)
	}
}

func TestParseRejectsEmpty(t *testing.T) {
	if _, err := Parse(strings.NewReader(`{"version":"4","groupKey":"g","alerts":[]}`)); err == nil {
		t.Fatal("payload sem alertas deveria falhar")
	}
	if _, err := Parse(strings.NewReader(`{"version":"4","alerts":[{"status":"firing"}]}`)); err == nil {
		t.Fatal("payload sem groupKey deveria falhar")
	}
}

func TestDedupKeyProperties(t *testing.T) {
	p := parseSample(t)
	k1 := p.DedupKey()

	// Determinística: mesmo payload → mesma chave.
	if k2 := parseSample(t).DedupKey(); k1 != k2 {
		t.Fatalf("chave não determinística: %s vs %s", k1, k2)
	}

	// startsAt novo (re-disparo) → chave nova (é um incidente novo).
	p.Alerts[0].StartsAt = p.Alerts[0].StartsAt.Add(time.Hour)
	if k3 := p.DedupKey(); k3 == k1 {
		t.Fatal("startsAt diferente deveria mudar a chave")
	}

	// Alerta resolved não participa da chave.
	q := parseSample(t)
	q.Alerts[1].EndsAt = q.Alerts[1].EndsAt.Add(time.Hour)
	if k4 := q.DedupKey(); k4 != k1 {
		t.Fatal("mudança em alerta resolved não deveria mudar a chave")
	}
}

func TestRenderContextCarriesFacts(t *testing.T) {
	got := parseSample(t).RenderContext()
	for _, want := range []string{"KubePodCrashLooping", "open-webui-0", "Pod em crashloop", "2026-07-03T18:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Fatalf("contexto não contém %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "KubePodNotReady") {
		t.Fatal("alerta resolved não deveria entrar no contexto")
	}
}

func TestSummaryReadable(t *testing.T) {
	p := parseSample(t)
	// sample tem KubePodCrashLooping (firing) + KubePodNotReady (resolved);
	// só o firing conta, e o namespace do groupLabels é "ai".
	got := p.Summary()
	if got != "KubePodCrashLooping em ai" {
		t.Fatalf("summary inesperado: %q", got)
	}
}

func TestSummaryDedupsNamesAndTruncates(t *testing.T) {
	p := &Payload{
		GroupLabels: map[string]string{"namespace": "observability"},
		Alerts: []Alert{
			{Status: "firing", Labels: map[string]string{"alertname": "A"}},
			{Status: "firing", Labels: map[string]string{"alertname": "A"}}, // duplicado
			{Status: "firing", Labels: map[string]string{"alertname": "B"}},
			{Status: "firing", Labels: map[string]string{"alertname": "C"}},
			{Status: "firing", Labels: map[string]string{"alertname": "D"}},
		},
	}
	// A dedup + 4 nomes distintos (A,B,C,D) => 3 + "+1".
	got := p.Summary()
	if got != "A, B, C e +1 em observability" {
		t.Fatalf("truncamento inesperado: %q", got)
	}
}

func TestSummaryWithoutNamespace(t *testing.T) {
	p := &Payload{
		Alerts: []Alert{{Status: "firing", Labels: map[string]string{"alertname": "Solo"}}},
	}
	if got := p.Summary(); got != "Solo" {
		t.Fatalf("sem namespace deveria ser só o nome: %q", got)
	}
}
