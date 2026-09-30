package main

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	duckdbdriver "github.com/marcboeker/go-duckdb"
)

const (
	investigateWorkerArgument = "--svart-investigation-worker"
	investigateResultBytes    = 4 << 20
	investigateTransferBytes  = 8 << 20
	investigateResultRows     = 10000
	investigateResultCells    = 100000
)

// Tests with the race detector use an ordinary service executable because
// ThreadSanitizer reserves terabytes of writable shadow memory.
var investigateExecutable = os.Executable

var errInvestigateResource = errors.New("investigation resource limit exceeded; select fewer columns, aggregate, or page a smaller result")

type investigateOperation uint8

const (
	investigateQueryOperation investigateOperation = iota
	investigateSchemaOperation
)

type investigateWorkerRequest struct {
	Operation              investigateOperation
	SQL, Database, Archive string
}
type investigateWorkerResponse struct {
	Schema      []InvestigationColumn
	Columns     []string
	Rows        [][]interface{}
	Error       string
	Invalid     bool
	Resource    bool
	Unavailable bool
}

// Re-exec works for the installed binary and Go test executables, before either
// main starts servers/tests. No user SQL is parsed in the DNS process.
func init() {
	gob.Register(time.Time{})
	gob.Register(&big.Int{})
	gob.Register([]interface{}{})
	gob.Register(map[string]interface{}{})
	gob.Register(duckdbdriver.Decimal{})
	gob.Register(duckdbdriver.Interval{})
	gob.Register(duckdbdriver.Map{})
	if len(os.Args) == 2 && os.Args[1] == investigateWorkerArgument {
		os.Exit(runInvestigateWorker())
	}
}

func runInvestigateWorker() int {
	var request investigateWorkerRequest
	if err := gob.NewDecoder(os.Stdin).Decode(&request); err != nil {
		return 72
	}
	if len(request.SQL) > maxInvestigateRequestBodyBytes {
		return 72
	}
	if request.Operation != investigateQueryOperation && request.Operation != investigateSchemaOperation {
		return 72
	}
	response := executeInvestigateWorker(request)
	// Finish the complete response before writing anything; resource rejection
	// never masquerades as successful partial rows.
	var transfer investigateLimitedBuffer
	if err := gob.NewEncoder(&transfer).Encode(response); err != nil {
		transfer.Reset()
		response = investigateWorkerResponse{Resource: true}
		if err := gob.NewEncoder(&transfer).Encode(response); err != nil {
			return 73
		}
	}
	if _, err := os.Stdout.Write(transfer.Bytes()); err != nil {
		return 74
	}
	return 0
}

func executeInvestigateWorker(request investigateWorkerRequest) investigateWorkerResponse {
	if err := initDuckDB(request.Database, request.Archive); err != nil {
		return investigateWorkerResponse{Error: "investigation engine unavailable; check server storage and retry"}
	}
	if err := limitInvestigateWorker(); err != nil {
		os.Exit(71)
	}
	response := readInvestigateWorker(request)
	// Release every native SQLite scanner before Go SQLite opens the catalog
	// again. Each library has independent process-local WAL/SHM bookkeeping.
	if err := closeDuckDBChecked(); err != nil {
		slog.Error("investigation engine cleanup failed", "component", "investigation", "error", err)
		return investigateWorkerResponse{Unavailable: true}
	}
	if response.Error != "" || response.Invalid || response.Resource || response.Unavailable {
		return response
	}
	if err := checkSummaryAvailability(context.Background(), request.Database, request.Archive); err != nil {
		fmt.Fprintln(os.Stderr, "investigation archive integrity failed:", err)
		return investigateWorkerResponse{Unavailable: true}
	}
	return response
}

