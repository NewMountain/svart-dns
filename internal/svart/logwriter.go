package svart

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	// Register the SQLite driver used by database/sql.
	_ "github.com/mattn/go-sqlite3"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/yeti/svart-dns/internal/policycore"
)

// Doom loop deduplication thresholds (queries per second per client+domain pair)
const doomLoopThresholdBlocked = 25
const doomLoopThresholdAllowed = 100

type clientDomainKey struct {
	clientIP string
	domain   string
}

type doomLoopState struct {
	coalescing     bool
	coalescedCount int
	lastEntry      queryLogEntry // store last entry for writing summary rows
}

type queryLogEntry struct {
	// ts is the query decision time (Unix nanoseconds), stamped before volatile
	// admission so replay preserves the original event time.
	ts int64

	clientIP     string
	queryName    string
	queryType    string
	responseCode string
	upstream     string

	// Rich policy result fields (DD-019)
	result         string
	resultReason   string
	resultTier     string
	resultEntity   string
	resultRule     string
	resultListName string
	rangeResult    string
	rangeEntity    string
	rangeRule      string
	rangeListName  string
	groupResult    string
	groupEntity    string
	groupRule      string
	groupListName  string
	ipResult       string
	ipEntity       string
	ipRule         string
	ipListName     string

	latencyMicroseconds int64 // processing time before queue admission and network write
	coalescedCount      int
	resultListID        int
	rangeListID         int
	groupListID         int
	ipListID            int

	blocked           bool
	resultIsPublished bool
	rangeIsPublished  bool
	groupIsPublished  bool
	ipIsPublished     bool
}

type logWriter struct {
	ch          chan *queryLogEntry
	db          *sql.DB
	stmt        *sql.Stmt
	done        chan struct{}
	dropped     atomic.Int64
	flushFailed atomic.Int64 // transient Begin/Exec/Commit failures (e.g. SQLITE_BUSY during VACUUM) — batch is retried, never dropped
	spool       *logSpool    // durable raw journal; replayed into presentation rows
	cursorReady bool
	replayDoom  *doomLoopTracker
	queued      atomic.Int64
	highWater   atomic.Int64
}

var queryLogWriter *logWriter

// logQueryLines mirrors every query into the structured log (and so into
// Loki, when LOKI_URL is set) in addition to SQLite. Off by default: it puts
// every lookup of every device into container logs, which is a privacy
// surprise for most installs. LOG_QUERIES=true
// turns it on for deployments that alert on query streams.
var logQueryLines atomic.Bool
var loggingEnabled atomic.Bool // controlled by 'logging_enabled' setting

func init() {
	loggingEnabled.Store(true) // default enabled
}

// logWriterFlushFailedTotal counts failed presentation writes. Their immutable
// raw events remain in the journal until the database transaction succeeds.
var logWriterFlushFailedTotal = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "svart_dns_log_writer_flush_failed_total",
	Help: "Total transient log writer flush failures (Begin/Exec/Commit errors). Rows are retried, not dropped.",
})

var logWriterBatchDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
	Name:    "svart_dns_log_writer_batch_duration_seconds",
	Help:    "Background query journal batch persistence time, including serialization and durable group commit.",
	Buckets: []float64{0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 5},
})

var logWriterLokiHandoffFailures = prometheus.NewCounter(prometheus.CounterOpts{
	Name: "svart_dns_log_writer_loki_handoff_failed_total",
	Help: "Failed transfers from the transactional query outbox to durable Loki delivery; records retained.",
})

func init() {
	prometheus.MustRegister(logWriterFlushFailedTotal, logWriterBatchDuration, logWriterLokiHandoffFailures)
}

func readLoggingSetting(q authCounter) (bool, error) {
	var value string
	err := q.QueryRow("SELECT value FROM settings WHERE key='logging_enabled'").Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return value != "false", nil
}
func loadLoggingSetting() error {
	value, err := readLoggingSetting(db)
	if err != nil {
		return err
	}
	loggingEnabled.Store(value)
	return nil
}

const (
	logChannelSize   = 65_536 // bounded volatile admission queue; full queues backpressure callers
	logBatchSize     = 5_000
	logFlushInterval = 100 * time.Millisecond
)

const queryJournalProgressSchema = `CREATE TABLE IF NOT EXISTS query_journal_progress (source TEXT PRIMARY KEY, sequence INTEGER NOT NULL)`

