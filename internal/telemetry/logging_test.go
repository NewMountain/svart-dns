package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"testing"
)

func TestProfilerErrorsRemainStructured(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, &slog.HandlerOptions{Level: slog.LevelDebug})).With("service", Service))
	defer slog.SetDefault(previous)
	logger := profileLogger{}
	logger.Infof("started %s", "cpu")
	logger.Debugf("sample %d", 1)
	logger.Errorf("upload returned HTTP %d", 503)
	scanner := json.NewDecoder(&output)
	for _, want := range []struct{ level, message, detail string }{{"INFO", "profiler", "started cpu"}, {"DEBUG", "profiler", "sample 1"}, {"ERROR", "profile export failed", "upload returned HTTP 503"}} {
		var entry struct {
			Level   string `json:"level"`
			Message string `json:"msg"`
			Detail  string `json:"detail"`
			Service string `json:"service"`
		}
		if err := scanner.Decode(&entry); err != nil {
			t.Fatal(err)
		}
		if entry.Level != want.level || entry.Message != want.message || entry.Detail != want.detail || entry.Service != "svart-dns" {
			t.Fatalf("log=%+v want=%+v", entry, want)
		}
	}
}

// A delivery failure must not enqueue another event into the failing outbox.
func TestLokiDeliveryFailureDoesNotFeedItsOwnLogQueue(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)
	_, done := Begin(context.Background(), LokiDelivery)
	done(errors.New("receiver unavailable"))
	if output.Len() != 0 {
		t.Fatal("Loki retry generated a new outbox event")
	}
}