func readInvestigateWorker(request investigateWorkerRequest) (response investigateWorkerResponse) {
	ctx := context.Background()
	conn, err := duckDB.Conn(ctx)
	if err != nil {
		return investigateWorkerResponse{Resource: true}
	}
	defer func() {
		if err := conn.Close(); err != nil {
			slog.Error("investigation connection cleanup failed", "component", "investigation", "error", err)
			if response.Error == "" && !response.Invalid && !response.Resource && !response.Unavailable {
				response = investigateWorkerFailure(err)
			}
		}
	}()
	if request.Operation == investigateSchemaOperation {
		request.SQL = "SELECT * FROM query_logs LIMIT 0"
	} else if err := validateInvestigateSQL(ctx, conn, request.SQL); err != nil {
		return investigateWorkerResponse{Invalid: errors.Is(err, errInvalidInvestigateQuery), Error: err.Error()}
	}
	viewSQL, err := investigateViewSQL(ctx, request.Database, request.Archive)
	if err != nil {
		fmt.Fprintln(os.Stderr, "investigation archive discovery failed:", err)
		if errors.Is(err, errInvestigateArchiveSchema) {
			return investigateWorkerResponse{Error: "investigation data unavailable; check server storage and retry"}
		}
		return investigateWorkerResponse{Unavailable: true}
	}
	if _, err := conn.ExecContext(ctx, viewSQL); err != nil {
		return investigateWorkerResponse{Error: "investigation data unavailable; check server storage and retry"}
	}
	rows, err := conn.QueryContext(ctx, request.SQL)
	if err != nil {
		return investigateWorkerFailure(err)
	}
	defer closeQueryRows(rows)
	columns, err := rows.Columns()
	if err != nil {
		return investigateWorkerFailure(err)
	}
	encodedColumns, err := json.Marshal(columns)
	if err != nil {
		return investigateWorkerFailure(err)
	}
	size := len(encodedColumns) + 128 // response envelope and row delimiters
	response = investigateWorkerResponse{Columns: columns}
	if request.Operation == investigateSchemaOperation {
		types, err := rows.ColumnTypes()
		if err != nil {
			return investigateWorkerFailure(err)
		}
		for _, column := range types {
			response.Schema = append(response.Schema, InvestigationColumn{Name: column.Name(), Type: column.DatabaseTypeName()})
		}
	}
	cells := 0
	for rows.Next() {
		if len(response.Rows) == investigateResultRows || cells+len(columns) > investigateResultCells {
			return investigateWorkerResponse{Resource: true}
		}
		vals := make([]interface{}, len(columns))
		ptrs := make([]interface{}, len(columns))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return investigateWorkerFailure(err)
		}
		for i, v := range vals {
			if b, ok := v.([]byte); ok {
				vals[i] = string(b)
			}
		}
		encoded, err := json.Marshal(vals)
		if err != nil {
			return investigateWorkerFailure(err)
		}
		size += len(encoded) + 1
		if size > investigateResultBytes {
			return investigateWorkerResponse{Resource: true}
		}
		response.Rows = append(response.Rows, vals)
		cells += len(columns)
	}
	if err := rows.Err(); err != nil {
		return investigateWorkerFailure(err)
	}
	if err := rows.Close(); err != nil {
		return investigateWorkerFailure(err)
	}
	return response
}

func investigateWorkerFailure(err error) investigateWorkerResponse {
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "memory") || strings.Contains(text, "allocat") || strings.Contains(text, "out of range") {
		return investigateWorkerResponse{Resource: true}
	}
	// Native engine errors can contain data paths and excerpts; do not disclose
	// those across the HTTP boundary.
	return investigateWorkerResponse{Error: "investigation query failed; check selected columns, types, and expressions"}
}

type investigateLimitedBuffer struct{ buffer bytes.Buffer }

func (b *investigateLimitedBuffer) Len() int                   { return b.buffer.Len() }
func (b *investigateLimitedBuffer) Bytes() []byte              { return b.buffer.Bytes() }
func (b *investigateLimitedBuffer) Reset()                     { b.buffer.Reset() }
func (b *investigateLimitedBuffer) Read(p []byte) (int, error) { return b.buffer.Read(p) }

func (b *investigateLimitedBuffer) Write(p []byte) (int, error) {
	if len(p) > investigateTransferBytes-b.Len() {
		return 0, errInvestigateResource
	}
	return b.buffer.Write(p)
}

