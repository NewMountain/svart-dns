package main

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// durableJournal is an append-only raw event archive. FULL WAL transactions
// are the admission boundary: callers wait for group commit before returning.
// Queue capacity bounds waiting work, never discards it. Receiver outages only
// grow durable storage. Exhausted storage blocks admission until repaired.
type durableJournal struct {
	db               *sql.DB
	path, identity   string
	requests         chan []*journalRequest
	done             chan struct{}
	mu               sync.RWMutex
	closed           bool
	failures         atomic.Int64
	unavailable      atomic.Bool
	commits          atomic.Int64
	committedRecords atomic.Int64
	commitNanos      atomic.Int64
	batchSizes       [15]atomic.Int64
	pending          atomic.Int64
	highWater        atomic.Int64
}
type journalRequest struct {
	payload []byte
	token   string
	done    chan struct{}
	id      int64
}
type journalRow struct {
	id      int64
	payload []byte
}

func journalToken() string {
	token, err := uuid.NewV7()
	if err != nil {
		panic(err)
	}
	return token.String()
}

func openDurableJournal(path string) (*durableJournal, error) {
	// SQLite files contain raw DNS and log records; establish mode before SQLite
	// opens it (SQLite otherwise uses the process umask's broader permissions).
	// #nosec G304 G703 -- Operator-selected durable journal/directory path; no request input; journal creation uses private permissions.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = f.Close(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite3", sqliteFileDSN(path, "_journal_mode=WAL&_synchronous=FULL&_busy_timeout=1000"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(2)
	fail := func(err error) (*durableJournal, error) { return nil, errors.Join(err, db.Close()) }
	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS journal_records (id INTEGER PRIMARY KEY AUTOINCREMENT, token TEXT NOT NULL UNIQUE, payload BLOB NOT NULL);
 CREATE TABLE IF NOT EXISTS journal_meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
 CREATE TABLE IF NOT EXISTS journal_quarantine (source TEXT NOT NULL, position INTEGER NOT NULL, payload BLOB NOT NULL, reason TEXT NOT NULL, PRIMARY KEY(source,position));`)
	if err != nil {
		return fail(err)
	}
	if _, err = db.Exec("INSERT OR IGNORE INTO journal_meta(key,value) VALUES('identity',?)", journalToken()); err != nil {
		return fail(err)
	}
	var identity string
	if err = db.QueryRow("SELECT value FROM journal_meta WHERE key='identity'").Scan(&identity); err != nil {
		return fail(err)
	}
	if err = syncDirectory(filepath.Dir(path)); err != nil {
		return fail(err)
	}
	j := &durableJournal{db: db, path: path, identity: identity, requests: make(chan []*journalRequest, 4096), done: make(chan struct{})}
	go j.run()
	return j, nil
}

func syncDirectory(path string) (resultErr error) {
	// #nosec G304 G703 -- Operator-selected durable journal/directory path; no request input; journal creation uses private permissions.
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, f.Close()) }()
	return f.Sync()
}

func (j *durableJournal) append(payload []byte) (int64, error) {
	request := &journalRequest{payload: payload, token: journalToken(), done: make(chan struct{})}
	j.mu.RLock()
	if j.closed {
		j.mu.RUnlock()
		return 0, errors.New("journal closed: event not admitted")
	}
	depth := j.pending.Add(1)
	for old := j.highWater.Load(); depth > old; old = j.highWater.Load() {
		if j.highWater.CompareAndSwap(old, depth) {
			break
		}
	}
	j.requests <- []*journalRequest{request}
	j.mu.RUnlock()
	<-request.done
	return request.id, nil
}

func (j *durableJournal) run() {
	defer close(j.done)
	for first := range j.requests {
		batch := first
		// Group synchronous journal callers; query DNS handlers enqueue into
		// their separate volatile queue and never wait for this commit.
		timer := time.NewTimer(200 * time.Microsecond)
		for len(batch) < 5000 {
			select {
			case next, ok := <-j.requests:
				if !ok {
					timer.Stop()
					goto commit
				}
				batch = append(batch, next...)
			case <-timer.C:
				goto commit
			}
		}
		timer.Stop()

	commit:
		for {
			err := j.writeBatch(batch)
			if err == nil {
				break
			}
			{
				j.failures.Add(1)
				j.unavailable.Store(true)
				// Do not recurse into the Loki handler when its own storage failed.
				fmt.Fprintf(os.Stderr, "durable journal unavailable: %s: %v; admission blocked, pending events retained; restore writable storage\n", j.path, err)
				timer := time.NewTimer(100 * time.Millisecond)
				<-timer.C
			}
		}
		j.unavailable.Store(false)
		for _, r := range batch {
			j.pending.Add(-1)
			close(r.done)
		}
	}
}

func (j *durableJournal) writeBatch(batch []*journalRequest) error {
	tx, err := j.db.Begin()
	if err != nil {
		return err
	}
	defer rollbackTransaction(tx)
	stmt, err := tx.Prepare("INSERT OR IGNORE INTO journal_records(token,payload) VALUES(?,?)")
	if err != nil {
		return err
	}
	defer closeReadResource(stmt)
	for _, r := range batch {
		result, execErr := stmt.Exec(r.token, r.payload)
		if execErr != nil {
			return execErr
		}
		affected, execErr := result.RowsAffected()
		if execErr != nil {
			return execErr
		}
		if affected == 1 {
			r.id, err = result.LastInsertId()
		} else {
			var stored []byte
			err = tx.QueryRow("SELECT id,payload FROM journal_records WHERE token=?", r.token).Scan(&r.id, &stored)
			if err == nil && !bytes.Equal(stored, r.payload) {
				return fmt.Errorf("journal retry token conflicts with committed payload")
			}
		}
		if err != nil {
			return err
		}
	}
	started := time.Now()
	err = tx.Commit()
	j.commitNanos.Add(time.Since(started).Nanoseconds())
	if err == nil {
		j.commits.Add(1)
		j.committedRecords.Add(int64(len(batch)))
		bucket := 0
		for n := len(batch); n > 1; n = (n + 1) / 2 {
			bucket++
		}
		j.batchSizes[bucket].Add(1)
	}
	return err
}

func (j *durableJournal) batch(after int64, n int) ([]journalRow, error) {
	rows, err := j.db.Query("SELECT id,payload FROM journal_records WHERE id>? ORDER BY id LIMIT ?", after, n)
	if err != nil {
		return nil, err
	}
	defer closeQueryRows(rows)
	result := make([]journalRow, 0, n)
	for rows.Next() {
		var r journalRow
		if err = rows.Scan(&r.id, &r.payload); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}
func (j *durableJournal) close() error {
	j.mu.Lock()
	if !j.closed {
		j.closed = true
		close(j.requests)
	}
	j.mu.Unlock()
	<-j.done
	return j.db.Close()
}

// Admission bounds handlers waiting for volatile queue space, including cache hits.
// A rejected request has not been accepted for resolution or query logging.
var dnsLogAdmission = make(chan struct{}, 1024)

func admitDNSLog() bool {
	select {
	case dnsLogAdmission <- struct{}{}:
		return true
	default:
		return false
	}
}

// appendBatch lets an existing logical batch share the same durable commit.
// Every payload remains individually identifiable and replayable.
func (j *durableJournal) appendBatch(payloads [][]byte) (int64, error) {
	return j.appendKeyedBatch(payloads, nil)
}

func (j *durableJournal) appendKeyedBatch(payloads [][]byte, keys []string) (int64, error) {
	requests := make([]*journalRequest, len(payloads))
	for i, payload := range payloads {
		token := journalToken()
		if keys != nil {
			token = keys[i]
		}
		requests[i] = &journalRequest{payload: payload, token: token, done: make(chan struct{})}
	}
	j.mu.RLock()
	if j.closed {
		j.mu.RUnlock()
		return 0, errors.New("journal closed: event not admitted")
	}
	for range requests {
		depth := j.pending.Add(1)
		for old := j.highWater.Load(); depth > old; old = j.highWater.Load() {
			if j.highWater.CompareAndSwap(old, depth) {
				break
			}
		}
	}
	// Preserve caller batches instead of racing their per-event enqueue against
	// the grouping timer. One query batch now needs one FULL commit.
	for start := 0; start < len(requests); start += 5000 {
		end := start + 5000
		if end > len(requests) {
			end = len(requests)
		}
		j.requests <- requests[start:end]
	}
	j.mu.RUnlock()
	for _, request := range requests {
		<-request.done
	}
	if len(requests) == 0 {
		return 0, nil
	}
	return requests[len(requests)-1].id, nil
}
