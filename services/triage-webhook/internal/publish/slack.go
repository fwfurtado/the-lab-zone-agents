package publish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// slackPostURL é o endpoint da Web API do Slack para postar mensagens.
const slackPostURL = "https://slack.com/api/chat.postMessage"

// SlackPublisher posta o diagnóstico de triagem num canal do Slack.
//
// Usa o bloco markdown NATIVO do Slack ({"type":"markdown"}) — não mrkdwn nem
// section — que é o que renderiza ##, **bold**, listas e code fences
// corretamente. Mesma escolha do slack-qa-bot (foi ela que resolveu os bugs
// de rendering); a diferença é só o transporte (Web API direta, sem SDK).
//
// Canal dedicado (#triage) em vez do #alerts: o diagnóstico não compete com o
// ruído de alerta, e o link pro Alertmanager amarra de volta ao incidente —
// o que dispensa correlação por thread com a mensagem de alerta original.
type SlackPublisher struct {
	token       string
	channel     string
	externalURL string // base do Alertmanager, pro link de volta ao alerta
	postURL     string
	hc          *http.Client
	log         *slog.Logger
}

// NewSlack cria o publisher. externalURL pode ser vazio (omite o link).
func NewSlack(token, channel, externalURL string, log *slog.Logger) *SlackPublisher {
	return &SlackPublisher{
		token:       token,
		channel:     channel,
		externalURL: externalURL,
		postURL:     slackPostURL,
		hc:          &http.Client{Timeout: 15 * time.Second},
		log:         log,
	}
}

type slackBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// actionsBlock é o bloco de botões de feedback (ADR-0014), anexado só na
// mensagem RAIZ (não nos chunks do diagnóstico na thread). Forma própria —
// não cabe em slackBlock (que só tem type+text).
type actionsBlock struct {
	Type     string        `json:"type"`
	BlockID  string        `json:"block_id"`
	Elements []slackButton `json:"elements"`
}

type slackButton struct {
	Type     string          `json:"type"`
	ActionID string          `json:"action_id"`
	Text     slackButtonText `json:"text"`
	Style    string          `json:"style,omitempty"`
	Value    string          `json:"value"`
}

type slackButtonText struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Emoji bool   `json:"emoji,omitempty"`
}

// feedbackButtonValue é o contexto que o botão carrega consigo (ADR-0014). O
// listener (Python, no processo do slack-qa-bot) o lê de volta no clique —
// sem índice reverso: o próprio botão sabe a que incidente pertence.
type feedbackButtonValue struct {
	DedupKey  string `json:"dedup_key"`
	TriageKey string `json:"triage_key"` // reportSuffix(r); mesma chave que conclusions/ e confirmations/ espelham
}

type slackPostRequest struct {
	Channel  string `json:"channel"`
	Text     string `json:"text"` // fallback p/ notificação e acessibilidade
	Blocks   []any  `json:"blocks,omitempty"`
	ThreadTS string `json:"thread_ts,omitempty"`
}

type slackPostResponse struct {
	OK    bool   `json:"ok"`
	TS    string `json:"ts"`
	Error string `json:"error"`
}

// Publish posta uma mensagem raiz padronizada no canal e envia o diagnóstico
// completo dentro da thread dessa raiz. Assim o canal não fica dependente do
// preview/corte da primeira mensagem longa.
//
// Falha de publicação retorna erro (o pipeline loga), mas NÃO deve liberar a
// chave de dedup: a triagem aconteceu, é problema de entrega. Isso já é
// respeitado no pipeline — só a falha de Triage libera a chave.
func (p *SlackPublisher) Publish(ctx context.Context, r Report) error {
	header := p.header(r)
	chunks := splitMarkdown(r.Diagnosis, maxSlackChars)
	if len(chunks) == 0 {
		chunks = []string{"_(diagnóstico vazio)_"}
	}

	rootTS, err := p.postRoot(ctx, r)
	if err != nil {
		return fmt.Errorf("postando notificação de triagem no Slack: %w", err)
	}

	// Diagnóstico na thread da raiz. Uma vez que a raiz foi postada, a
	// entrega "aconteceu" do ponto de vista do pipeline (a dedup não é
	// liberada). Falha de chunk é entrega PARCIAL: logada, não abortiva —
	// abortar aqui deixaria a raiz órfã no canal ("Diagnóstico completo na
	// thread") apontando pra uma thread vazia. A raiz é a única post dura.
	chunks[0] = header + "\n\n" + chunks[0]
	for i, chunk := range chunks {
		if _, err := p.post(ctx, chunk, rootTS); err != nil {
			p.log.Error("falha ao postar diagnóstico no thread do Slack",
				"dedup_key", r.DedupKey, "chunk", i+1, "err", err)
		}
	}
	return nil
}

