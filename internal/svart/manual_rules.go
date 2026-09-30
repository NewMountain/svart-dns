package svart

import (
	"database/sql"
	"errors"
	"fmt"
	"github.com/miekg/dns"
	"github.com/yeti/svart-dns/internal/policycore"
	"net/http"
	"net/url"
	"strings"
)

func validManualDomain(domain string) bool {
	name := strings.TrimPrefix(domain, "*.")
	_, ok := dns.IsDomainName(name)
	return name != "" && !strings.ContainsAny(name, " \t\r\n/*|:") && ok
}

func manualAssignment(store listStore, owner manualListOwner) (string, string) {
	switch owner {
	case manualClient:
		return "client_" + store.lists, "client_ip"
	case manualGroup:
		return "group_" + store.lists, "group_id"
	case manualPolicy:
		return "policy_" + store.lists, "policy_id"
	case manualRange:
		return "range_" + store.lists, "range_id"
	}
	panic("unknown manual owner")
}
func readManualDomains(q queryer, store listStore, owner manualListOwner, value any) ([]string, error) {
	assignment, column := manualAssignment(store, owner)
	domains := []string{}
	err := scanRows(q, "SELECT d.domain FROM "+store.rules+" d JOIN "+store.lists+" l ON l.id=d."+store.idColumn+" JOIN "+assignment+" a ON a."+store.idColumn+"=l.id WHERE a."+column+"=? AND l.url='' ORDER BY d.domain", func(rows *sql.Rows) error {
		var domain string
		if err := rows.Scan(&domain); err != nil {
			return err
		}
		domains = append(domains, domain)
		return nil
	}, value)
	return domains, err
}

type manualRuleConflict struct{ rule string }

func (e manualRuleConflict) Error() string { return "conflicts with custom rule: " + e.rule }

func mutateManualRule[T string | int](store listStore, owner manualListOwner, value T, domain string, remove bool) error {
	var keys []policycore.ListKey
	return localMutationLists(snapshotPolicy, func(tx *sql.Tx) error {
		var ids []int64
		if remove {
			assignment, column := manualAssignment(store, owner)
			// GET merges all assigned manual lists; DELETE removes this domain from
			// every matching list, preserving unrelated rules and assignments.
			err := scanRows(tx, "SELECT l.id FROM "+store.lists+" l JOIN "+assignment+" a ON a."+store.idColumn+"=l.id WHERE a."+column+"=? AND l.url='' ORDER BY l.id", func(rows *sql.Rows) error {
				var id int64
				if err := rows.Scan(&id); err != nil {
					return err
				}
				ids = append(ids, id)
				return nil
			}, value)
			if err != nil {
				return err
			}
			if len(ids) == 0 {
				return sql.ErrNoRows
			}
		} else {
			opposite := blockListStore
			if store.block {
				opposite = allowListStore
			}
			domains, err := readManualDomains(tx, opposite, owner, value)
			if err != nil {
				return err
			}
			if conflict, rule := domainConflicts(domains, domain); conflict {
				return manualRuleConflict{rule}
			}
			id, err := getOrCreateManualListTx(tx, store, owner, value)
			if err != nil {
				return err
			}
			ids = []int64{id}
		}
		for _, id := range ids {
			if err := writeManualRule(tx, store, id, domain, remove); err != nil {
				return err
			}
			keys = append(keys, policycore.ListKey{ID: int(id), Allow: !store.block})
		}
		return nil
	}, func() []policycore.ListKey { return keys })
}

func writeManualRule(tx *sql.Tx, store listStore, id int64, domain string, remove bool) error {
	var count int64
	if err := tx.QueryRow("SELECT COUNT(*) FROM "+store.rules+" WHERE "+store.idColumn+"=? AND domain=?", id, domain).Scan(&count); err != nil {
		return err
	}
	if remove {
		var alias string
		if err := tx.QueryRow("SELECT alias FROM "+store.lists+" WHERE id=?", id).Scan(&alias); err != nil {
			return err
		}
		if err := writeLocalTombstone(tx, store.rules, alias+"|"+domain); err != nil {
			return err
		}
		// #nosec G202 G701 -- Identifiers are fixed blockListStore/allowListStore fields; rule text and IDs use placeholders.
		result, err := tx.Exec("DELETE FROM "+store.rules+" WHERE "+store.idColumn+"=? AND domain=?", id, domain)
		if err := checkListWrite(result, err, count); err != nil {
			return err
		}
	} else {
		ts, nid := syncNow()
		if count == 0 {
			// #nosec G202 G701 -- Identifiers are fixed blockListStore/allowListStore fields; rule text and IDs use placeholders.
			result, err := tx.Exec("INSERT INTO "+store.rules+"("+store.idColumn+",domain,updated_at,node_id) VALUES(?,?,?,?)", id, domain, ts, nid)
			if err := checkListWrite(result, err, 1); err != nil {
				return err
			}
		} else {
			if err := updateMatchingRows(tx, store.rules, "updated_at=?,node_id=?", store.idColumn+"=? AND domain=?", []any{ts, nid}, id, domain); err != nil {
				return err
			}
		}
	}
	// #nosec G202 G701 -- Identifiers are fixed blockListStore/allowListStore fields; rule text and IDs use placeholders.
	result, err := tx.Exec("UPDATE "+store.lists+" SET domain_count=(SELECT COUNT(*) FROM "+store.rules+" WHERE "+store.idColumn+"=?) WHERE id=?", id, id)
	return checkListWrite(result, err, 1)
}

func handleManualRule[T string | int](w http.ResponseWriter, r *http.Request, store listStore, owner manualListOwner, value T, parts []string) {
	var domain string
	switch r.Method {
	case "POST":
		var req DomainRuleRequest
		if !decodeJSONBody(w, r, maxSmallAdminRequestBodyBytes, &req) {
			return
		}
		domain = strings.ToLower(strings.TrimSpace(req.Domain))
	case "DELETE":
		if len(parts) != 3 {
			writeError(w, http.StatusBadRequest, "domain required in path")
			return
		}
		var err error
		domain, err = url.PathUnescape(parts[2])
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid domain escape")
			return
		}
		domain = strings.ToLower(strings.TrimSpace(domain))
	default:
		writeError(w, http.StatusMethodNotAllowed, "Method not allowed")
		return
	}
	if !validManualDomain(domain) {
		writeError(w, http.StatusBadRequest, "valid domain required")
		return
	}
	if err := mutateManualRule(store, owner, value, domain, r.Method == "DELETE"); err != nil {
		var conflict manualRuleConflict
		if errors.As(err, &conflict) {
			writeError(w, http.StatusConflict, fmt.Sprintf("conflicts with custom rule: %s", conflict.rule))
			return
		}
		writeDBLookupError(w, err, "manual list or owner not found")
		return
	}
	writeJSON(w, http.StatusOK, SuccessResponse{Success: true})
}
