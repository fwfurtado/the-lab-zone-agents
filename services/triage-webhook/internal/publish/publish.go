// Package publish define para onde vai o diagnóstico produzido pela triagem.
//
// Na Fase B1 o destino é o log estruturado do próprio serviço (visível via
// kubectl logs e VictoriaLogs). A Fase B2 adiciona SlackPublisher (thread do
// alerta no canal de incidentes) e persistência — a interface é o ponto de
// extensão; o pipeline não muda.
package publish

import (
	"context"
	"log/slog"
)

// Report é o resultado de uma triagem pronto para publicação.
type Report struct {
	// DedupKey identifica o grupo triado (correlaciona com os logs de ingest).
	DedupKey string
	// GroupKey é o groupKey original do Alertmanager (correlação técnica).
	GroupKey string
	// Summary é o resumo legível do grupo, para o título da notificação.
	Summary string
	// Context é o texto que foi entregue ao núcleo.
	Context string
	// Diagnosis é o relatório produzido pelo agente.
	Diagnosis string
}

// Publisher entrega o diagnóstico ao destino.
type Publisher interface {
	Publish(ctx context.Context, r Report) error
}

// LogPublisher escreve o diagnóstico no log estruturado.
type LogPublisher struct {
	log *slog.Logger
}

// NewLog cria o publisher de log.
func NewLog(log *slog.Logger) *LogPublisher {
	return &LogPublisher{log: log}
}

// Publish registra o diagnóstico. Nunca falha — log é best-effort por natureza.
func (p *LogPublisher) Publish(_ context.Context, r Report) error {
	p.log.Info("diagnóstico de triagem",
		"dedup_key", r.DedupKey,
		"group_key", r.GroupKey,
		"diagnosis", r.Diagnosis,
	)
	return nil
}

// MultiPublisher entrega o mesmo relatório a vários destinos. Best-effort: um
// destino que falha não impede os outros nem aborta o conjunto — cada erro é
// coletado e devolvido junto (o pipeline loga). Assim ganhar o Slack não custa
// perder o log estruturado: ambos recebem o diagnóstico.
type MultiPublisher struct {
	publishers []Publisher
	log        *slog.Logger
}

// NewMulti agrega os publishers dados (nil-safe: entradas nil são ignoradas).
func NewMulti(log *slog.Logger, publishers ...Publisher) *MultiPublisher {
	out := make([]Publisher, 0, len(publishers))
	for _, p := range publishers {
		if p != nil {
			out = append(out, p)
		}
	}
	return &MultiPublisher{publishers: out, log: log}
}

// Publish entrega a todos; devolve o primeiro erro (os demais são logados).
func (m *MultiPublisher) Publish(ctx context.Context, r Report) error {
	var firstErr error
	for _, p := range m.publishers {
		if err := p.Publish(ctx, r); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			m.log.Error("publisher falhou", "dedup_key", r.DedupKey, "err", err)
		}
	}
	return firstErr
}
