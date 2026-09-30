package telemetry

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// Operation names are constants at call sites, never operator-controlled data.
type Operation string

// Stable operation names bound trace and metric cardinality.
const (
	Sync         Operation = "sync"
	RawArchive   Operation = "raw_archive"
	AutoRefresh  Operation = "auto_refresh"
	LokiDelivery Operation = "loki_delivery"
	StorageRead  Operation = "storage_read"
	StorageWrite Operation = "storage_write"
	ListDownload Operation = "list_download"
)

var jobs = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "svart_background_operations_total", Help: "Background/storage operations by bounded operation and outcome."}, []string{"operation", "outcome"})
var jobDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "svart_background_operation_duration_seconds", Help: "Background/storage operation elapsed time."}, []string{"operation"})
var jobSuccess = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "svart_background_last_success_timestamp_seconds", Help: "Last completed successful background/storage operation."}, []string{"operation"})

func init() { prometheus.MustRegister(jobs, jobDuration, jobSuccess) }

// Begin starts an operation; the returned completion function records its result.
func Begin(ctx context.Context, op Operation) (context.Context, func(error)) {
	started := time.Now()
	ctx, span := otel.Tracer(Service).Start(ctx, string(op))
	return ctx, func(err error) {
		outcome := "success"
		if err != nil {
			outcome = "error"
			span.SetStatus(codes.Error, "operation failed")
			// The Loki owner reports delivery errors outside its own outbox.
			// Logging through the default handler here would grow that failed queue.
			if op != LokiDelivery {
				Logger(ctx).ErrorContext(ctx, "operation failed", "operation", string(op))
			}
		} else {
			jobSuccess.WithLabelValues(string(op)).Set(float64(time.Now().Unix()))
		}
		jobs.WithLabelValues(string(op), outcome).Inc()
		jobDuration.WithLabelValues(string(op)).Observe(time.Since(started).Seconds())
		span.End()
	}
}

// Transport deliberately exports no URL, header, request body, peer identity, or raw error.
type Transport struct {
	Base      http.RoundTripper
	Operation Operation
}

// RoundTrip propagates trace context and records only method and response status.
func (t Transport) RoundTrip(r *http.Request) (*http.Response, error) {
	ctx, span := otel.Tracer(Service).Start(r.Context(), string(t.Operation)+" HTTP", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(attribute.String("http.request.method", methodLabel(r.Method))))
	defer span.End()
	clone := r.Clone(ctx)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(clone.Header))
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	response, err := base.RoundTrip(clone)
	if err != nil {
		span.SetStatus(codes.Error, "outbound request failed")
	} else {
		span.SetAttributes(attribute.Int("http.response.status_code", response.StatusCode))
		if response.StatusCode >= 400 {
			span.SetStatus(codes.Error, "outbound HTTP error")
		}
	}
	return response, err
}
