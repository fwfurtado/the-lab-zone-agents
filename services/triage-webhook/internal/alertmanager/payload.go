// Package alertmanager modela o payload do webhook (versão 4) e o traduz para
// o que o pipeline precisa: uma chave de deduplicação estável e o contexto
// textual entregue ao núcleo de triagem.
package alertmanager

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Payload é o corpo do webhook do Alertmanager (data version 4).
type Payload struct {
	Version           string            `json:"version"`
	GroupKey          string            `json:"groupKey"`
	Status            string            `json:"status"` // firing | resolved (do grupo)
	Receiver          string            `json:"receiver"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
	Alerts            []Alert           `json:"alerts"`
}

// Alert é um alerta individual dentro do grupo.
type Alert struct {
	Status      string            `json:"status"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
	StartsAt    time.Time         `json:"startsAt"`
	EndsAt      time.Time         `json:"endsAt"`
	Fingerprint string            `json:"fingerprint"`
}

// maxBody limita o corpo aceito do webhook. Um grupo com max_alerts: 20 fica
// muito abaixo disto; acima é payload malformado ou abuso.
const maxBody = 1 << 20 // 1 MiB

// Parse decodifica e valida o payload a partir do corpo da requisição.
//
// Tolerante a campos desconhecidos de propósito (sem DisallowUnknownFields):
// o Alertmanager pode ganhar campos novos no v4 sem quebrar a borda. A
// validação dura é estrutural (groupKey e alerts presentes); a versão
// divergente é responsabilidade do chamador logar, não abortar.
func Parse(r io.Reader) (*Payload, error) {
	dec := json.NewDecoder(io.LimitReader(r, maxBody))

	var p Payload
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("decodificando payload: %w", err)
	}
	if p.GroupKey == "" {
		return nil, fmt.Errorf("payload sem groupKey")
	}
	if len(p.Alerts) == 0 {
		return nil, fmt.Errorf("payload sem alertas")
	}
	return &p, nil
}

// Firing devolve só os alertas em estado firing.
func (p *Payload) Firing() []Alert {
	out := make([]Alert, 0, len(p.Alerts))
	for _, a := range p.Alerts {
		if a.Status == "firing" {
			out = append(out, a)
		}
	}
	return out
}

// DedupKey deriva a chave de deduplicação do grupo: groupKey + o conjunto
// ordenado de (fingerprint, startsAt) dos alertas firing.
//
// Propriedades desejadas:
//   - Reenvio idêntico (repeat_interval sem mudança) → mesma chave → dedup.
//   - Alerta NOVO entrando no grupo (group_interval) → chave muda → re-triagem
//     com o contexto mais completo.
//   - Mesmo alertname re-disparando depois de resolver → startsAt muda → chave
//     muda → triagem nova (é um incidente novo).
func (p *Payload) DedupKey() string {
	firing := p.Firing()
	parts := make([]string, 0, len(firing))
	for _, a := range firing {
		parts = append(parts, a.Fingerprint+"|"+a.StartsAt.UTC().Format(time.RFC3339))
	}
	sort.Strings(parts)
	h := sha256.Sum256([]byte(p.GroupKey + "\n" + strings.Join(parts, "\n")))
	return hex.EncodeToString(h[:16])
}

// Facts são os fatos estruturados do grupo de alertas, extraídos VERBATIM do
// payload (Fase D). Diferente de Summary/RenderContext, que produzem texto,
// Facts carrega os campos crus para o front-matter da persistência — sem
// achatar em prosa e sem passar por nenhuma heurística. São autoritativos: o
// diagnóstico do modelo nunca é fonte destes valores.
type Facts struct {
	// Alertnames são os alertnames distintos do grupo, na ordem de aparição.
	// Lista porque um grupo pode conter mais de um alertname.
	Alertnames []string
	// Namespace é o namespace representante do grupo (GroupLabels, com fallback
	// para CommonLabels). Vazio se o grupo não tem namespace.
	Namespace string
	// FiredAt é o startsAt do alerta firing mais antigo — o início do incidente.
	FiredAt time.Time
}