// queryLogInsertSQL is the single definition of a query log row.
const queryLogInsertSQL = `INSERT INTO query_logs (
		timestamp, client_ip, query_name, query_type, response_code, blocked, upstream, latency_microseconds,
		block_tier, block_rule, block_source, block_list_id, block_list_name,
		result, result_reason, client_name, policy_json,
		result_tier, result_entity, result_is_published, result_rule, result_list_id, result_list_name,
		range_result, range_entity, range_is_published, range_rule, range_list_id, range_list_name,
		group_result, group_entity, group_is_published, group_rule, group_list_id, group_list_name,
		ip_result, ip_entity, ip_is_published, ip_rule, ip_list_id, ip_list_name,
		coalesced_count
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

// queryLogTimestamp formats ts the way SQLite's CURRENT_TIMESTAMP does (UTC,
// "YYYY-MM-DD HH:MM:SS") plus milliseconds, so rows written by older versions
// and range filters against datetime('now', ...) keep comparing correctly.
func queryLogTimestamp(ts int64) string {
	return time.Unix(0, ts).UTC().Format("2006-01-02 15:04:05.000")
}

var (
	logSpoolSpilledTotal = prometheus.NewCounterFunc(prometheus.CounterOpts{
		Name: "svart_dns_log_writer_spilled_total",
		Help: "Query events admitted durably to the raw journal before presentation replay.",
	}, func() float64 {
		if lw := queryLogWriter; lw != nil && lw.spool != nil {
			return float64(lw.spool.spilled.Load())
		}
		return 0
	})
	logSpoolPendingRecords = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_log_writer_spool_pending_records",
		Help: "Raw journal records not yet replayed into SQLite.",
	}, func() float64 {
		if lw := queryLogWriter; lw != nil && lw.spool != nil {
			var count int64
			if err := lw.spool.journal.db.QueryRow("SELECT COUNT(*) FROM journal_records WHERE id>?", lw.spool.read.Load()).Scan(&count); err != nil {
				return math.NaN()
			}
			return float64(count)
		}
		return 0
	})
	logSpoolPendingBytes = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_log_writer_spool_pending_bytes",
		Help: "Raw payload bytes not yet replayed into SQLite; NaN when storage cannot be read.",
	}, func() float64 {
		if lw := queryLogWriter; lw != nil && lw.spool != nil {
			var bytes int64
			err := lw.spool.journal.db.QueryRow("SELECT COALESCE(SUM(length(payload)),0) FROM journal_records WHERE id>?", lw.spool.read.Load()).Scan(&bytes)
			if err != nil {
				return math.NaN()
			}
			return float64(bytes)
		}
		return 0
	})
	logSpoolRetainedBytes = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "svart_dns_log_writer_journal_retained_bytes",
		Help: "Disk bytes retained by the raw event journal and its WAL, including replayed history; NaN when unavailable.",
	}, func() float64 {
		if lw := queryLogWriter; lw != nil && lw.spool != nil {
			var bytes int64
			for _, suffix := range []string{"", "-wal", "-shm"} {
				info, err := os.Stat(lw.spool.journal.path + suffix)
				if suffix != "" && os.IsNotExist(err) {
					continue
				}
				if err != nil {
					return math.NaN()
				}
				bytes += info.Size()
			}
			return float64(bytes)
		}
		return 0
	})
)

func init() {
	prometheus.MustRegister(logSpoolSpilledTotal, logSpoolPendingRecords, logSpoolPendingBytes, logSpoolRetainedBytes)
}

func newLogWriter(writerDB *sql.DB, stmt *sql.Stmt, spool *logSpool) *logWriter {
	lw := &logWriter{
		ch:    make(chan *queryLogEntry, logChannelSize),
		db:    writerDB,
		stmt:  stmt,
		done:  make(chan struct{}),
		spool: spool,
	}
	go lw.run()
	return lw
}

func initLogWriter(dbPath string) error {
	// _busy_timeout matches db/readDB (database.go) — without it, the writer's
	// exclusive-lock waits (e.g. against the nightly archiver's VACUUM, DD-013/DD-024)
	// return SQLITE_BUSY immediately instead of waiting up to 5s for the lock to clear.
	writerDB, err := sql.Open("sqlite3", sqliteFileDSN(dbPath, "_journal_mode=WAL&_synchronous=FULL&_busy_timeout=5000&_mmap_size=268435456&_cache_size=-64000"))
	if err != nil {
		return err
	}
	writerDB.SetMaxOpenConns(1)

	stmt, err := writerDB.Prepare(queryLogInsertSQL)
	if err != nil {
		return errors.Join(err, writerDB.Close())
	}
	spool, err := openLogSpool(dbPath + ".spool")
	if err != nil {
		return errors.Join(err, stmt.Close(), writerDB.Close())
	}
	// Load the authoritative cursor before exposing startup backlog metrics.
	if _, err = writerDB.Exec(queryJournalProgressSchema); err != nil {
		spool.close()
		return errors.Join(err, stmt.Close(), writerDB.Close())
	}
	var committed int64
	err = writerDB.QueryRow("SELECT sequence FROM query_journal_progress WHERE source=?", spool.journal.identity).Scan(&committed)
	if err != nil && err != sql.ErrNoRows {
		spool.close()
		return errors.Join(err, stmt.Close(), writerDB.Close())
	}
	spool.read.Store(committed)
	if spool.pending() {
		logLW.Warn("replaying query log entries spooled before the last shutdown",
			"path", spool.journal.path, "records", spool.written.Load()-spool.read.Load())
	}
	if legacy := dbPath + ".pending_logs.jsonl"; fileExists(legacy) {
		logLW.Error("found a dead-letter file from an older version; it is tab-separated, not replayed automatically, and holds only a subset of columns — import or archive it by hand",
			"path", legacy)
	}
	queryLogWriter = newLogWriter(writerDB, stmt, spool)
	return nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func closeLogWriter() {
	if queryLogWriter != nil {
		close(queryLogWriter.ch)
		<-queryLogWriter.done
		if queryLogWriter.spool != nil {
			queryLogWriter.spool.close()
		}
		closeQueryWriterResource(queryLogWriter.stmt)
		closeQueryWriterResource(queryLogWriter.db)
		queryLogWriter = nil
	}
}

var queryLogEntryPool = sync.Pool{New: func() any { return new(queryLogEntry) }}

func (lw *logWriter) log(e queryLogEntry) {
	if lw.spool == nil {
		panic("query log journal unavailable: event not admitted")
	}
	if e.ts == 0 {
		e.ts = clock.Now().UnixNano()
	}
	queued, ok := queryLogEntryPool.Get().(*queryLogEntry)
	if !ok {
		panic("query log entry pool has an invalid internal type")
	}
	*queued = e
	depth := lw.queued.Add(1)
	for old := lw.highWater.Load(); depth > old; old = lw.highWater.Load() {
		if lw.highWater.CompareAndSwap(old, depth) {
			break
		}
	}
	// Success here is volatile admission, not a durable acknowledgement.
	// The caller only waits when bounded memory is already fully occupied.
	lw.ch <- queued
}

// doomLoopTracker coalesces runaway (client, domain) pairs (DD-027). Counts
// live only for the current second, so its memory is bounded by the distinct
// pairs seen in one second, not by everything seen recently; only pairs in
// coalescing mode carry state across seconds.
type doomLoopTracker struct {
	second     int64
	counts     map[clientDomainKey]int
	coalescing map[clientDomainKey]*doomLoopState
}

func newDoomLoopTracker() *doomLoopTracker {
	return &doomLoopTracker{
		counts:     make(map[clientDomainKey]int),
		coalescing: make(map[clientDomainKey]*doomLoopState),
	}
}

// Copy before replay so a rolled-back batch cannot advance rate state.
func cloneDoomLoopTracker(source *doomLoopTracker) *doomLoopTracker {
	cloned := newDoomLoopTracker()
	if source == nil {
		return cloned
	}
	cloned.second = source.second
	for key, count := range source.counts {
		cloned.counts[key] = count
	}
	for key, state := range source.coalescing {
		value := *state
		cloned.coalescing[key] = &value
	}
	return cloned
}

// drainSummaries emits the counts absorbed so far without leaving coalescing
// mode, so a replay batch never commits entries whose count isn't written.
func (d *doomLoopTracker) drainSummaries(emit func(queryLogEntry)) {
	for _, st := range d.coalescing {
		if st.coalescedCount > 0 {
			summary := st.lastEntry
			summary.coalescedCount = st.coalescedCount
			emit(summary)
			st.coalescedCount = 0
		}
	}
}

func doomLoopThreshold(blocked bool) int {
	if blocked {
		return doomLoopThresholdBlocked
	}
	return doomLoopThresholdAllowed
}

// advance closes every second before now: coalesced pairs emit a summary row
// for the counts they absorbed, and leave coalescing mode once a whole second
// passes below threshold.
func (d *doomLoopTracker) advance(now int64, emit func(queryLogEntry)) {
	if now <= d.second {
		return
	}
	for key, st := range d.coalescing {
		if st.coalescedCount > 0 {
			summary := st.lastEntry
			summary.coalescedCount = st.coalescedCount
			emit(summary)
			st.coalescedCount = 0
		}
		if d.counts[key] < doomLoopThreshold(st.lastEntry.blocked) {
			delete(d.coalescing, key)
		}
	}
	d.second = now
	clear(d.counts)
}

// observe counts entry and reports whether it was absorbed into a coalesced
// summary (true) or must be written as its own row (false).
func (d *doomLoopTracker) observe(entry queryLogEntry) bool {
	key := clientDomainKey{clientIP: entry.clientIP, domain: entry.queryName}
	d.counts[key]++
	if st, ok := d.coalescing[key]; ok {
		st.coalescedCount++
		st.lastEntry = entry
		return true
	}
	threshold := doomLoopThreshold(entry.blocked)
	if d.counts[key] <= threshold {
		return false
	}
	d.coalescing[key] = &doomLoopState{coalescing: true, coalescedCount: 1, lastEntry: entry}
	dnsDiagnosticLogger(logLW, entry.clientIP, entry.queryName).Warn("doom loop detected",
		"blocked", entry.blocked,
		"rate", d.counts[key],
		"threshold", threshold,
	)
	return true
}

func (lw *logWriter) run() {
	defer close(lw.done)
	replayWake := make(chan struct{}, 1)
	replayStop := make(chan struct{})
	replayDone := make(chan struct{})
	go func() {
		defer close(replayDone)
		ticker := time.NewTicker(logFlushInterval)
		defer ticker.Stop()
		for {
			select {
			case <-replayWake:
				lw.replaySpool()
			case <-ticker.C:
				lw.replaySpool()
			case <-replayStop:
				lw.replaySpool()
				return
			}
		}
	}()
	batch := make([]*queryLogEntry, 0, logBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		started := clock.Now()
		lw.spool.spill(batch...)
		logWriterBatchDuration.Observe(clock.Now().Sub(started).Seconds())
		lw.queued.Add(-int64(len(batch)))
		for _, entry := range batch {
			*entry = queryLogEntry{}
			queryLogEntryPool.Put(entry)
		}
		clear(batch)
		batch = batch[:0]
		select {
		case replayWake <- struct{}{}:
		default:
		}
	}
	ticker := time.NewTicker(logFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case entry, ok := <-lw.ch:
			if !ok {
				flush()
				close(replayStop)
				<-replayDone
				return
			}
			batch = append(batch, entry)
			if len(batch) == logBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// replaySpool derives presentation rows from immutable journal events and
// atomically records their source cursor in the same SQLite transaction.
func (lw *logWriter) replaySpool() {
	if lw.spool == nil {
		return
	}
	lw.drainQueryLokiOutbox()
	if !lw.cursorReady {
		if _, err := lw.db.Exec(queryJournalProgressSchema); err != nil {
			lw.flushFailed.Add(1)
			logLW.Error("query journal replay unavailable; raw records retained", "error", err)
			return
		}
		lw.cursorReady = true
	}
	var committed int64
	err := lw.db.QueryRow("SELECT sequence FROM query_journal_progress WHERE source=?", lw.spool.journal.identity).Scan(&committed)
	if err != nil && err != sql.ErrNoRows {
		logLW.Error("read query journal cursor failed", "error", err)
		return
	}
	lw.spool.read.Store(committed)
	for {
		entries, next, err := lw.spool.readBatch(logBatchSize)
		if err != nil {
			logLW.Error("query journal replay stopped", "error", err)
			return
		}
		if len(entries) == 0 {
			return
		}
		// Reconstruct a failed batch from immutable raw events. Never carry
		// mutated coalescer state across a rolled-back transaction.
		doom := cloneDoomLoopTracker(lw.replayDoom)
		rows := make([]queryLogEntry, 0, len(entries))
		emit := func(e queryLogEntry) { rows = append(rows, e) }
		for _, e := range entries {
			if e.coalescedCount > 1 {
				rows = append(rows, e)
				continue
			}
			doom.advance(e.ts/int64(time.Second), emit)
			if !doom.observe(e) {
				rows = append(rows, e)
			}
		}
		doom.drainSummaries(emit)
		if !lw.flushWithCursor(rows, next) {
			return
		}
		// The SQLite transaction already acknowledged these exact raw events.
		// There is no second filesystem checkpoint that can diverge after a crash.
		lw.replayDoom = doom
		lw.spool.read.Store(next)
	}
}

// flush is used by direct batch callers; production replay includes its
// source cursor in the same transaction through flushWithCursor.
func (lw *logWriter) flush(batch []queryLogEntry) bool {
	return lw.flushWithCursor(batch, 0)
}

func (lw *logWriter) flushWithCursor(batch []queryLogEntry, next int64) bool {
	tx, err := lw.db.Begin()
	if err != nil {
		logLW.Error("begin tx failed — batch retained for retry", "error", err, "batch_size", len(batch))
		lw.flushFailed.Add(1)
		logWriterFlushFailedTotal.Inc()
		return false
	}
	lokiHandler, durableLoki := logDNS.Handler().(*lokiHandler)
	stagedRecords := make(map[int]slog.Record)
	if durableLoki {
		if _, err := tx.Exec(queryLokiOutboxSchema); err != nil {
			rollbackTransaction(tx)
			return false
		}
	}
	txStmt := tx.Stmt(lw.stmt)
	policySnapshots := make([]*queryLogPolicySnapshot, len(batch))
	policyJSONs := make([]string, len(batch))
	for i, e := range batch {
		legacyBlock := deriveLegacyBlockFields(e)
		clientName := getClientAliasCached(e.clientIP)
		policySnapshots[i] = buildQueryLogPolicySnapshot(e)
		policyJSONs[i] = buildQueryLogPolicyJSON(e)
		cc := e.coalescedCount
		if cc < 1 {
			cc = 1
		}
		ts := e.ts
		if ts == 0 {
			ts = time.Now().UnixNano()
		}
		result, err := txStmt.Exec(
			queryLogTimestamp(ts), e.clientIP, e.queryName, e.queryType, e.responseCode, e.blocked, e.upstream, e.latencyMicroseconds,
			legacyBlock.tier, legacyBlock.rule, legacyBlock.source, legacyBlock.listID, legacyBlock.listName,
			e.result, e.resultReason, clientName, policyJSONs[i],
			e.resultTier, e.resultEntity, e.resultIsPublished, e.resultRule, e.resultListID, e.resultListName,
			e.rangeResult, e.rangeEntity, e.rangeIsPublished, e.rangeRule, e.rangeListID, e.rangeListName,
			e.groupResult, e.groupEntity, e.groupIsPublished, e.groupRule, e.groupListID, e.groupListName,
			e.ipResult, e.ipEntity, e.ipIsPublished, e.ipRule, e.ipListID, e.ipListName,
			cc,
		)
		if err == nil {
			err = requireOneMutation(result)
		}
		if err != nil {
			logLW.Error("insert failed — batch retained for retry", "error", err, "batch_size", len(batch))
			rollbackTransaction(tx)
			lw.flushFailed.Add(1)
			logWriterFlushFailedTotal.Inc()
			return false
		}
	}
	if durableLoki && logQueryLines.Load() && lokiHandler.Enabled(context.Background(), slog.LevelInfo) {
		for i, e := range batch {
			record := querySlogRecord(e, policySnapshots[i])
			payload, err := lokiHandler.encodeRecord(record)
			if err != nil {
				rollbackTransaction(tx)
				return false
			}
			key := journalToken()
			if next > 0 {
				key = fmt.Sprintf("query:%s:%d:%d", lw.spool.journal.identity, next, i)
			}
			if err = insertExactOutbox(tx, key, payload); err != nil {
				rollbackTransaction(tx)
				return false
			}
			stagedRecords[i] = record
		}
	}

	if next > 0 {
		if err := rawArchiveChange(tx, "INSERT INTO query_journal_progress(source,sequence) VALUES(?,?) ON CONFLICT(source) DO UPDATE SET sequence=excluded.sequence", lw.spool.journal.identity, next); err != nil {
			rollbackTransaction(tx)
			lw.flushFailed.Add(1)
			logWriterFlushFailedTotal.Inc()
			logLW.Error("query journal cursor commit failed; batch retained", "error", err)
			return false
		}
	}
	if err := tx.Commit(); err != nil {
		logLW.Error("commit failed — batch retained for retry", "error", err, "batch_size", len(batch))
		lw.flushFailed.Add(1)
		logWriterFlushFailedTotal.Inc()
		return false
	}

	// Prometheus metrics + structured slog (off hot path — runs in background goroutine)
	for i, e := range batch {
		cc := e.coalescedCount
		if cc < 1 {
			cc = 1
		}
		fcc := float64(cc)
		recordQueryMetrics(e, fcc)

		// Doom loop anomaly alerting — emit to Loki via logDNS for Grafana alerting
		threshold := doomLoopThresholdAllowed
		if e.blocked {
			threshold = doomLoopThresholdBlocked
		}
		if e.coalescedCount > threshold {
			dnsDiagnosticLogger(logDNS, e.clientIP, e.queryName).Warn("DNS doom loop detected",
				"coalesced_count", e.coalescedCount,
				"blocked", e.blocked)
		}
		if !logQueryLines.Load() {
			continue
		}
		if durableLoki {
			if _, ok := stagedRecords[i]; !ok {
				continue
			}
		}
		record := querySlogRecord(e, policySnapshots[i])
		if durableLoki {
			// The exact Loki payload already belongs to the transactional
			// outbox. Stdout remains the normal presentation side effect.
			if err := lokiHandler.inner.Handle(context.Background(), stagedRecords[i]); err != nil {
				fmt.Fprintf(os.Stderr, "query stdout unavailable: %v\n", err)
			}
		} else {
			if logDNS.Enabled(context.Background(), record.Level) {
				if err := logDNS.Handler().Handle(context.Background(), record); err != nil {
					fmt.Fprintf(os.Stderr, "query stdout unavailable: %v; query retained in SQLite and raw journal\n", err)
				}
			}
		}
	}

	lw.drainQueryLokiOutbox()

	return true
}

func querySlogRecord(e queryLogEntry, snapshot *queryLogPolicySnapshot) slog.Record {
	attrs := []slog.Attr{
		slog.String("client_ip", e.clientIP),
		slog.String("domain", e.queryName),
		slog.String("type", e.queryType),
		slog.String("rcode", e.responseCode),
		slog.Bool("blocked", e.blocked),
		slog.Int64("latency_microseconds", e.latencyMicroseconds),
	}
	if e.upstream != "" {
		attrs = append(attrs, slog.String("upstream", e.upstream))
	} else if !e.blocked {
		attrs = append(attrs, slog.String("upstream", "cache"))
	}

	if policy := policySnapshotToPolicyResult(snapshot); policy != nil {
		attrs = append(attrs, policyToSlogAttrs(policy)...)
	} else {
		attrs = append(attrs,
			slog.String("result", "allow"),
			slog.Group("result_source",
				slog.String("tier", "default"),
				slog.String("name", "no matching rule"),
			),
		)
	}

	record := slog.NewRecord(clock.Now(), slog.LevelInfo, "query", 0)
	record.AddAttrs(attrs...)
	return record
}

const queryLokiOutboxSchema = `CREATE TABLE IF NOT EXISTS query_loki_outbox(id TEXT PRIMARY KEY,payload BLOB NOT NULL)`

// Destination ownership is durable before the presentation cursor advances.
// A crash between presentation commit and Loki admission resumes here.
func (lw *logWriter) drainQueryLokiOutbox() {
	if err := lw.transferQueryLokiOutbox(); err != nil {
		logWriterLokiHandoffFailures.Inc()
		fmt.Fprintf(os.Stderr, "query Loki handoff unavailable: %v; transactional outbox retained\n", err)
	}
}

func (lw *logWriter) transferQueryLokiOutbox() error {
	handler, ok := logDNS.Handler().(*lokiHandler)
	if !ok {
		return nil
	}
	if _, err := lw.db.Exec(queryLokiOutboxSchema); err != nil {
		return err
	}
	for {
		rows, err := lw.db.Query("SELECT id,payload FROM query_loki_outbox ORDER BY rowid LIMIT 1000")
		if err != nil {
			return err
		}
		var keys []string
		var payloads [][]byte
		for rows.Next() {
			var key string
			var payload []byte
			if err = rows.Scan(&key, &payload); err != nil {
				return errors.Join(err, rows.Close())
			}
			keys = append(keys, key)
			payloads = append(payloads, payload)
		}
		err = errors.Join(rows.Err(), rows.Close())
		if err != nil || len(keys) == 0 {
			return err
		}
		if _, err = handler.sender.journal.appendKeyedBatch(payloads, keys); err != nil {
			return err
		}
		select {
		case handler.sender.wake <- struct{}{}:
		default:
		}
		tx, err := lw.db.Begin()
		if err != nil {
			return err
		}
		for _, key := range keys {
			if _, err = tx.Exec("DELETE FROM query_loki_outbox WHERE id=?", key); err != nil {
				break
			}
		}
		if err != nil {
			rollbackTransaction(tx)
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
	}
}

// policyToSlogAttrs serializes a PolicyResult into slog attributes for structured output.
func policyToSlogAttrs(p *policycore.PolicyResult) []slog.Attr {
	attrs := []slog.Attr{
		slog.String("result", p.Result),
	}

	if p.ResultSource != nil {
		srcAttrs := []any{
			slog.String("tier", p.ResultSource.Tier),
			slog.String("name", p.ResultSource.Name),
		}
		if p.ResultSource.PublishedList != nil {
			srcAttrs = append(srcAttrs, slog.Group("published_list",
				slog.String("rule", p.ResultSource.PublishedList.Rule),
				slog.Int("list_id", p.ResultSource.PublishedList.ListID),
				slog.String("list_name", p.ResultSource.PublishedList.ListName),
			))
		}
		if p.ResultSource.CustomRule != nil {
			srcAttrs = append(srcAttrs, slog.Group("custom_rule",
				slog.String("action", p.ResultSource.CustomRule.Action),
				slog.String("rule", p.ResultSource.CustomRule.Rule),
			))
		}
		attrs = append(attrs, slog.Group("result_source", srcAttrs...))
	} else {
		attrs = append(attrs, slog.Group("result_source",
			slog.String("tier", "default"),
			slog.String("name", "no matching rule"),
		))
	}

	if p.RangeEvaluation != nil {
		attrs = append(attrs, tierEvalToSlogAttr("range_evaluation", p.RangeEvaluation))
	}
	if p.GroupEvaluation != nil {
		attrs = append(attrs, tierEvalToSlogAttr("group_evaluation", p.GroupEvaluation))
	}
	if p.IPEvaluation != nil {
		attrs = append(attrs, tierEvalToSlogAttr("ip_evaluation", p.IPEvaluation))
	}

	return attrs
}

// tierEvalToSlogAttr converts a TierEvaluation to a slog group attribute.
func tierEvalToSlogAttr(name string, te *policycore.TierEvaluation) slog.Attr {
	groupAttrs := []any{
		slog.String("result", te.Result),
	}
	for i, ent := range te.Entities {
		entAttrs := []any{
			slog.String("name", ent.Name),
			slog.String("result", ent.Result),
		}
		if ent.PublishedList != nil {
			entAttrs = append(entAttrs, slog.Group("published_list",
				slog.String("rule", ent.PublishedList.Rule),
				slog.Int("list_id", ent.PublishedList.ListID),
				slog.String("list_name", ent.PublishedList.ListName),
			))
		}
		if ent.CustomRule != nil {
			entAttrs = append(entAttrs, slog.Group("custom_rule",
				slog.String("action", ent.CustomRule.Action),
				slog.String("rule", ent.CustomRule.Rule),
			))
		}
		groupAttrs = append(groupAttrs, slog.Group(fmt.Sprintf("entity_%d", i), entAttrs...))
	}
	return slog.Group(name, groupAttrs...)
}

// Shutdown follows journal drain; report cleanup failures without replaying
// already committed rows or routing diagnostics through the query writer.
func closeQueryWriterResource(resource io.Closer) {
	if err := resource.Close(); err != nil {
		logLW.Error("query writer resource cleanup failed", "error", err)
	}
}
