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
	hc          *http.Client
	log         *slog.Logger
}

// NewSlack cria o publisher. externalURL pode ser vazio (omite o link).
func NewSlack(token, channel, externalURL string, log *slog.Logger) *SlackPublisher {
	return &SlackPublisher{
		token:       token,
		channel:     channel,
		externalURL: externalURL,
		hc:          &http.Client{Timeout: 15 * time.Second},
		log:         log,
	}
}

type slackBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type slackPostRequest struct {
	Channel  string       `json:"channel"`
	Text     string       `json:"text"` // fallback p/ notificação e acessibilidade
	Blocks   []slackBlock `json:"blocks,omitempty"`
	ThreadTS string       `json:"thread_ts,omitempty"`
}

type slackPostResponse struct {
	OK    bool   `json:"ok"`
	TS    string `json:"ts"`
	Error string `json:"error"`
}

// Publish posta o diagnóstico. A primeira mensagem carrega o cabeçalho +
// primeiro pedaço; as continuações vão em thread da primeira, para o
// diagnóstico ficar agrupado no canal.
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

	// Primeira mensagem: cabeçalho + primeiro pedaço, sem thread.
	firstText := header + "\n\n" + chunks[0]
	rootTS, err := p.post(ctx, firstText, "")
	if err != nil {
		return fmt.Errorf("postando diagnóstico no Slack: %w", err)
	}

	// Continuações: thread da primeira mensagem. Erro aqui é logado, não
	// aborta — a primeira mensagem (a mais importante) já foi entregue.
	for i, chunk := range chunks[1:] {
		if _, err := p.post(ctx, chunk, rootTS); err != nil {
			p.log.Error("falha ao postar continuação do diagnóstico no Slack",
				"dedup_key", r.DedupKey, "chunk", i+1, "err", err)
		}
	}
	return nil
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
// resposta em thread. Retorna o ts da mensagem criada.
func (p *SlackPublisher) post(ctx context.Context, text, threadTS string) (string, error) {
	body, err := json.Marshal(slackPostRequest{
		Channel:  p.channel,
		Text:     text, // fallback de notificação; os blocks fazem o rendering
		Blocks:   []slackBlock{{Type: "markdown", Text: text}},
		ThreadTS: threadTS,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slackPostURL, bytes.NewReader(body))
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
