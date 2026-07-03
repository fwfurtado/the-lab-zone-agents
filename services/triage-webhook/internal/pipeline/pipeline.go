// Package pipeline implementa a fila limitada e o worker pool de triagens.
//
// O modelo de concorrência (ADR-0017):
//
//		webhook → dedup → fila limitada (chan) → N workers → núcleo → publisher
//
//	  - A fila cheia rejeita com 429; o Alertmanager É o mecanismo de retry.
//	  - N workers é o teto de triagens concorrentes — o teto de custo LLM.
//	  - Shutdown drena: a triagem EM CURSO termina (context desacoplado do
//	    cancelamento, com o próprio TriageTimeout como teto); o que está NA FILA
//	    é descartado e contado. Persistir fila para retomar seria redundância:
//	    alerta ainda firing será reenviado pelo Alertmanager.
package pipeline

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/metrics"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/publish"
)

// Job é uma triagem aguardando execução.
type Job struct {
	DedupKey string
	GroupKey string
	Context  string
	Received time.Time
}

// Triager executa uma triagem (implementado por core.Client).
type Triager interface {
	Triage(ctx context.Context, contextText string) (string, error)
}

// Pool é a fila + workers.
type Pool struct {
	jobs    chan Job
	workers int
	timeout time.Duration
	core    Triager
	pub     publish.Publisher
	m       *metrics.Set
	log     *slog.Logger
}

// New cria o pool. Nada roda até Run.
func New(workers, queueSize int, timeout time.Duration, core Triager, pub publish.Publisher, m *metrics.Set, log *slog.Logger) *Pool {
	return &Pool{
		jobs:    make(chan Job, queueSize),
		workers: workers,
		timeout: timeout,
		core:    core,
		pub:     pub,
		m:       m,
		log:     log,
	}
}

// TryEnqueue tenta enfileirar sem bloquear. false = fila cheia (o handler
// devolve 429 e o Alertmanager reenvia — backpressure honesto, sem buffer
// infinito escondendo o problema).
func (p *Pool) TryEnqueue(job Job) bool {
	select {
	case p.jobs <- job:
		p.m.JobsEnqueued.Inc()
		p.m.QueueDepth.Set(int64(len(p.jobs)))
		return true
	default:
		p.m.QueueRejected.Inc()
		return false
	}
}

// Run executa os workers até o contexto encerrar e então drena: espera as
// triagens em curso e descarta (contando) o que restou na fila. Retorna só
// quando todos os workers pararam.
func (p *Pool) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(p.workers)
	for i := 0; i < p.workers; i++ {
		go func(id int) {
			defer wg.Done()
			p.worker(ctx, id)
		}(i)
	}
	wg.Wait()

	// Workers pararam; o que sobrou na fila é descartado por contrato.
	close(p.jobs)
	dropped := 0
	for job := range p.jobs {
		dropped++
		p.log.Warn("triagem descartada no shutdown", "dedup_key", job.DedupKey, "group_key", job.GroupKey)
	}
	if dropped > 0 {
		p.m.JobsDroppedOnStop.Add(uint64(dropped))
	}
	p.m.QueueDepth.Set(0)
}

func (p *Pool) worker(ctx context.Context, id int) {
	log := p.log.With("worker", id)
	for {
		// Checagem prioritária: com ctx cancelado E job disponível, um select
		// único escolheria ALEATORIAMENTE — o worker poderia pegar mais um job
		// da fila depois do shutdown, violando o contrato de drenagem
		// ("termina o em curso, descarta a fila"). O default torna a saída
		// determinística.
		select {
		case <-ctx.Done():
			return
		default:
		}
		select {
		case <-ctx.Done():
			return
		case job := <-p.jobs:
			p.m.QueueDepth.Set(int64(len(p.jobs)))
			p.process(ctx, log, job)
		}
	}
}

// process executa uma triagem. O contexto da chamada ao núcleo é desacoplado
// do cancelamento do pool (WithoutCancel): SIGTERM não mata a triagem em
// curso — quem limita é o TriageTimeout e, em última instância, o
// terminationGracePeriodSeconds do pod (que DEVE ser maior que o timeout).
func (p *Pool) process(ctx context.Context, log *slog.Logger, job Job) {
	p.m.WorkersBusy.Inc()
	defer p.m.WorkersBusy.Dec()
	p.m.RunsTotal.Inc()

	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.timeout)
	defer cancel()

	start := time.Now()
	log.Info("triagem iniciada", "dedup_key", job.DedupKey, "group_key", job.GroupKey, "queued_for", time.Since(job.Received).String())

	report, err := p.core.Triage(runCtx, job.Context)
	elapsed := time.Since(start)
	p.m.RunDuration.Observe(elapsed.Seconds())

	if err != nil {
		p.m.RunErrors.Inc()
		log.Error("triagem falhou", "dedup_key", job.DedupKey, "elapsed", elapsed.String(), "err", err)
		return
	}

	log.Info("triagem concluída", "dedup_key", job.DedupKey, "elapsed", elapsed.String())
	if err := p.pub.Publish(runCtx, publish.Report{
		DedupKey:  job.DedupKey,
		GroupKey:  job.GroupKey,
		Context:   job.Context,
		Diagnosis: report,
	}); err != nil {
		log.Error("publicação do diagnóstico falhou", "dedup_key", job.DedupKey, "err", err)
	}
}
