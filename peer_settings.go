package main

import (
	"database/sql"
	"errors"
	"strings"
)

func changePeerSetting(tx *sql.Tx, key, peer string, add bool) (bool, error) {
	var current string
	err := tx.QueryRow("SELECT value FROM settings WHERE key=?", key).Scan(&current)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	parts := []string{}
	found := false
	for _, part := range strings.Split(current, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part == peer {
			found = true
			if !add {
				continue
			}
		}
		parts = append(parts, part)
	}
	if add && !found {
		parts = append(parts, peer)
	}
	ts, nid := syncNow()
	result, err := tx.Exec(`INSERT INTO settings(key,value,updated_at,node_id) VALUES(?,?,?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value,updated_at=excluded.updated_at,node_id=excluded.node_id`, key, strings.Join(parts, ","), ts, nid)
	return add && !found, checkListWrite(result, err, 1)
}
func addPeer(peer string, explicit bool) (bool, error) {
	var added bool
	err := localMutation(snapshotNone, func(tx *sql.Tx) error {
		var err error
		if !explicit {
			var deleted string
			if err := tx.QueryRow("SELECT value FROM settings WHERE key='deleted_peers'").Scan(&deleted); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			for _, candidate := range strings.Split(deleted, ",") {
				if strings.TrimSpace(candidate) == peer {
					return nil
				}
			}
		}
		if explicit {
			if _, err = changePeerSetting(tx, "deleted_peers", peer, false); err != nil {
				return err
			}
		}
		added, err = changePeerSetting(tx, "sync_peers", peer, true)
		return err
	})
	return added, err
}
