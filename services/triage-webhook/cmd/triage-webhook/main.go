// triage-webhook é a borda do agente de triagem (Fase B, ADR-0017).
//
// Recebe webhooks do Alertmanager, deduplica, enfileira com backpressure e
// aciona o núcleo Python de triagem (sidecar em localhost). O diagnóstico vai
// para o Publisher (log estruturado na B1; Slack/persistência na B2).
//
// Ordem de shutdown (SIGTERM):
//  1. servidor do webhook para de aceitar (novos POSTs → conexão recusada);
//  2. workers: a triagem em curso TERMINA (context desacoplado, teto =
//     TRIAGE_TIMEOUT); o que está na fila é descartado e contado;
//  3. servidor de métricas cai por último (o scrape final ainda vê os
//     contadores de drop).
//
// O terminationGracePeriodSeconds do pod deve exceder TRIAGE_TIMEOUT.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/config"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/core"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/dedup"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/metrics"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/pipeline"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/publish"
	"github.com/fwfurtado/the-lab-zone-agents/services/triage-webhook/internal/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "erro fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("carregando config: %w", err)
	}

	log := newLogger()
	log.Info("triage-webhook iniciando",
		"webhook_addr", cfg.WebhookAddr,
		"metrics_addr", cfg.MetricsAddr,
		"core_url", cfg.CoreURL,
		"workers", cfg.Workers,
		"queue_size", cfg.QueueSize,
		"dedup_ttl", cfg.DedupTTL.String(),
		"triage_timeout", cfg.TriageTimeout.String(),
	)

	reg := metrics.NewRegistry()
	m := metrics.NewSet(reg)
	cache := dedup.New(cfg.DedupTTL, nil)
	coreClient := core.New(cfg.CoreURL, cfg.CoreHealthURL)

	// Publisher: sempre loga (registro durável no VictoriaLogs); Slack e Garage
	// são destinos OPCIONAIS que se somam quando configurados. Fan-out
	// best-effort — um destino que falha não derruba os outros nem a triagem.
	publishers := []publish.Publisher{publish.NewLog(log)}

	if cfg.SlackToken != "" {
		publishers = append(publishers, publish.NewSlack(cfg.SlackToken, cfg.SlackChannel, cfg.AlertmanagerURL, log))
		log.Info("SlackPublisher ativo", "channel", cfg.SlackChannel)
	} else {
		log.Info("SLACK_BOT_TOKEN ausente; sem publicação no Slack")
	}

	if cfg.GarageEndpoint != "" {
		garagePub, err := publish.NewGarage(publish.GarageConfig{
			Endpoint:  cfg.GarageEndpoint,
			AccessKey: cfg.GarageAccessKey,
			SecretKey: cfg.GarageSecretKey,
			Bucket:    cfg.GarageBucket,
			Region:    cfg.GarageRegion,
			UseSSL:    cfg.GarageUseSSL,
		}, log)
		if err != nil {
			return fmt.Errorf("configurando GaragePublisher: %w", err)
		}
		publishers = append(publishers, garagePub)
		log.Info("GaragePublisher ativo (persistência Fase D)", "bucket", cfg.GarageBucket, "endpoint", cfg.GarageEndpoint)
	} else {
		log.Info("GARAGE_ENDPOINT ausente; triagens não serão persistidas")
	}

	// NewMulti é nil-safe e agrega; com um só publisher ainda entrega o contrato.
	pub := publish.NewMulti(log, publishers...)

	pool := pipeline.New(cfg.Workers, cfg.QueueSize, cfg.TriageTimeout, coreClient, pub, cache, m, log)

	webhookSrv := server.Webhook(cfg.WebhookAddr, pool, cache, coreClient, m, log)
	metricsSrv := server.Metrics(cfg.MetricsAddr, reg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// errCh recebe falhas fatais dos servidores (bind falhou, etc.).
	errCh := make(chan error, 2)
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := webhookSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("servidor do webhook: %w", err)
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("servidor de métricas: %w", err)
		}
	}()

	// Ciclo de vida do pool e do sweeper: cancelados por poolCtx, que é
	// derivado mas cancelado EXPLICITAMENTE na ordem certa do shutdown.
	poolCtx, poolCancel := context.WithCancel(context.Background())
	defer poolCancel()

	poolDone := make(chan struct{})
	go func() {
		defer close(poolDone)
		pool.Run(poolCtx)
	}()
	go cache.Sweep(poolCtx)

	var fatal error
	select {
	case <-ctx.Done():
		log.Info("sinal de término recebido; iniciando shutdown ordenado")
	case fatal = <-errCh:
		log.Error("falha fatal; iniciando shutdown", "err", fatal)
	}

	// 1) Webhook para de aceitar novas requisições.
	if err := server.Shutdown(webhookSrv, 10*time.Second); err != nil {
		log.Warn("shutdown do webhook não foi limpo", "err", err)
	}

	// 2) Pool drena: cancela a captação; process() em curso sobrevive via
	//    WithoutCancel até TriageTimeout. Aguardamos o Run devolver.
	poolCancel()
	select {
	case <-poolDone:
		log.Info("pool drenado")
	case <-time.After(cfg.TriageTimeout + 30*time.Second):
		// Não deveria acontecer (process tem teto); registrado para autópsia.
		log.Error("pool não drenou dentro do teto; abandonando")
	}

	// 3) Métricas por último: o scrape final ainda enxerga os drops.
	if err := server.Shutdown(metricsSrv, 5*time.Second); err != nil {
		log.Warn("shutdown das métricas não foi limpo", "err", err)
	}

	wg.Wait()
	log.Info("triage-webhook encerrado")
	return fatal
}

// newLogger monta o slog JSON com nível vindo de LOG_LEVEL (default INFO).
func newLogger() *slog.Logger {
	level := slog.LevelInfo
	switch strings.ToUpper(os.Getenv("LOG_LEVEL")) {
	case "DEBUG":
		level = slog.LevelDebug
	case "WARN", "WARNING":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	}
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
}
