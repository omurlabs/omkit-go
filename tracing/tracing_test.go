package tracing_test

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/omurlabs/omur-core/packages/omur-go-sdk/tracing"
)

// TestInit_AcceptsVersion locks in the three-arg signature
// (ctx, service, version) so callers can propagate service.version
// to the OTLP resource attributes.
func TestInit_AcceptsVersion(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "disabled")
	shutdown, err := tracing.Init(context.Background(), "test-svc", "1.2.3")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown != nil {
		t.Cleanup(func() { _ = shutdown(context.Background()) })
	}
}

// TestInit_EmptyVersionStillWorks verifies that an empty version is
// accepted without error (the implementation must omit the
// service.version attribute rather than emit an empty string).
func TestInit_EmptyVersionStillWorks(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "disabled")
	shutdown, err := tracing.Init(context.Background(), "test-svc", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if shutdown != nil {
		t.Cleanup(func() { _ = shutdown(context.Background()) })
	}
}

// TestInit_SetsPropagator verifies that Init installs a global
// TextMapPropagator so otelhttp.NewTransport can inject Traceparent
// headers on outbound HTTP calls.
func TestInit_SetsPropagator(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := tracing.Init(context.Background(), "test-svc", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if shutdown != nil {
		t.Cleanup(func() { _ = shutdown(context.Background()) })
	}

	// Verify global propagator injects a Traceparent when a span is active.
	ctx, span := otel.Tracer("test").Start(context.Background(), "t")
	defer span.End()

	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if _, ok := carrier["traceparent"]; !ok {
		t.Errorf("expected traceparent in carrier, got %v", carrier)
	}
}
