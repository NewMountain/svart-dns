package svart

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestBootstrapImportRetainedServerAndRollback(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "retained", true: "rollback"}[fail], func(t *testing.T) {
			defer setupTestDB(t)()
			invalidateBootstrapCache()
			defer invalidateBootstrapCache()
			useBootstrap(t, "192.0.2.53:53", "192.0.2.54:53")
			for _, stmt := range []string{
				`UPDATE bootstrap_servers SET updated_at='2026-01-01T00:00:00Z',node_id='before-import'`,
				`INSERT INTO sync_tombstones(table_name,natural_key,deleted_at,node_id) VALUES('bootstrap_servers','192.0.2.55:53','2026-01-01T00:00:00Z','old-peer')`,
			} {
				if _, err := db.Exec(stmt); err != nil {
					t.Fatal(err)
				}
			}
			// Capture complete stored rows, including row IDs and all conflict metadata.
			snapshot := func() string {
				var servers, tombstones string
				for query, target := range map[string]*string{
					`SELECT json_group_array(json_object('id',id,'server',server,'updated_at',updated_at,'node_id',node_id)) FROM (SELECT * FROM bootstrap_servers ORDER BY id)`:                                &servers,
					`SELECT json_group_array(json_object('table',table_name,'key',natural_key,'deleted_at',deleted_at,'node_id',node_id)) FROM (SELECT * FROM sync_tombstones ORDER BY table_name,natural_key)`: &tombstones,
				} {
					if err := db.QueryRow(query).Scan(target); err != nil {
						t.Fatal(err)
					}
				}
				return servers + "\n" + tombstones
			}
			before := snapshot()
			bootstrapHosts.Store("import-cache.example", bootstrapEntry{ip: "192.0.2.10", expiresAt: clock.Now().Add(time.Hour)})
			if fail {
				if _, err := db.Exec(`CREATE TRIGGER fail_import_after_bootstrap BEFORE INSERT ON client_aliases BEGIN SELECT RAISE(ABORT,'injected later failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			w := httptest.NewRecorder()
			handleAPIConfigImport(w, httptest.NewRequest("POST", "/api/config/import", bytes.NewBufferString(`{"bootstrap_servers":["192.0.2.54:53","192.0.2.55:53"],"clients":[{"ip":"192.0.2.10","alias":"Printer"}]}`)))
			if fail {
				if w.Code != 500 {
					t.Fatalf("status=%d want500: %s", w.Code, w.Body.String())
				}
				if after := snapshot(); after != before {
					t.Fatalf("rollback changed rows/tombstones:\nbefore=%s\nafter=%s", before, after)
				}
				ip, err := bootstrapLookup(context.Background(), "import-cache.example")
				if err != nil || ip != "192.0.2.10" {
					t.Fatalf("rollback lost cached answer: %q %v", ip, err)
				}
				return
			}
			if w.Code != 200 {
				t.Fatalf("status=%d want200: %s", w.Code, w.Body.String())
			}
			if _, ok := bootstrapHosts.Load("import-cache.example"); ok {
				t.Fatal("successful import left cached answer")
			}
			var removed string
			if err := db.QueryRow(`SELECT group_concat(natural_key,',') FROM sync_tombstones WHERE table_name='bootstrap_servers'`).Scan(&removed); err != nil {
				t.Fatal(err)
			}
			if removed != "192.0.2.53:53" {
				t.Fatalf("removed keys=%q want192.0.2.53:53", removed)
			}
			response, err := buildSyncResponse(time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			if len(response.Changes.BootstrapSvrs) != 2 {
				t.Fatalf("live bootstrap rows=%d want2", len(response.Changes.BootstrapSvrs))
			}
			if err := mergeSyncResponse(response); err != nil {
				t.Fatal(err)
			}
			exported, err := buildConfigExport()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(exported.Bootstrap, []string{"192.0.2.54:53", "192.0.2.55:53"}) {
				got, fixtureErr3614 := json.Marshal(exported.Bootstrap)
				if fixtureErr3614 != nil {
					t.Errorf("fixture operation failed: %v", fixtureErr3614)
				}
				t.Fatalf("retained/re-added servers=%s", got)
			}
		})
	}
}

func TestBootstrapImportRemovalConvergesAcrossPeers(t *testing.T) {
	defer setupTestDB(t)()
	useBootstrap(t, "192.0.2.53:53", "192.0.2.54:53")
	if _, err := db.Exec("UPDATE bootstrap_servers SET updated_at=?,node_id='peer-one'", time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	oldPeer, err := buildSyncResponse(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(oldPeer.Changes.BootstrapSvrs) != 2 {
		t.Fatalf("old peer servers=%d, want 2", len(oldPeer.Changes.BootstrapSvrs))
	}
	var replacement *SyncResponse
	// The standard helper owns one process-global log writer. Stop it before
	// temporarily swapping databases. These peers exercise configuration only.
	closeLogWriter()
	func() {
		defer setupTestDB(t)()
		useBootstrap(t)
		if err := mergeSyncResponse(oldPeer); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		handleAPIConfigImport(w, httptest.NewRequest("POST", "/api/config/import", bytes.NewBufferString(`{"bootstrap_servers":["192.0.2.54:53"]}`)))
		if w.Code != 200 {
			t.Fatalf("import=%d %s", w.Code, w.Body.String())
		}
		if err := mergeSyncResponse(oldPeer); err != nil {
			t.Fatal(err)
		}
		export, err := buildConfigExport()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(export.Bootstrap, []string{"192.0.2.54:53"}) {
			t.Fatalf("peer two resurrected server: %v", export.Bootstrap)
		}
		replacement, err = buildSyncResponse(time.Time{})
		if err != nil {
			t.Fatal(err)
		}
	}()
	if err := mergeSyncResponse(replacement); err != nil {
		t.Fatal(err)
	}
	export, err := buildConfigExport()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(export.Bootstrap, []string{"192.0.2.54:53"}) {
		t.Fatalf("peer one failed to converge: %v", export.Bootstrap)
	}
}

func TestBootstrapSyncHonorsStoredDeletionAndNewerReaddition(t *testing.T) {
	defer setupTestDB(t)()
	useBootstrap(t)
	deletedAt := time.Now().UTC().Add(-time.Minute)
	if _, err := db.Exec(`INSERT INTO sync_tombstones(table_name,natural_key,deleted_at,node_id) VALUES('bootstrap_servers','192.0.2.53:53',?,'peer-one')`, deletedAt.Format(time.RFC3339Nano)); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct {
		updated time.Time
		want    int
	}{
		{deletedAt.Add(-time.Second), 0}, {deletedAt, 0}, {deletedAt.Add(time.Second), 1},
	} {
		response := &SyncResponse{Changes: SyncChanges{BootstrapSvrs: []SyncBootstrap{{Server: "192.0.2.53:53", UpdatedAt: fixture.updated.Format(time.RFC3339Nano), NodeID: "peer-two"}}}}
		if err := mergeSyncResponse(response); err != nil {
			t.Fatal(err)
		}
		var count int
		if err := db.QueryRow("SELECT COUNT(*) FROM bootstrap_servers WHERE server='192.0.2.53:53'").Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != fixture.want {
			t.Fatalf("timestamp=%s live rows=%d want%d", fixture.updated, count, fixture.want)
		}
	}
}
