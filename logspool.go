package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
)

// logSpool retains every original event, including those summarized in the UI.
// read/written are immutable journal sequence numbers, not byte offsets.
type logSpool struct {
	path, offsetPath       string
	journal                *durableJournal
	written, read, spilled atomic.Int64
}

// spoolRecord is the on-disk form of a queryLogEntry. queryLogEntry keeps its
// fields unexported for the hot-path layout, so the spool maps them here.
type spoolRecord struct {
	TS                  int64  `json:"ts"`
	ClientIP            string `json:"client_ip"`
	QueryName           string `json:"query_name"`
	QueryType           string `json:"query_type"`
	ResponseCode        string `json:"response_code"`
	Upstream            string `json:"upstream,omitempty"`
	Result              string `json:"result,omitempty"`
	ResultReason        string `json:"result_reason,omitempty"`
	ResultTier          string `json:"result_tier,omitempty"`
	ResultEntity        string `json:"result_entity,omitempty"`
	ResultRule          string `json:"result_rule,omitempty"`
	ResultListName      string `json:"result_list_name,omitempty"`
	RangeResult         string `json:"range_result,omitempty"`
	RangeEntity         string `json:"range_entity,omitempty"`
	RangeRule           string `json:"range_rule,omitempty"`
	RangeListName       string `json:"range_list_name,omitempty"`
	GroupResult         string `json:"group_result,omitempty"`
	GroupEntity         string `json:"group_entity,omitempty"`
	GroupRule           string `json:"group_rule,omitempty"`
	GroupListName       string `json:"group_list_name,omitempty"`
	IPResult            string `json:"ip_result,omitempty"`
	IPEntity            string `json:"ip_entity,omitempty"`
	IPRule              string `json:"ip_rule,omitempty"`
	IPListName          string `json:"ip_list_name,omitempty"`
	LatencyMicroseconds int64  `json:"latency_us"`
	CoalescedCount      int    `json:"coalesced_count"`
	ResultListID        int    `json:"result_list_id,omitempty"`
	RangeListID         int    `json:"range_list_id,omitempty"`
	GroupListID         int    `json:"group_list_id,omitempty"`
	IPListID            int    `json:"ip_list_id,omitempty"`
	Blocked             bool   `json:"blocked,omitempty"`
	ResultIsPublished   bool   `json:"result_is_published,omitempty"`
	RangeIsPublished    bool   `json:"range_is_published,omitempty"`
	GroupIsPublished    bool   `json:"group_is_published,omitempty"`
	IPIsPublished       bool   `json:"ip_is_published,omitempty"`
}

func toSpoolRecord(e *queryLogEntry) spoolRecord {
	return spoolRecord{
		TS: e.ts, ClientIP: e.clientIP, QueryName: e.queryName, QueryType: e.queryType,
		ResponseCode: e.responseCode, Upstream: e.upstream,
		Result: e.result, ResultReason: e.resultReason, ResultTier: e.resultTier, ResultEntity: e.resultEntity,
		ResultRule: e.resultRule, ResultListName: e.resultListName,
		RangeResult: e.rangeResult, RangeEntity: e.rangeEntity, RangeRule: e.rangeRule, RangeListName: e.rangeListName,
		GroupResult: e.groupResult, GroupEntity: e.groupEntity, GroupRule: e.groupRule, GroupListName: e.groupListName,
		IPResult: e.ipResult, IPEntity: e.ipEntity, IPRule: e.ipRule, IPListName: e.ipListName,
		LatencyMicroseconds: e.latencyMicroseconds, CoalescedCount: e.coalescedCount,
		ResultListID: e.resultListID, RangeListID: e.rangeListID, GroupListID: e.groupListID, IPListID: e.ipListID,
		Blocked: e.blocked, ResultIsPublished: e.resultIsPublished, RangeIsPublished: e.rangeIsPublished,
		GroupIsPublished: e.groupIsPublished, IPIsPublished: e.ipIsPublished,
	}
}

func (r *spoolRecord) entry() queryLogEntry {
	return queryLogEntry{
		ts: r.TS, clientIP: r.ClientIP, queryName: r.QueryName, queryType: r.QueryType,
		responseCode: r.ResponseCode, upstream: r.Upstream,
		result: r.Result, resultReason: r.ResultReason, resultTier: r.ResultTier, resultEntity: r.ResultEntity,
		resultRule: r.ResultRule, resultListName: r.ResultListName,
		rangeResult: r.RangeResult, rangeEntity: r.RangeEntity, rangeRule: r.RangeRule, rangeListName: r.RangeListName,
		groupResult: r.GroupResult, groupEntity: r.GroupEntity, groupRule: r.GroupRule, groupListName: r.GroupListName,
		ipResult: r.IPResult, ipEntity: r.IPEntity, ipRule: r.IPRule, ipListName: r.IPListName,
		latencyMicroseconds: r.LatencyMicroseconds, coalescedCount: r.CoalescedCount,
		resultListID: r.ResultListID, rangeListID: r.RangeListID, groupListID: r.GroupListID, ipListID: r.IPListID,
		blocked: r.Blocked, resultIsPublished: r.ResultIsPublished, rangeIsPublished: r.RangeIsPublished,
		groupIsPublished: r.GroupIsPublished, ipIsPublished: r.IPIsPublished,
	}
}

func openLogSpool(path string) (*logSpool, error) {
	j, err := openDurableJournal(path + ".sqlite")
	if err != nil {
		return nil, err
	}
	s := &logSpool{path: path, offsetPath: path + ".offset", journal: j}
	if err = s.importLegacy(); err != nil {
		return nil, errors.Join(err, j.close())
	}
	var last int64
	if err = j.db.QueryRow("SELECT COALESCE(MAX(id),0) FROM journal_records").Scan(&last); err != nil {
		return nil, errors.Join(err, j.close())
	}
	s.written.Store(last)
	return s, nil
}

func (s *logSpool) spill(entries ...*queryLogEntry) {
	payloads := make([][]byte, len(entries))
	for i, e := range entries {
		record := toSpoolRecord(e)
		payload, err := json.Marshal(record)
		if err != nil {
			panic(err)
		}
		payloads[i] = payload
	}
	id, err := s.journal.appendBatch(payloads)
	if err != nil {
		panic(fmt.Sprintf("query admission failed: %v", err))
	}
	for old := s.written.Load(); id > old; old = s.written.Load() {
		if s.written.CompareAndSwap(old, id) {
			break
		}
	}
	s.spilled.Add(int64(len(entries)))
}
func (s *logSpool) pending() bool { return s.read.Load() < s.written.Load() }
func (s *logSpool) readBatch(n int) ([]queryLogEntry, int64, error) {
	next := s.read.Load()
	rows, err := s.journal.batch(next, n)
	if err != nil {
		return nil, next, err
	}
	var result []queryLogEntry
	for _, row := range rows {
		var record spoolRecord
		if err = json.Unmarshal(row.payload, &record); err != nil {
			return nil, next, fmt.Errorf("journal record %d: %w", row.id, err)
		}
		result = append(result, record.entry())
		next = row.id
	}
	return result, next, nil
}

func (s *logSpool) close() {
	if err := s.journal.close(); err != nil {
		panic(fmt.Sprintf("close durable query journal: %v", err))
	}
}
