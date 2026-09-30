package main

import (
	"database/sql"
	"fmt"
	"io"
	"strings"

	"github.com/yeti/svart-dns/internal/listparse"
)

// listWriterRole identifies the separately qualified reader-first bridge build.
// Normal builds activate the typed writer; bridge builds are linked with
// -X main.listWriterRole=bridge and honor an already durable activation.
var listWriterRole = "active"

type downloadedList struct {
	listparse.Result
	typed bool
}

// initListWriterCapability runs in the startup schema transaction, before any
// worker can publish a refresh. This state is deliberately local, never a
// synchronized setting. A bridge binary cannot reverse durable activation.
func initListWriterCapability(tx *sql.Tx) error {
	if listWriterRole != "active" && listWriterRole != "bridge" {
		return fmt.Errorf("unknown list writer build role %q", listWriterRole)
	}
	if _, err := tx.Exec(`CREATE TABLE IF NOT EXISTS local_list_storage_capabilities (
 singleton INTEGER PRIMARY KEY CHECK(singleton=1),
 typed_rules_v1 INTEGER NOT NULL CHECK(typed_rules_v1 IN (0,1))
 )`); err != nil {
		return fmt.Errorf("create list writer capability: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO local_list_storage_capabilities(singleton,typed_rules_v1) VALUES(1,0) ON CONFLICT(singleton) DO NOTHING`); err != nil {
		return fmt.Errorf("initialize list writer capability: %w", err)
	}
	active, err := typedListWriterEnabled(tx)
	if err != nil {
		return err
	}
	if listWriterRole == "active" && !active {
		result, err := tx.Exec("UPDATE local_list_storage_capabilities SET typed_rules_v1=1 WHERE singleton=1 AND typed_rules_v1=0")
		if err := checkListWrite(result, err, 1); err != nil {
			return fmt.Errorf("activate list writer capability: %w", err)
		}
	}
	return nil
}

func typedListWriterEnabled(q interface{ QueryRow(string, ...any) *sql.Row }) (bool, error) {
	if q == nil {
		return false, fmt.Errorf("read list writer capability: database unavailable")
	}
	var active bool
	if err := q.QueryRow("SELECT typed_rules_v1 FROM local_list_storage_capabilities WHERE singleton=1").Scan(&active); err != nil {
		return false, fmt.Errorf("read list writer capability: %w", err)
	}
	return active, nil
}

func parseDownloadedList(body io.Reader, maxBytes int64, maxLines int) (downloadedList, error) {
	if readDB == nil {
		return downloadedList{}, fmt.Errorf("read list writer capability: database unavailable")
	}
	active, err := typedListWriterEnabled(readDB)
	if err != nil {
		return downloadedList{}, err
	}
	var result listparse.Result
	if active {
		result, err = listparse.Parse(body, maxBytes, maxLines)
	} else {
		result, err = listparse.ParseLegacy(body, maxBytes, maxLines)
	}
	return downloadedList{Result: result, typed: active}, err
}

// Validate inside the same write transaction that will replace the rules. A
// legacy download begun before activation may not overwrite a typed generation.
func validateListWriterGeneration(tx *sql.Tx, rows []string, parsedTyped []bool) error {
	active, err := typedListWriterEnabled(tx)
	if err != nil {
		return err
	}
	if len(parsedTyped) > 0 && parsedTyped[0] != active {
		return fmt.Errorf("list writer capability changed during download; retry the refresh")
	}
	if !active {
		for _, row := range rows {
			if strings.HasPrefix(row, "!svart-rule-") {
				return fmt.Errorf("typed list rule requires durable writer activation")
			}
		}
	}
	return nil
}
