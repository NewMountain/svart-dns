package telemetry

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHTTPTraceCorrelationAndPrivacy(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)).With("service", Service))
	t.Cleanup(func() { slog.SetDefault(previous) })
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	old := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(old)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown provider: %v", err)
		}
	})
	oldProp := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(oldProp) })
	mux := http.NewServeMux()
	mux.HandleFunc("/api/items/", func(w http.ResponseWriter, r *http.Request) {
		ctx, done := Begin(r.Context(), StorageRead)
		Logger(ctx).InfoContext(ctx, "storage checked")
		done(errors.New("private-query-body"))
		w.WriteHeader(503)
		w.WriteHeader(200)
	})
	handler := HTTP(mux, func(r *http.Request) string { _, pattern := mux.Handler(r); return pattern })
	req := httptest.NewRequest("GET", "http://localhost/api/items/private-client?token=secret", nil)
	req.Header.Set("Authorization", "private-token")
	req.Header.Set("traceparent", "00-12345678901234567890123456789012-1234567890123456-01")
	result := httptest.NewRecorder()
	handler.ServeHTTP(result, req)
	if result.Code != 503 {
		t.Fatalf("status=%d want 503", result.Code)
	}
	spans := recorder.Ended()
	if len(spans) != 2 {
		t.Fatalf("spans=%d want 2", len(spans))
	}
	if spans[1].Name() != "GET /api/items/" || spans[1].SpanContext().TraceID().String() != "12345678901234567890123456789012" || spans[1].Status().Code != codes.Error {
		t.Fatalf("unexpected request span: %v", spans[1])
	}
	if spans[0].Parent().SpanID() != spans[1].SpanContext().SpanID() {
		t.Fatal("storage parent did not match request span")
	}
	for _, secret := range []string{"private-client", "secret", "private-token", "private-query-body"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("log leaked %s", secret)
		}
	}
	if strings.Count(logs.String(), `"trace_id":"12345678901234567890123456789012"`) != 3 {
		t.Fatalf("correlated logs=%s", logs.String())
	}
}
