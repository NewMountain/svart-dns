package main

import (
	"github.com/prometheus/client_golang/prometheus"
	"math"
	"path/filepath"
	"testing"
	"time"
)

func TestQueueOwnershipAndUnavailableMetrics(t *testing.T) {
	journal, err := openDurableJournal(filepath.Join(t.TempDir(), "queue.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := journal.close(); err != nil {
			t.Errorf("close journal: %v", err)
		}
	}()
	id, err := journal.append([]byte(`{"ts":1700000000000000000,"line":"private event"}`))
	if err != nil {
		t.Fatal(err)
	}
	count, age := queueState(journal, 0, time.Unix(1700000300, 0))
	if count != 1 || age != 300 {
		t.Fatalf("count=%v age=%v want 1 300", count, age)
	}
	count, age = queueState(journal, id, time.Unix(1700000300, 0))
	if count != 0 || age != 0 {
		t.Fatalf("caught-up count=%v age=%v want 0 0", count, age)
	}
	if _, err = journal.db.Exec("UPDATE journal_records SET payload='corrupt'"); err != nil {
		t.Fatal(err)
	}
	_, age = queueState(journal, 0, time.Unix(1700000300, 0))
	if !math.IsNaN(age) {
		t.Fatalf("corrupt age=%v want NaN", age)
	}
	if err = journal.db.Close(); err != nil {
		t.Fatal(err)
	}
	count, age = queueState(journal, 0, time.Unix(1700000300, 0))
	if !math.IsNaN(count) || !math.IsNaN(age) {
		t.Fatalf("unavailable count=%v age=%v want NaN", count, age)
	}
}

func TestQueueCollectorMarksUnreadableOwnershipUnavailable(t *testing.T) {
	journal, err := openDurableJournal(filepath.Join(t.TempDir(), "ownership.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := journal.close(); err != nil {
			t.Errorf("close journal: %v", err)
		}
	}()
	if _, err = journal.append([]byte(`{"ts":0,"line":"retained"}`)); err != nil {
		t.Fatal(err)
	}
	previousWriter, previousLoki := queryLogWriter, activeLoki
	defer func() { queryLogWriter, activeLoki = previousWriter, previousLoki }()
	queryLogWriter = &logWriter{spool: &logSpool{journal: journal}}
	activeLoki = &lokiSender{journal: journal}
	check := func() {
		t.Helper()
		registry := prometheus.NewRegistry()
		registry.MustRegister(newQueueCollector())
		metrics, err := registry.Gather()
		if err != nil {
			t.Fatal(err)
		}
		unavailable := map[string]float64{}
		for _, family := range metrics {
			if family.GetName() != "svart_journal_unavailable" {
				continue
			}
			for _, metric := range family.Metric {
				unavailable[metric.Label[0].GetValue()] = metric.GetGauge().GetValue()
			}
		}
		if unavailable["presentation"] != 1 || unavailable["loki"] != 1 {
			t.Fatalf("unavailable=%v", unavailable)
		}
	}
	check() // Invalid owned timestamps must not look like an empty healthy queue.
	if err = journal.db.Close(); err != nil {
		t.Fatal(err)
	}
	check() // Both the record read and Loki acknowledgement read now fail.
}
