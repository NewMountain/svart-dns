package main

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/yeti/svart-dns/internal/listparse"
)

// ListCompatibilityReport belongs to one successfully stored local list generation.
// Applied counts effective rules, excluding blocklist exceptions, like domain_count.
// Diagnostics preserves every rejected source line; pagination only limits a response.
type ListCompatibilityReport struct {
	Version     int                    `json:"version"`
	AssessedAt  string                 `json:"assessed_at"`
	Lines       int                    `json:"lines"`
	Applied     int                    `json:"applied"`
	Unsupported int                    `json:"unsupported"`
	Invalid     int                    `json:"invalid"`
	Diagnostics []listparse.Diagnostic `json:"diagnostics"`
}

type ListCompatibilitySummary struct {
	Assessed        bool   `json:"assessed"`
	AssessedAt      string `json:"assessed_at"`
	Lines           int    `json:"lines"`
	Applied         int    `json:"applied"`
	Unsupported     int    `json:"unsupported"`
	Invalid         int    `json:"invalid"`
	DiagnosticCount int    `json:"diagnostic_count"`
}

type ListCompatibilityPage struct {
	Summary     ListCompatibilitySummary `json:"summary"`
	Diagnostics []listparse.Diagnostic   `json:"diagnostics"`
	Total       int                      `json:"total"`
	Offset      int                      `json:"offset"`
	Limit       int                      `json:"limit"`
}

func compatibilitySummary(report *ListCompatibilityReport) ListCompatibilitySummary {
	if report == nil {
		return ListCompatibilitySummary{}
	}
	return ListCompatibilitySummary{Assessed: true, AssessedAt: report.AssessedAt, Lines: report.Lines, Applied: report.Applied, Unsupported: report.Unsupported, Invalid: report.Invalid, DiagnosticCount: len(report.Diagnostics)}
}

func handleAPIListCompatibility(w http.ResponseWriter, r *http.Request, kind string, id int) {
	limit, offset, err := pageParams(r.URL.Query(), 50, 1000)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	report, err := readListCompatibilityReport(readDB, kind, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeError(w, http.StatusNotFound, "List not found")
		return
	}
	if err != nil {
		writeDBError(w, err)
		return
	}
	page := ListCompatibilityPage{Summary: compatibilitySummary(report), Diagnostics: []listparse.Diagnostic{}, Offset: offset, Limit: limit}
	if report != nil {
		page.Total = len(report.Diagnostics)
		if offset < page.Total {
			page.Diagnostics = report.Diagnostics[offset:min(offset+limit, page.Total)]
		}
	}
	writeJSON(w, http.StatusOK, page)
}
