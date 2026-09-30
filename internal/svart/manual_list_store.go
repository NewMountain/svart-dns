package svart

import (
	"database/sql"
	"errors"
	"fmt"
)

type manualListOwner uint8

const (
	manualClient manualListOwner = iota
	manualGroup
	manualPolicy
	manualRange
)

// Creation and assignment belong to one write transaction. Reusing an existing
// manual list preserves its alias and enabled state, including disabled lists.
func getOrCreateManualList[T string | int | int64](store listStore, owner manualListOwner, value T) (int64, error) {
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer rollbackTransaction(tx)
	id, err := getOrCreateManualListTx(tx, store, owner, value)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func getOrCreateManualListTx[T string | int | int64](tx *sql.Tx, store listStore, owner manualListOwner, value T) (int64, error) {
	var assignment, ownerColumn, nameTable string
	aliasPattern := "Manual (%s)"
	if store.block {
		aliasPattern = "Manual Block (%s)"
	}
	switch owner {
	case manualClient:
		assignment, ownerColumn = "client_"+store.lists, "client_ip"
	case manualGroup:
		assignment, ownerColumn, nameTable = "group_"+store.lists, "group_id", "client_groups"
	case manualPolicy:
		assignment, ownerColumn, nameTable = "policy_"+store.lists, "policy_id", "policies"
		aliasPattern = "Manual Allow (Policy: %s)"
		if store.block {
			aliasPattern = "Manual Block (Policy: %s)"
		}
	case manualRange:
		assignment, ownerColumn, nameTable = "range_"+store.lists, "range_id", "ip_ranges"
		aliasPattern = "Manual Allow (Range: %s)"
		if store.block {
			aliasPattern = "Manual Block (Range: %s)"
		}
	default:
		return 0, fmt.Errorf("unknown manual list owner %d", owner)
	}
	var id int64
	query := "SELECT l.id FROM " + store.lists + " l JOIN " + assignment + " a ON l.id=a." + store.idColumn + " WHERE a." + ownerColumn + "=? AND l.url=''"
	if err := tx.QueryRow(query, value).Scan(&id); err == nil {
		return id, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	name := fmt.Sprint(value)
	if nameTable != "" {
		if err := tx.QueryRow("SELECT name FROM "+nameTable+" WHERE id=?", value).Scan(&name); err != nil {
			return 0, err
		}
	}
	ts, nodeID := syncNow()
	// #nosec G202 G701 -- Owner enum and fixed listStore select schema identifiers; client/group/policy/range values use placeholders.
	result, err := tx.Exec("INSERT INTO "+store.lists+"(url,alias,enabled,updated_at,node_id) VALUES('',?,1,?,?)", fmt.Sprintf(aliasPattern, name), ts, nodeID)
	if err := checkListWrite(result, err, 1); err != nil {
		return 0, fmt.Errorf("create manual %s: %w", store.kind, err)
	}
	id, err = result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("manual %s identity: %w", store.kind, err)
	}
	// #nosec G202 G701 -- Owner enum and fixed listStore select schema identifiers; client/group/policy/range values use placeholders.
	result, err = tx.Exec("INSERT INTO "+assignment+"("+ownerColumn+","+store.idColumn+",updated_at,node_id) VALUES(?,?,?,?)", value, id, ts, nodeID)
	if err := checkListWrite(result, err, 1); err != nil {
		return 0, fmt.Errorf("assign manual %s: %w", store.kind, err)
	}
	return id, nil
}
