package main

import (
	"bytes"
	"database/sql"
	"fmt"
)

func requireOneMutation(result sql.Result) error {
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("ownership mutation affected %d rows, expected 1", n)
	}
	return nil
}

func insertExactOutbox(tx *sql.Tx, key string, payload []byte) error {
	result, err := tx.Exec("INSERT OR IGNORE INTO query_loki_outbox(id,payload) VALUES(?,?)", key, payload)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	if n != 0 {
		return fmt.Errorf("outbox mutation affected %d rows", n)
	}
	var stored []byte
	if err := tx.QueryRow("SELECT payload FROM query_loki_outbox WHERE id=?", key).Scan(&stored); err != nil {
		return err
	}
	if !bytes.Equal(stored, payload) {
		return fmt.Errorf("outbox identity conflicts with committed payload")
	}
	return nil
}
