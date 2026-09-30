package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/yeti/svart-dns/internal/telemetry"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type lokiEntry struct {
	ts              int64
	line, component string
}
type lokiRecord struct {
	TS        int64  `json:"ts"`
	Line      string `json:"line"`
	Component string `json:"component"`
}
type lokiRender struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}
type lokiHandler struct {
	inner       slog.Handler
	sender      *lokiSender
	render      *lokiRender
	jsonHandler slog.Handler
	component   string
}
type lokiSender struct {
	journal            *durableJournal
	client             *http.Client
	endpoint, instance string
	wake, stop, done   chan struct{}
	cancel             context.CancelFunc
	ctx                context.Context
	once               sync.Once
	dnsRequests        chan []byte
	dnsDone            chan struct{}
	dnsMu              sync.RWMutex
	dnsClosed          bool
}

var activeLoki *lokiSender

func initLoki() {
	url := os.Getenv("LOKI_URL")
	if url == "" {
		slog.Info("loki disabled (LOKI_URL not set)")
		return
	}
	instance := os.Getenv("NODE_ID")
	if instance == "" {
		var err error
		instance, err = os.Hostname()
		if err != nil {
			panic(fmt.Errorf("loki node identity unavailable: %w", err))
		}
	}
	sender, err := newLokiSender(getEnv("DB_PATH", "./svart-dns.db")+".loki.sqlite", url+"/loki/api/v1/push", instance, &http.Client{Timeout: 5 * time.Second})
	if err != nil {
		panic(fmt.Errorf("loki durable delivery unavailable; repair storage before startup: %w", err))
	}
	activeLoki = sender
	slog.SetDefault(slog.New(newLokiHandler(loggingRootHandler, sender)).With("service", "svart-dns"))
	recreateComponentLoggers()
	slog.Info("loki enabled", "endpoint", url, "instance", instance)
}
func newLokiHandler(inner slog.Handler, sender *lokiSender) *lokiHandler {
	render := &lokiRender{}
	return &lokiHandler{inner: inner, sender: sender, render: render, jsonHandler: slog.NewJSONHandler(&render.buffer, &slog.HandlerOptions{Level: slog.LevelDebug})}
}
func shutdownLoki(ctx context.Context) {
	if activeLoki != nil {
		if err := activeLoki.shutdown(ctx); err != nil {
			panic(fmt.Errorf("loki shutdown incomplete; pending records retained: %w", err))
		}
	}
}
func (h *lokiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}
func (h *lokiHandler) Handle(ctx context.Context, r slog.Record) error {
	if h.component != "dns" && h.component != "resolver" {
		return h.HandleBatch(ctx, []slog.Record{r})
	}
	outputErr := h.inner.Handle(ctx, r)
	payload, err := h.encodeRecord(r)
	if err != nil {
		return errors.Join(outputErr, err)
	}
	return errors.Join(outputErr, h.sender.enqueueDNS(payload))
}

// HandleBatch retains stdout's exact records and lets a query presentation
// batch share durable Loki admission instead of fsyncing each row separately.
func (h *lokiHandler) HandleBatch(ctx context.Context, records []slog.Record) error {
	var outputErr error
	payloads := make([][]byte, 0, len(records))
	for _, record := range records {
		if !h.Enabled(ctx, record.Level) {
			continue
		}
		outputErr = errors.Join(outputErr, h.inner.Handle(ctx, record))
		payload, err := h.encodeRecord(record)
		if err != nil {
			return errors.Join(outputErr, err)
		}
		payloads = append(payloads, payload)
	}
	if len(payloads) == 0 {
		return outputErr
	}
	_, err := h.sender.journal.appendBatch(payloads)
	if err == nil {
		select {
		case h.sender.wake <- struct{}{}:
		default:
		}
	}
	return errors.Join(outputErr, err)
}

// encodeRecord preserves handler attributes/groups without emitting stdout.
func (h *lokiHandler) encodeRecord(record slog.Record) ([]byte, error) {
	h.render.mu.Lock()
	defer h.render.mu.Unlock()
	h.render.buffer.Reset()
	if err := h.jsonHandler.Handle(context.Background(), record); err != nil {
		return nil, err
	}
	return json.Marshal(lokiRecord{TS: record.Time.UnixNano(), Line: h.render.buffer.String(), Component: h.component})
}

func (h *lokiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	component := h.component
	for _, attr := range attrs {
		if attr.Key == "component" {
			component = attr.Value.String()
		}
	}
	return &lokiHandler{inner: h.inner.WithAttrs(attrs), sender: h.sender, render: h.render, jsonHandler: h.jsonHandler.WithAttrs(attrs), component: component}
}
func (h *lokiHandler) WithGroup(name string) slog.Handler {
	return &lokiHandler{inner: h.inner.WithGroup(name), sender: h.sender, render: h.render, jsonHandler: h.jsonHandler.WithGroup(name), component: h.component}
}
func newLokiSender(path, endpoint, instance string, client *http.Client) (*lokiSender, error) {
	// #nosec G704 -- Endpoint is trusted operator configuration; self-hosted private receivers are supported.
	if _, err := http.NewRequest(http.MethodPost, endpoint, nil); err != nil {
		return nil, err
	}
	journal, err := openDurableJournal(path)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	sender := &lokiSender{journal: journal, endpoint: endpoint, instance: instance, client: client, wake: make(chan struct{}, 1), stop: make(chan struct{}), done: make(chan struct{}), ctx: ctx, cancel: cancel, dnsRequests: make(chan []byte, 4096), dnsDone: make(chan struct{})}
	go sender.runDNSQueue()
	go sender.run()
	return sender, nil
}

