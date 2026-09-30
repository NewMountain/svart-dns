package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/grafana/pyroscope-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Service identifies this application consistently across all signal types.
const Service = "svart-dns"

// Start enables configured exporters and returns their shutdown function.
func Start(ctx context.Context, c Config, version string) (func(context.Context) error, error) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) { slog.Error("trace export failed", "error", err) }))
	var provider *sdktrace.TracerProvider
	var profiler *pyroscope.Profiler
	if c.TraceEndpoint != "" {
		exporter, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(c.TraceEndpoint), otlptracehttp.WithTimeout(3*time.Second))
		if err != nil {
			return nil, err
		}
		provider = sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter, sdktrace.WithBlocking()), sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", Service), attribute.String("service.version", version))))
		otel.SetTracerProvider(provider)
		otel.SetTextMapPropagator(propagation.TraceContext{})
	}
	if c.ProfileEndpoint != "" {
		var err error
		profiler, err = pyroscope.Start(pyroscope.Config{Logger: profileLogger{}, DisableGCRuns: true, HTTPClient: &http.Client{Timeout: 3 * time.Second}, ApplicationName: Service, ServerAddress: c.ProfileEndpoint, UploadRate: 15 * time.Second, ProfileTypes: []pyroscope.ProfileType{pyroscope.ProfileCPU, pyroscope.ProfileAllocObjects, pyroscope.ProfileAllocSpace, pyroscope.ProfileInuseObjects, pyroscope.ProfileInuseSpace, pyroscope.ProfileGoroutines}, Tags: map[string]string{"service_name": Service, "version": version}})
		if err != nil {
			if provider != nil {
				err = errors.Join(err, provider.Shutdown(ctx))
			}
			return nil, err
		}
	}
	return func(ctx context.Context) error {
		var errs []error
		if profiler != nil {
			errs = append(errs, profiler.Stop())
		}
		if provider != nil {
			errs = append(errs, provider.Shutdown(ctx))
		}
		return errors.Join(errs...)
	}, nil
}

// Logger adds the current span identifiers to the structured application logger.
func Logger(ctx context.Context) *slog.Logger {
	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		return slog.Default().With("trace_id", sc.TraceID().String(), "span_id", sc.SpanID().String())
	}
	return slog.Default()
}
