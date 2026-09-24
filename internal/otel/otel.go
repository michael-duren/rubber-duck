// Package otel wires the OpenTelemetry metrics pipeline: the MeterProvider
// and its exporter, Go runtime metrics, and HTTP server instrumentation.
package otel

import (
	"context"
	"errors"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	otelapi "go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutmetric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// Options configures the telemetry pipeline.
type Options struct {
	// Endpoint is the OTLP gRPC collector URL; empty exports to stdout.
	Endpoint       string
	ServiceName    string
	ExportInterval time.Duration
}

// Setup bootstraps the OpenTelemetry pipeline and registers global providers.
// Call the returned shutdown func on exit to flush pending telemetry.
func Setup(ctx context.Context, opts Options) (func(context.Context) error, error) {
	var shutdownFuncs []func(context.Context) error
	shutdown := func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	otelapi.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	meterProvider, err := newMeterProvider(ctx, opts)
	if err != nil {
		return shutdown, errors.Join(err, shutdown(ctx))
	}
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otelapi.SetMeterProvider(meterProvider)

	// Go runtime metrics (memory, GC, goroutines); scheduling latency comes
	// from the producer attached to the reader.
	if err := runtime.Start(runtime.WithMeterProvider(meterProvider)); err != nil {
		return shutdown, errors.Join(err, shutdown(ctx))
	}

	return shutdown, nil
}

func newMeterProvider(ctx context.Context, opts Options) (*metric.MeterProvider, error) {
	var exporter metric.Exporter
	var err error
	if opts.Endpoint != "" {
		// An http:// URL disables TLS; https:// enables it.
		exporter, err = otlpmetricgrpc.New(ctx, otlpmetricgrpc.WithEndpointURL(opts.Endpoint))
	} else {
		exporter, err = stdoutmetric.New()
	}
	if err != nil {
		return nil, err
	}
	// Service name first so OTEL_RESOURCE_ATTRIBUTES can still override it.
	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(opts.ServiceName)),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, err
	}
	return metric.NewMeterProvider(
		metric.WithResource(res),
		metric.WithReader(metric.NewPeriodicReader(exporter,
			metric.WithInterval(opts.ExportInterval),
			metric.WithProducer(runtime.NewProducer()),
		)),
	), nil
}
