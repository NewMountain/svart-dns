package main

import (
	"encoding/json"
	"math"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// Read only owned pending rows. A failed read remains unknown, never healthy zero.
func queueState(j *durableJournal, cursor int64, now time.Time) (float64, float64) {
	var count int64
	if err := j.db.QueryRow("SELECT COUNT(*) FROM journal_records WHERE id>?", cursor).Scan(&count); err != nil {
		return math.NaN(), math.NaN()
	}
	if count == 0 {
		return 0, 0
	}
	var payload []byte
	if err := j.db.QueryRow("SELECT payload FROM journal_records WHERE id>? ORDER BY id LIMIT 1", cursor).Scan(&payload); err != nil {
		return float64(count), math.NaN()
	}
	var record struct {
		TS int64 `json:"ts"`
	}
	if err := json.Unmarshal(payload, &record); err != nil || record.TS <= 0 {
		return float64(count), math.NaN()
	}
	return float64(count), math.Max(0, now.Sub(time.Unix(0, record.TS)).Seconds())
}

type queueCollector struct{ pending, age, failures, unavailable *prometheus.Desc }

func newQueueCollector() *queueCollector {
	return &queueCollector{
		pending:     prometheus.NewDesc("svart_queue_pending_records", "Persisted events still owned by each delivery queue; NaN on read failure.", []string{"queue"}, nil),
		age:         prometheus.NewDesc("svart_queue_oldest_age_seconds", "Age of first persisted unacknowledged event; NaN on read/integrity failure.", []string{"queue"}, nil),
		failures:    prometheus.NewDesc("svart_journal_failures_total", "Failed durable journal commits; queued events retained for retry.", []string{"queue"}, nil),
		unavailable: prometheus.NewDesc("svart_journal_unavailable", "Journal cannot commit or read valid ownership state; 1 means unavailable.", []string{"queue"}, nil),
	}
}
func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.pending
	ch <- c.age
	ch <- c.failures
	ch <- c.unavailable
}
func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	emit := func(name string, j *durableJournal, cursor int64) {
		count, age := queueState(j, cursor, time.Now())
		ch <- prometheus.MustNewConstMetric(c.pending, prometheus.GaugeValue, count, name)
		ch <- prometheus.MustNewConstMetric(c.age, prometheus.GaugeValue, age, name)
		ch <- prometheus.MustNewConstMetric(c.failures, prometheus.CounterValue, float64(j.failures.Load()), name)
		unavailable := 0.0
		if j.unavailable.Load() || math.IsNaN(count) || math.IsNaN(age) {
			unavailable = 1
		}
		ch <- prometheus.MustNewConstMetric(c.unavailable, prometheus.GaugeValue, unavailable, name)
	}
	if lw := queryLogWriter; lw != nil && lw.spool != nil {
		emit("presentation", lw.spool.journal, lw.spool.read.Load())
	}
	if sender := activeLoki; sender != nil {
		var cursor int64
		if err := sender.journal.db.QueryRow("SELECT COALESCE((SELECT CAST(value AS INTEGER) FROM journal_meta WHERE key='loki_ack'),0)").Scan(&cursor); err != nil {
			ch <- prometheus.MustNewConstMetric(c.pending, prometheus.GaugeValue, math.NaN(), "loki")
			ch <- prometheus.MustNewConstMetric(c.age, prometheus.GaugeValue, math.NaN(), "loki")
			ch <- prometheus.MustNewConstMetric(c.failures, prometheus.CounterValue, float64(sender.journal.failures.Load()), "loki")
			ch <- prometheus.MustNewConstMetric(c.unavailable, prometheus.GaugeValue, 1, "loki")
		} else {
			emit("loki", sender.journal, cursor)
		}
	}
}
func init() { prometheus.MustRegister(newQueueCollector()) }
