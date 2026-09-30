package telemetry

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

var requests = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "svart_http_requests_total", Help: "HTTP responses by registered route, method and status."}, []string{"route", "method", "status"})
var duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "svart_http_request_duration_seconds", Help: "Full HTTP handler duration including rejected requests.", Buckets: prometheus.DefBuckets}, []string{"route", "method"})

func init() { prometheus.MustRegister(requests, duration) }

func methodLabel(method string) string {
	switch method {
	case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS":
		return method
	default:
		return "OTHER"
	}
}

type response struct {
	http.ResponseWriter
	status int
}

func (w *response) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}
func (w *response) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}
func (w *response) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// HTTP accepts only a trusted route resolver; never label with request paths or query strings.
func HTTP(next http.Handler, routeFor func(*http.Request) string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		route := routeFor(r)
		method := methodLabel(r.Method)
		ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := otel.Tracer(Service).Start(ctx, method+" "+route, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attribute.String("http.route", route), attribute.String("http.request.method", method)))
		defer span.End()
		captured := &response{ResponseWriter: w}
		defer func() {
			status := captured.status
			if status == 0 {
				status = http.StatusOK
			}
			if p := recover(); p != nil {
				status = http.StatusInternalServerError
				recordHTTP(ctx, span, route, method, status, time.Since(started))
				panic(p)
			}
			recordHTTP(ctx, span, route, method, status, time.Since(started))
		}()
		next.ServeHTTP(captured, r.WithContext(ctx))
	})
}

func recordHTTP(ctx context.Context, span trace.Span, route, method string, status int, elapsed time.Duration) {
	requests.WithLabelValues(route, method, strconv.Itoa(status)).Inc()
	duration.WithLabelValues(route, method).Observe(elapsed.Seconds())
	span.SetAttributes(attribute.Int("http.response.status_code", status))
	if status >= 500 {
		span.SetStatus(codes.Error, "HTTP request failed")
	}
	// Health and metrics still have rate/errors/duration, without duplicating scrape logs.
	if route != "/health" && route != "/metrics" {
		Logger(ctx).InfoContext(ctx, "http request", "route", route, "method", method, "status", status, "duration_ms", elapsed.Milliseconds())
	}
}
