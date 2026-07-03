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
	// GroupKey é o groupKey original do Alertmanager.
	GroupKey string
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
