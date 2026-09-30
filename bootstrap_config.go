package main

import "database/sql"

// Replacement rows and removal metadata share the caller's transaction: a failed
// import cannot advertise deletions, and a peer cannot resurrect removed servers.
func replaceBootstrapServers(tx *sql.Tx, servers []string, timestamp, nodeID string) error {
	var previous int64
	if err := tx.QueryRow("SELECT COUNT(*) FROM bootstrap_servers").Scan(&previous); err != nil {
		return err
	}
	result, err := tx.Exec(`INSERT OR REPLACE INTO sync_tombstones(table_name,natural_key,deleted_at,node_id)
		SELECT 'bootstrap_servers',server,?,? FROM bootstrap_servers`, timestamp, nodeID)
	if err := checkListWrite(result, err, previous); err != nil {
		return err
	}
	result, err = tx.Exec("DELETE FROM bootstrap_servers")
	if err := checkListWrite(result, err, previous); err != nil {
		return err
	}
	for _, server := range servers {
		if err := addBootstrapServer(tx, server, timestamp, nodeID); err != nil {
			return err
		}
	}
	return nil
}

func addBootstrapServer(tx *sql.Tx, server, timestamp, nodeID string) error {
	result, err := tx.Exec("INSERT INTO bootstrap_servers(server,updated_at,node_id) VALUES(?,?,?)", server, timestamp, nodeID)
	if err := checkListWrite(result, err, 1); err != nil {
		return err
	}
	// A retained/re-added server is live, not a deletion at an equal timestamp.
	var previous int64
	if err := tx.QueryRow("SELECT COUNT(*) FROM sync_tombstones WHERE table_name='bootstrap_servers' AND natural_key=?", server).Scan(&previous); err != nil {
		return err
	}
	result, err = tx.Exec("DELETE FROM sync_tombstones WHERE table_name='bootstrap_servers' AND natural_key=?", server)
	return checkListWrite(result, err, previous)
}