func investigateQueryContext(parent context.Context, sqlQuery string, timeoutSec int) ([]string, [][]interface{}, time.Duration, error) {
	start := time.Now()
	if strings.TrimSpace(sqlQuery) == "" {
		return nil, nil, 0, invalidInvestigate("sql is required")
	}
	if len(sqlQuery) > maxInvestigateRequestBodyBytes {
		return nil, nil, 0, errInvestigateResource
	}
	ctx, cancel := context.WithTimeout(parent, time.Duration(timeoutSec)*time.Second)
	defer cancel()
	response, err := runInvestigateProcess(ctx, investigateWorkerRequest{Operation: investigateQueryOperation, SQL: sqlQuery})
	return response.Columns, response.Rows, time.Since(start), err
}

// The shared lock spans both full archive verifications and the isolated read,
// preserving publication/retention ownership until the worker has been reaped.
func runInvestigateProcess(ctx context.Context, request investigateWorkerRequest) (investigateWorkerResponse, error) {
	if err := ctx.Err(); err != nil {
		return investigateWorkerResponse{}, err
	}
	if err := lockInvestigation(ctx, nil); err != nil {
		return investigateWorkerResponse{}, err
	}
	defer investigateMu.Unlock()
	if duckDB == nil {
		return investigateWorkerResponse{}, errors.New("DuckDB not initialized")
	}
	if err := ctx.Err(); err != nil {
		return investigateWorkerResponse{}, err
	}
	if request.Operation == investigateSchemaOperation {
		if _, err := db.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)"); err != nil {
			return investigateWorkerResponse{}, fmt.Errorf("checkpoint investigation source: %w", err)
		}
	}
	executable, err := investigateExecutable()
	if err != nil {
		return investigateWorkerResponse{}, errors.New("investigation worker unavailable")
	}
	scratch, err := os.MkdirTemp("", "svart-investigate-*")
	if err != nil {
		return investigateWorkerResponse{}, errors.New("investigation worker storage unavailable")
	}
	defer removeInvestigationScratch(scratch)
	request.Database, request.Archive = duckDBPath, duckDBArchive
	var input bytes.Buffer
	if err := gob.NewEncoder(&input).Encode(request); err != nil {
		return investigateWorkerResponse{}, err
	}
	// #nosec G204 -- executable is this process from os.Executable; the sole argument is constant. SQL travels only through bounded stdin.
	cmd := exec.CommandContext(ctx, executable, investigateWorkerArgument)
	configureInvestigateProcess(cmd)
	// No application credentials or loader overrides are inherited. The worker
	// needs only its private temporary directory and bounded Go concurrency.
	cmd.Env = []string{"TMPDIR=" + scratch, "GOMAXPROCS=2", "GOMEMLIMIT=96MiB", "GOGC=50"}
	cmd.Stdin = &input
	var output investigateLimitedBuffer
	cmd.Stdout = &output
	cmd.Stderr = os.Stderr
	cmd.WaitDelay = time.Second
	// Linux parent-death signals track the spawning OS thread. Keep it alive
	// until the child has been reaped, even if Go retires other threads.
	runtime.LockOSThread()
	err = cmd.Run() // always waits/reaps after kill; no orphan or abandoned engine
	runtime.UnlockOSThread()
	if ctx.Err() != nil {
		return investigateWorkerResponse{}, ctx.Err()
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 71 {
			return investigateWorkerResponse{}, errors.New("investigation resource isolation unavailable on this platform")
		}
		return investigateWorkerResponse{}, errInvestigateResource
	}
	var response investigateWorkerResponse
	if err := gob.NewDecoder(&output).Decode(&response); err != nil {
		return investigateWorkerResponse{}, errors.New("investigation worker returned an invalid response")
	}
	if response.Resource {
		return investigateWorkerResponse{}, errInvestigateResource
	}
	if response.Unavailable {
		return investigateWorkerResponse{}, errInvestigateArchiveUnavailable
	}
	if response.Invalid {
		return investigateWorkerResponse{}, fmt.Errorf("%w: %s", errInvalidInvestigateQuery, strings.TrimPrefix(response.Error, "invalid investigation query: "))
	}
	if response.Error != "" {
		return investigateWorkerResponse{}, errors.New(response.Error)
	}
	return response, nil
}
