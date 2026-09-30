package main

import "testing"

func TestClientHistoryPreservesUnparseableStoredTimestamp(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	for _, statement := range []string{
		`INSERT INTO client_groups(id,name) VALUES(73,'history')`,
		`INSERT INTO client_group_members(group_id,client_ip) VALUES(73,'192.0.2.4')`,
		`INSERT INTO query_logs(timestamp,client_ip,query_name,query_type) VALUES('historical-unparseable-time','192.0.2.4','history.example.','A')`,
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	clients, err := getAllClients()
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 || clients[0].LastSeen != "historical-unparseable-time" {
		t.Errorf("client history fabricated timestamp: %#v", clients)
	}
	members, err := getGroupMembers(73)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0].LastSeen != "historical-unparseable-time" {
		t.Errorf("group history fabricated timestamp: %#v", members)
	}
}
