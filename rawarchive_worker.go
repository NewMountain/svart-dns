package main

import (
	"context"
	"database/sql"
	"errors"
	"github.com/yeti/svart-dns/internal/telemetry"
	"io"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

var rawArchiveFailures = prometheus.NewCounter(prometheus.CounterOpts{Name: "svart_dns_raw_archive_failures_total", Help: "Raw history archive/retention cycles that failed; source ownership is preserved."})
var rawArchiveDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "svart_dns_raw_archive_cycle_seconds", Help: "Raw archive/retention cycle duration.", Buckets: prometheus.ExponentialBuckets(.01, 4, 9)})
var rawArchiveRecords = prometheus.NewGauge(prometheus.GaugeOpts{Name: "svart_dns_raw_archive_retained_records", Help: "Raw events retained in verified Parquet segments, independently of pending presentation or Loki delivery. NaN when unavailable."})
var rawArchiveBytes = prometheus.NewGauge(prometheus.GaugeOpts{Name: "svart_dns_raw_archive_retained_bytes", Help: "Physical raw archive bytes, including partial files; inventoried at startup, hourly, and after failed cycles. NaN when inventory fails."})

func init() {
	prometheus.MustRegister(rawArchiveFailures, rawArchiveDuration, rawArchiveRecords, rawArchiveBytes)
	rawArchiveRecords.Set(math.NaN())
	rawArchiveBytes.Set(math.NaN())
}

type rawArchiveWorker struct {
	stop, done chan struct{}
	once       sync.Once
}

var queryRawArchiveWorker *rawArchiveWorker

func startRawArchiveWorker() {
	if queryLogWriter == nil || queryLogWriter.spool == nil {
		return
	}
	queryRawArchiveWorker = newRawArchiveWorker(queryLogWriter.spool.journal, readDB, archivePath, time.Minute)
}
func newRawArchiveWorker(j *durableJournal, source *sql.DB, root string, interval time.Duration) *rawArchiveWorker {
	w := &rawArchiveWorker{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(w.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		var a *rawArchiver
		var lastInventory time.Time
		for {
			_, complete := telemetry.Begin(context.Background(), telemetry.RawArchive)
			started := time.Now()
			var err error
			if a == nil {
				a, err = newRawArchiver(j, source, root)
				if a != nil {
					a.stop = w.stop
					a.deferSmall = true
				}
			}
			if err == nil {
				err = a.cycle(started)
			}
			complete(err)
			rawArchiveDuration.Observe(time.Since(started).Seconds())
			if err != nil {
				rawArchiveFailures.Inc()
				logArchiver.Error("raw history archive unavailable; sources preserved", "error", err)
			}
			if a != nil && !a.stopping() {
				inventory := lastInventory.IsZero() || started.Sub(lastInventory) >= time.Hour || err != nil
				a.updateMetrics(inventory)
				if inventory {
					lastInventory = started
				}
			}
			select {
			case <-w.stop:
				return
			case <-ticker.C:
			}
		}
	}()
	return w
}
func (w *rawArchiveWorker) close() { w.once.Do(func() { close(w.stop) }); <-w.done }
func closeRawArchiveWorker() {
	if queryRawArchiveWorker != nil {
		queryRawArchiveWorker.close()
	}
}
func (a *rawArchiver) stopping() bool {
	select {
	case <-a.stop:
		return true
	default:
		return false
	}
}
func (a *rawArchiver) updateMetrics(inventory bool) {
	var count int64
	if err := a.journal.db.QueryRow("SELECT COALESCE(SUM(count),0) FROM raw_archive_files WHERE state='ready'").Scan(&count); err != nil {
		rawArchiveRecords.Set(math.NaN())
	} else {
		rawArchiveRecords.Set(float64(count))
	}
	if !inventory {
		return
	}
	size, err := rawArchiveDirectoryBytes(a.dir)
	if err != nil {
		rawArchiveBytes.Set(math.NaN())
	} else {
		rawArchiveBytes.Set(float64(size))
	}
}

// Filesystem iteration holds at most 64 entries at each of the three date
// levels. WalkDir/ReadDir without a count would materialize whole directories.
func walkRawArchiveFiles(root string, visit func(string, os.FileInfo) error) error {
	var walk func(string, int) error
	walk = func(relative string, depth int) error {
		// #nosec G304 G703 -- Archive root is operator selected; names are generated or validated by scanRawManifest, and directory walks use actual entry names.
		f, err := os.Open(filepath.Join(root, relative))
		if err != nil {
			return err
		}
		defer closeReadResource(f)
		for {
			entries, readErr := f.ReadDir(64)
			for _, entry := range entries {
				child := filepath.Join(relative, entry.Name())
				info, err := entry.Info()
				if err != nil {
					return err
				}
				if info.IsDir() {
					if depth >= 3 {
						return errors.New("unexpected nesting in raw archive directory")
					}
					if err = walk(child, depth+1); err != nil {
						return err
					}
				} else {
					if !info.Mode().IsRegular() {
						return errors.New("unexpected nonregular file in raw archive directory")
					}
					if err = visit(child, info); err != nil {
						return err
					}
				}
			}
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			if readErr != nil {
				return readErr
			}
		}
	}
	return walk("", 0)
}
func rawArchiveDirectoryBytes(dir string) (int64, error) {
	var total int64
	err := walkRawArchiveFiles(dir, func(_ string, info os.FileInfo) error { total += info.Size(); return nil })
	return total, err
}
