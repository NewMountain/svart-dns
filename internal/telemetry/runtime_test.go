package telemetry

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	collector "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	"google.golang.org/protobuf/proto"
)

func TestDisabledTelemetry(t *testing.T) {
	stop, err := Start(context.Background(), Config{}, "test")
	if err != nil {
		t.Fatal(err)
	}
	if err = stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if Logger(context.Background()) == nil {
		t.Fatal("disabled logger must exist")
	}
}

func TestOTLPExportPreservesServiceAndTrace(t *testing.T) {
	received := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
		}
		received <- payload
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(200)
	}))
	defer server.Close()
	oldProvider := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	defer func() { otel.SetTracerProvider(oldProvider); otel.SetTextMapPropagator(oldProp) }()
	stop, err := Start(context.Background(), Config{TraceEndpoint: server.URL}, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	_, span := otel.Tracer(Service).Start(context.Background(), "test control request")
	wantID := span.SpanContext().TraceID()
	span.End()
	if err = stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-received:
		var request collector.ExportTraceServiceRequest
		if err = proto.Unmarshal(payload, &request); err != nil {
			t.Fatal(err)
		}
		if len(request.ResourceSpans) != 1 || len(request.ResourceSpans[0].ScopeSpans) != 1 {
			t.Fatalf("unexpected export envelope: %v", &request)
		}
		resource := request.ResourceSpans[0]
		values := map[string]string{}
		for _, a := range resource.Resource.Attributes {
			values[a.Key] = a.Value.GetStringValue()
		}
		if values["service.name"] != "svart-dns" || values["service.version"] != "test-version" {
			t.Fatalf("attributes=%v", values)
		}
		spans := resource.ScopeSpans[0].Spans
		if len(spans) != 1 || spans[0].Name != "test control request" || string(spans[0].TraceId) != string(wantID[:]) {
			t.Fatalf("spans=%v", spans)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown did not export accepted span")
	}
}

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("private transport error")
}

func TestOutboundTracePropagationAndSafeFailure(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	old := otel.GetTracerProvider()
	oldProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	defer func() {
		otel.SetTracerProvider(old)
		otel.SetTextMapPropagator(oldProp)
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown provider: %v", err)
		}
	}()
	var traceparent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceparent = r.Header.Get("traceparent")
		w.WriteHeader(503)
	}))
	defer server.Close()
	req := httptest.NewRequest("GET", server.URL+"/private?token=secret", nil)
	req.RequestURI = ""
	result, err := (Transport{Operation: Sync}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if err := result.Body.Close(); err != nil {
		t.Fatal(err)
	}
	if result.StatusCode != 503 || len(traceparent) != 55 || req.Header.Get("traceparent") != "" {
		t.Fatalf("status=%d propagated=%q original=%q", result.StatusCode, traceparent, req.Header.Get("traceparent"))
	}
	if _, err = (Transport{Base: failingTransport{}, Operation: Sync}).RoundTrip(req); err == nil || err.Error() != "private transport error" {
		t.Fatalf("error=%v", err)
	}
	spans := recorder.Ended()
	if len(spans) != 2 || spans[0].Name() != "sync HTTP" || spans[0].Status().Code != codes.Error || spans[1].Status().Description != "outbound request failed" {
		t.Fatalf("spans=%v", spans)
	}
	for _, span := range spans {
		for _, a := range span.Attributes() {
			if a.Key != "http.request.method" && a.Key != "http.response.status_code" {
				t.Fatalf("unexpected attribute %s", a.Key)
			}
		}
	}
}