// Facts extrai os fatos estruturados do grupo firing. Reusa a mesma regra de
// "namespace representante" do Summary (GroupLabels → CommonLabels), para os
// dois não divergirem.
func (p *Payload) Facts() Facts {
	firing := p.Firing()

	var names []string
	seen := make(map[string]bool)
	var oldest time.Time
	for _, a := range firing {
		if n := a.Labels["alertname"]; n != "" && !seen[n] {
			seen[n] = true
			names = append(names, n)
		}
		if oldest.IsZero() || a.StartsAt.Before(oldest) {
			oldest = a.StartsAt
		}
	}

	// Namespace representante do grupo, em ordem de preferência:
	//   1. GroupLabels — o eixo pelo qual o Alertmanager agrupou (group_by).
	//   2. CommonLabels — comum a todos os alertas do grupo.
	//   3. Labels do primeiro alerta firing — presente em quase todo alerta k8s
	//      mesmo quando o grupo NÃO é por namespace (ex.: group_by: [alertname]).
	// Sem o passo 3, um grupo que não agrupa por namespace produzia namespace
	// vazio, e a chave de persistência degradava para triage/no-namespace/... —
	// quebrando o prefixo do Tier 0. O fallback fecha esse buraco para alertas
	// reais (o passo 3 só não salva um payload que não tem namespace em lugar
	// nenhum, como um curl de teste sintético).
	ns := p.GroupLabels["namespace"]
	if ns == "" {
		ns = p.CommonLabels["namespace"]
	}
	if ns == "" && len(firing) > 0 {
		ns = firing[0].Labels["namespace"]
	}

	return Facts{Alertnames: names, Namespace: ns, FiredAt: oldest}
}

// RenderContext monta o texto entregue ao núcleo de triagem. Só fatos do
// alerta — o formato do diagnóstico é responsabilidade do system prompt do
// agente, não daqui.
func (p *Payload) RenderContext() string {
	firing := p.Firing()
	var sb strings.Builder
	fmt.Fprintf(&sb, "Grupo de alertas do Alertmanager (%d firing).\n", len(firing))
	if len(p.GroupLabels) > 0 {
		fmt.Fprintf(&sb, "Agrupado por: %s\n", renderLabels(p.GroupLabels))
	}
	sb.WriteString("\n")

	for i, a := range firing {
		name := a.Labels["alertname"]
		if name == "" {
			name = "(sem alertname)"
		}
		fmt.Fprintf(&sb, "[%d] %s\n", i+1, name)
		fmt.Fprintf(&sb, "    desde: %s\n", a.StartsAt.UTC().Format(time.RFC3339))
		if v := a.Annotations["summary"]; v != "" {
			fmt.Fprintf(&sb, "    summary: %s\n", v)
		}
		if v := a.Annotations["description"]; v != "" {
			fmt.Fprintf(&sb, "    description: %s\n", v)
		}
		fmt.Fprintf(&sb, "    labels: %s\n", renderLabels(a.Labels))
	}

	sb.WriteString("\nInvestigue e produza o diagnóstico triado deste(s) alerta(s).")
	return sb.String()
}

// Summary devolve um resumo curto e legível do grupo, para o título da
// notificação (Slack). Prioriza o que um humano quer ver ao bater o olho:
// os alertnames distintos e o namespace, não o groupKey de máquina.
//
// Exemplos: "KubePodCrashLooping em ai", "TargetDown, TooManyLogs em
// observability", "3 alertas". Vazio nunca — sempre há ao menos a contagem.
func (p *Payload) Summary() string {
	firing := p.Firing()
	if len(firing) == 0 {
		return "sem alertas firing"
	}

	// Alertnames distintos, preservando ordem de aparição.
	var names []string
	seen := make(map[string]bool)
	for _, a := range firing {
		n := a.Labels["alertname"]
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}

	// Namespace do agrupamento (group_by: [namespace]) ou o common label.
	ns := p.GroupLabels["namespace"]
	if ns == "" {
		ns = p.CommonLabels["namespace"]
	}

	var head string
	switch {
	case len(names) == 0:
		head = fmt.Sprintf("%d alertas", len(firing))
	case len(names) <= 3:
		head = strings.Join(names, ", ")
	default:
		head = fmt.Sprintf("%s e +%d", strings.Join(names[:3], ", "), len(names)-3)
	}
	if ns != "" {
		return head + " em " + ns
	}
	return head
}

// renderLabels serializa labels de forma determinística (ordenadas por chave).
func renderLabels(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%q", k, labels[k]))
	}
	return strings.Join(parts, " ")
}
