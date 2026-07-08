// Package observability configura o tracing OTel da borda de triagem.
//
// A borda ENRAÍZA o trace do caminho de resposta a incidente: o span raiz nasce
// aqui (por job, pós-dedup) e o contexto W3C — traceparent + baggage — é
// propagado ao núcleo Python via header, costurando os dois processos num único
// trace.
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

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// Setup configura o TracerProvider global e o propagador (TraceContext +
// Baggage) e devolve um shutdown que drena os spans pendentes. No-op quando
// enabled=false — permite rodar local/CI sem um Collector no ar.
func Setup(ctx context.Context, enabled bool, serviceName, environment string) (func(context.Context) error, error) {
	if !enabled {
		return func(context.Context) error { return nil }, nil
	}

	// Sem endpoint explícito: o exporter lê OTEL_EXPORTER_OTLP_ENDPOINT/_PROTOCOL
	// do ambiente (comportamento padrão do SDK).
	exp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("criando exporter OTLP: %w", err)
	}

	// service.name distingue a borda (triage-webhook) do núcleo (triage-agent)
	// no plano de sistema; ambos sob service.namespace the-lab-zone. No Langfuse
	// o domínio é filtrado pela baggage, não pelo service.name.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			attribute.String("service.name", serviceName),
			attribute.String("service.namespace", "the-lab-zone"),
			attribute.String("deployment.environment", environment),
		),
	)
	if err != nil {
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

	return tp.Shutdown, nil
}