// DNS warnings/debug records share the accepted asynchronous reply contract.
// This queue never spawns per-record goroutines; full capacity backpressures
// bounded DNS handlers, and clean shutdown drains every admitted record.
func (s *lokiSender) enqueueDNS(payload []byte) error {
	s.dnsMu.RLock()
	defer s.dnsMu.RUnlock()
	if s.dnsClosed {
		return errors.New("loki DNS queue closed: event not admitted")
	}
	s.dnsRequests <- payload
	return nil
}
func (s *lokiSender) runDNSQueue() {
	defer close(s.dnsDone)
	for first := range s.dnsRequests {
		batch := [][]byte{first}
	gather:
		for len(batch) < 5000 {
			select {
			case next, ok := <-s.dnsRequests:
				if !ok {
					break gather
				}
				batch = append(batch, next)
			default:
				break gather
			}
		}
		if _, err := s.journal.appendBatch(batch); err != nil {
			panic(fmt.Sprintf("Loki DNS drain failed: %v", err))
		}
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

func (s *lokiSender) run() {
	defer close(s.done)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	retryPending := false
	for {
		select {
		case <-s.stop:
			return
		case <-s.wake:
			if retryPending {
				continue
			}
		case <-ticker.C:
			retryPending = false
		}
		for {
			if err := s.deliver(); err != nil {
				retryPending = true
				if !errors.Is(err, context.Canceled) {
					fmt.Fprintf(os.Stderr, "Loki delivery unavailable: %v; raw batch retained at %s\n", err, s.journal.path)
				}
				break
			}
			// deliver drains one bounded batch; do not spin when caught up.
			rows, err := s.pending(1)
			if err != nil || len(rows) == 0 {
				break
			}
			select {
			case <-s.stop:
				return
			default:
			}
		}
	}
}
func (s *lokiSender) pending(n int) ([]journalRow, error) {
	var cursor int64
	err := s.journal.db.QueryRow("SELECT CAST(value AS INTEGER) FROM journal_meta WHERE key='loki_ack'").Scan(&cursor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return s.journal.batch(cursor, n)
}
func (s *lokiSender) deliver() (deliveryErr error) {
	_, complete := telemetry.Begin(context.Background(), telemetry.LokiDelivery)
	defer func() { complete(deliveryErr) }()
	rows, err := s.pending(1000)
	if err != nil || len(rows) == 0 {
		return err
	}
	batch := make(map[string][]lokiEntry)
	for _, row := range rows {
		var record lokiRecord
		if err = json.Unmarshal(row.payload, &record); err != nil {
			return fmt.Errorf("loki journal record %d: %w", row.id, err)
		}
		component := record.Component
		if component == "" {
			component = "main"
		}
		batch[component] = append(batch[component], lokiEntry{ts: record.TS, line: record.Line, component: component})
	}
	if err = s.post(lokiBody(s.instance, batch)); err != nil {
		return err
	}
	// Only a successful receiver acknowledgement advances the cursor. If its
	// persistence fails, retry the exact original timestamp and payload.
	tx, err := s.journal.db.Begin()
	if err != nil {
		return err
	}
	defer func() {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			deliveryErr = errors.Join(deliveryErr, rollbackErr)
		}
	}()
	next := rows[len(rows)-1].id
	if _, err = tx.Exec("INSERT INTO journal_meta(key,value) VALUES('loki_ack',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", strconv.FormatInt(next, 10)); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM journal_records WHERE id<=?", next); err != nil {
		return err
	}
	return tx.Commit()
}
func (s *lokiSender) post(body []byte) error {
	req, err := http.NewRequestWithContext(s.ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	_, readErr := io.Copy(io.Discard, resp.Body)
	closeErr := resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("loki receiver status %d", resp.StatusCode)
	}
	return errors.Join(readErr, closeErr)
}
func (s *lokiSender) shutdown(ctx context.Context) error {
	s.once.Do(func() {
		s.dnsMu.Lock()
		s.dnsClosed = true
		close(s.dnsRequests)
		s.dnsMu.Unlock()
		close(s.stop)
		s.cancel()
	})
	select {
	case <-s.dnsDone:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.done:
		return s.journal.close()
	case <-ctx.Done():
		return ctx.Err()
	}
}
func lokiBody(instance string, batch map[string][]lokiEntry) []byte {
	var buf bytes.Buffer
	buf.WriteString(`{"streams":[`)
	first := true
	for component, entries := range batch {
		if !first {
			buf.WriteByte(',')
		}
		first = false
		buf.WriteString(`{"stream":{"job":"svart-dns","instance":`)
		writeJSONString(&buf, instance)
		buf.WriteString(`,"component":`)
		writeJSONString(&buf, component)
		buf.WriteString(`},"values":[`)
		for i, e := range entries {
			if i > 0 {
				buf.WriteByte(',')
			}
			buf.WriteString(`["`)
			buf.WriteString(strconv.FormatInt(e.ts, 10))
			buf.WriteString(`",`)
			writeJSONString(&buf, e.line)
			buf.WriteByte(']')
		}
		buf.WriteString(`]}`)
	}
	buf.WriteString(`]}`)
	return buf.Bytes()
}

// writeJSONString writes a JSON-escaped string (with quotes) to buf.
func writeJSONString(buf *bytes.Buffer, s string) {
	buf.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if c < 0x20 {
				fmt.Fprintf(buf, `\u%04x`, c)
			} else {
				buf.WriteByte(c)
			}
		}
	}
	buf.WriteByte('"')
}