// postRoot posta a mensagem raiz COM o bloco de botões de feedback
// (ADR-0014). Falha ao montar o bloco não aborta a triagem — degrada para a
// raiz sem botões, best-effort como o resto do publisher (ver Publish).
func (p *SlackPublisher) postRoot(ctx context.Context, r Report) (string, error) {
	var extra []any
	if fb, err := p.feedbackBlock(r); err != nil {
		p.log.Error("montando bloco de feedback; postando raiz sem botões",
			"dedup_key", r.DedupKey, "err", err)
	} else {
		extra = append(extra, fb)
	}
	return p.post(ctx, p.rootMessage(r), "", extra...)
}

// feedbackBlock monta os botões "Confirmar"/"Refutar" (ADR-0014). O `value` de
// cada botão carrega dedup_key + triage_key (== reportSuffix(r), o mesmo
// sufixo que conclusions/ e confirmations/ espelham) — o listener no processo
// do slack-qa-bot lê isso de volta no clique, sem precisar de índice reverso
// (channel+ts) para descobrir a que incidente o clique pertence.
func (p *SlackPublisher) feedbackBlock(r Report) (actionsBlock, error) {
	raw, err := json.Marshal(feedbackButtonValue{
		DedupKey:  r.DedupKey,
		TriageKey: reportSuffix(r),
	})
	if err != nil {
		return actionsBlock{}, err
	}
	value := string(raw)
	return actionsBlock{
		Type:    "actions",
		BlockID: "triage_feedback",
		Elements: []slackButton{
			{
				Type:     "button",
				ActionID: "triage_confirm",
				Text:     slackButtonText{Type: "plain_text", Text: "✅ Confirmar diagnóstico", Emoji: true},
				Style:    "primary",
				Value:    value,
			},
			{
				Type:     "button",
				ActionID: "triage_refute",
				Text:     slackButtonText{Type: "plain_text", Text: "❌ Refutar diagnóstico", Emoji: true},
				Style:    "danger",
				Value:    value,
			},
		},
	}, nil
}

func (p *SlackPublisher) rootMessage(r Report) string {
	summary := r.Summary
	if summary == "" {
		summary = "grupo de alertas"
	}
	return fmt.Sprintf("🔍 *Triagem automática* — %s\nDiagnóstico completo na thread.", summary)
}

// header monta a linha de contexto acima do diagnóstico: resumo legível do
// grupo (alertnames + namespace) e link pro alerta no Alertmanager. O
// groupKey de máquina fica fora do título — ele serve pra correlação no log,
// não pra leitura humana.
func (p *SlackPublisher) header(r Report) string {
	summary := r.Summary
	if summary == "" {
		summary = "grupo de alertas"
	}
	h := fmt.Sprintf("🔍 *Triagem automática* — %s", summary)
	if p.externalURL != "" {
		h += fmt.Sprintf("\n<%s/#/alerts|Ver alerta no Alertmanager>", p.externalURL)
	}
	return h
}

// post envia uma mensagem. threadTS vazio = mensagem raiz; caso contrário,
// resposta em thread. extraBlocks são anexados APÓS o bloco markdown do texto
// (hoje só a mensagem raiz usa isso, para os botões de feedback — ADR-0014).
// Retorna o ts da mensagem criada.
func (p *SlackPublisher) post(ctx context.Context, text, threadTS string, extraBlocks ...any) (string, error) {
	blocks := append([]any{slackBlock{Type: "markdown", Text: text}}, extraBlocks...)
	body, err := json.Marshal(slackPostRequest{
		Channel:  p.channel,
		Text:     text, // fallback de notificação; os blocks fazem o rendering
		Blocks:   blocks,
		ThreadTS: threadTS,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.postURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.hc.Do(req)
	if err != nil {
		return "", err
	}

	defer func() {
		_ = resp.Body.Close()
	}()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	// A Web API do Slack devolve 200 mesmo em erro lógico; o campo ok manda.
	var sr slackPostResponse
	if err := json.Unmarshal(raw, &sr); err != nil {
		return "", fmt.Errorf("resposta do Slack não é JSON (status %d): %w", resp.StatusCode, err)
	}
	if !sr.OK {
		return "", fmt.Errorf("slack recusou a mensagem: %s", sr.Error)
	}
	return sr.TS, nil
}
