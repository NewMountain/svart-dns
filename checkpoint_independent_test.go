package main

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"
)

func TestIndependentCheckpointEveryVersionAndPage(t *testing.T) {
	t.Cleanup(setupTestDB(t))
	localSQL(t, `INSERT INTO blocklists(id,url,alias) VALUES(777,'https://example.com/review','Review complete history')`)
	type version struct {
		id    int
		rules []string
	}
	versions := []version{}
	for step := 0; step < 12; step++ {
		rules := []string{}
		if step != 5 {
			for n := 0; n < 75; n++ {
				rules = append(rules, fmt.Sprintf("rule-%03d.example", (n+step*13)%131))
			}
		}
		sort.Strings(rules)
		if err := storeListGeneration(blockListStore, 777, rules, len(rules), "https://example.com/review", nil); err != nil {
			t.Fatal(err)
		}
		var id int
		if err := db.QueryRow("SELECT MAX(id) FROM blocklist_history WHERE blocklist_id=777").Scan(&id); err != nil {
			t.Fatal(err)
		}
		localSQL(t, "UPDATE blocklist_history SET refreshed_at='2026-09-28 00:00:00' WHERE id=?", id)
		versions = append(versions, version{id, rules})
	}
	for index, version := range versions {
		all := []string{}
		for offset := 0; offset < len(version.rules)+17; offset += 17 {
			page, err := queryCheckpointPage(context.Background(), version.id, "", 17, offset)
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != len(version.rules) {
				t.Fatalf("version%d total=%d want%d", index, page.Total, len(version.rules))
			}
			all = append(all, page.Domains...)
		}
		if !reflect.DeepEqual(all, version.rules) {
			t.Fatalf("version%d complete pages=%v want%v", index, all, version.rules)
		}
	}
	t.Log("all 12 equal-timestamp versions reconstructed exactly across every page, including empty generation and later re-additions")
}
