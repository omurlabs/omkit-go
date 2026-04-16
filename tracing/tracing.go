// Package tracing bootstraps OpenTelemetry OTLP/HTTP tracing for Omur services.
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

const defaultEndpoint = "alloy:4318"

// Init sets up OTel tracing and returns a shutdown function.
// Returns nil shutdown if tracing is disabled (empty OTEL_EXPORTER_OTLP_ENDPOINT).
// The version argument is exported to Tempo as the service.version resource
// attribute when non-empty; pass "" to omit it.
func Init(ctx context.Context, service, version string) (shutdown func(context.Context) error, err error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	if endpoint == "disabled" || endpoint == "none" {
		slog.Info("tracing.disabled", "service", service)
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	attrs := []attribute.KeyValue{semconv.ServiceName(service)}
	if version != "" {
		attrs = append(attrs, semconv.ServiceVersion(version))
	}
	res, err := resource.New(ctx,
		resource.WithAttributes(attrs...),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	slog.Info("tracing.enabled", "service", service, "version", version, "endpoint", endpoint)
	return tp.Shutdown, nil
}
