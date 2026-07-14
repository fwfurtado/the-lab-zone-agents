// Package observability configura o tracing OTel da borda de triagem.
//
// A borda ENRAÍZA o trace do caminho de resposta a incidente: o span raiz nasce
// aqui (por job, pós-dedup) e o contexto W3C — traceparent + baggage — é
// propagado ao núcleo Python via header, costurando os dois processos num único
// trace.
//
// O mesmo setup inicia o Pyroscope para continuous profiling. Os profiles usam
// os labels service_name/service_namespace compatíveis com a correlação
// tracesToProfiles provisionada no Grafana.
//
// EMENDA o ADR-0003 (borda "stdlib-only" -> "dependências justificadas"): o SDK
// OTel entra porque a borda é a raiz do trace e prepara o terreno para o A2A.
//
// Endpoint e protocolo do exporter vêm das envs PADRÃO do OTel
// (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_EXPORTER_OTLP_PROTOCOL), lidas pelo próprio
// exporter — não reimplementamos essa lógica aqui.
package observability

import (
	"context"
	"fmt"

	"github.com/grafana/pyroscope-go"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Setup configura o profiler Pyroscope, o TracerProvider global e o propagador
// (TraceContext + Baggage). Devolve um shutdown que drena profiles/spans
// pendentes. Cada backend tem seu gate para permitir runs locais/CI sem
// dependências externas.
func Setup(ctx context.Context, otelEnabled, pyroscopeEnabled bool, serviceName, namespace, environment, pyroscopeServerAddress string) (func(context.Context) error, error) {
	var profiler *pyroscope.Profiler
	if pyroscopeEnabled {
		var err error
		profiler, err = pyroscope.Start(pyroscope.Config{
			ApplicationName: serviceName,
			ServerAddress:   pyroscopeServerAddress,
			Tags: map[string]string{
				"service_name":           serviceName,
				"service_namespace":      namespace,
				"deployment_environment": environment,
			},
			ProfileTypes: []pyroscope.ProfileType{
				pyroscope.ProfileCPU,
				pyroscope.ProfileAllocObjects,
				pyroscope.ProfileAllocSpace,
				pyroscope.ProfileInuseObjects,
				pyroscope.ProfileInuseSpace,
			},
		})
		if err != nil {
			return nil, fmt.Errorf("iniciando profiler Pyroscope: %w", err)
		}
	}

	shutdownTrace := func(context.Context) error { return nil }
	if !otelEnabled {
		return func(context.Context) error {
			if profiler != nil {
				return profiler.Stop()
			}
			return nil
		}, nil
	}

	// Sem endpoint explícito: o exporter lê OTEL_EXPORTER_OTLP_ENDPOINT/_PROTOCOL
	// do ambiente (comportamento padrão do SDK).
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		if profiler != nil {
			_ = profiler.Stop()
		}
		return nil, fmt.Errorf("criando exporter OTLP: %w", err)
	}

	// service.name distingue a borda (triage-webhook) do núcleo (triage-agent).
	// service.namespace vem do namespace real do pod para casar com os labels
	// dos profiles e com a correlação trace -> profile no Grafana.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.namespace", namespace),
			attribute.String("deployment.environment", environment),
		),
	)
	if err != nil {
		if profiler != nil {
			_ = profiler.Stop()
		}
		return nil, fmt.Errorf("montando resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	// Propagador global: traceparent costura os processos; baggage leva os
	// atributos de domínio ao núcleo Python (e adiante).
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdownTrace = tp.Shutdown

	return func(ctx context.Context) error {
		var shutdownErr error
		if profiler != nil {
			shutdownErr = profiler.Stop()
		}
		if err := shutdownTrace(ctx); err != nil && shutdownErr == nil {
			shutdownErr = err
		}
		return shutdownErr
	}, nil
}
