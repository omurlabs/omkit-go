// tracing.go — tracing module.
//
// exports: Init
// rules:   none
// agent:   codedna-cli (no-llm) | codedna-cli | 2026-04-30 | codedna-cli | initial CodeDNA annotation pass
// message: 

// Package tracing bootstraps OpenTelemetry OTLP/HTTP tracing for services.
package tracing

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Init sets up OTel tracing and returns a shutdown function.
//
// The W3C TraceContext + Baggage propagator is always installed and a
// TracerProvider with valid (but non-exporting) span context is registered so
// outbound otelhttp clients inject `traceparent` headers regardless of whether
// a collector is reachable. The OTLP/HTTP exporter is only attached when
// OTEL_EXPORTER_OTLP_ENDPOINT is set to a real endpoint; an unset, empty,
// "disabled", or "none" value skips the exporter and returns a noop shutdown.
//
// The version argument is exported as the service.version resource attribute
// when non-empty; pass "" to omit it.
func Init(ctx context.Context, service, version string) (shutdown func(context.Context) error, err error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	attrs := []attribute.KeyValue{semconv.ServiceName(service)}
	if version != "" {
		attrs = append(attrs, semconv.ServiceVersion(version))
	}
	res, err := resource.New(ctx, resource.WithAttributes(attrs...))
	if err != nil {
		return nil, err
	}

	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" || endpoint == "disabled" || endpoint == "none" {
		// No exporter: still register a TracerProvider so spans carry valid
		// SpanContexts and the propagator can inject traceparent headers for
		// cross-service request_id correlation.
		tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		otel.SetTracerProvider(tp)
		slog.Info("tracing.disabled", "service", service)
		return tp.Shutdown, nil
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)

	slog.Info("tracing.enabled", "service", service, "version", version, "endpoint", endpoint)
	return tp.Shutdown, nil
}
