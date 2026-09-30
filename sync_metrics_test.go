package main

import (
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestSyncErrorMetricsUseOpaquePeerLabel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "error")
	initLogging()

	syncErrorsTotal.Reset()
	syncPeerHealthy.Reset()
	defer syncErrorsTotal.Reset()
	defer syncPeerHealthy.Reset()

	registry := prometheus.NewRegistry()
	registry.MustRegister(syncErrorsTotal, syncPeerHealthy)

	const peerURL = "https://10.42.1.7:443"
	peer := &peerState{
		URL:         peerURL,
		MetricLabel: "peer-1",
	}
	syncError(peer, errors.New("test sync failure"))

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather sync metrics: %v", err)
	}

	metricsChecked := 0
	for _, family := range families {
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() != "peer" {
					continue
				}
				metricsChecked++
				if got := label.GetValue(); got != "peer-1" {
					t.Errorf("%s peer label = %q, want opaque label peer-1", family.GetName(), got)
				}
				if label.GetValue() == peerURL {
					t.Errorf("%s exposes peer URL in metrics", family.GetName())
				}
			}
		}
	}
	if metricsChecked != 2 {
		t.Fatalf("checked %d peer metric labels, want 2", metricsChecked)
	}
}
