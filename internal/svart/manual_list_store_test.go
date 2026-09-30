package svart

import "testing"

type manualStoreCase struct {
	name, lists, assignment, alias string
	create                         func() (int64, error)
}

func manualStoreCases() []manualStoreCase {
	return []manualStoreCase{
		{"client block", "blocklists", "client_blocklists", "Manual Block (10.2.3.4)", func() (int64, error) { return getOrCreateManualBlocklist("10.2.3.4") }},
		{"client allow", "allowlists", "client_allowlists", "Manual (10.2.3.4)", func() (int64, error) { return getOrCreateManualAllowlist("10.2.3.4") }},
		{"group block", "blocklists", "group_blocklists", "Manual Block (Family)", func() (int64, error) { return getOrCreateGroupManualBlocklist(1) }},
		{"group allow", "allowlists", "group_allowlists", "Manual (Family)", func() (int64, error) { return getOrCreateGroupManualAllowlist(1) }},
		{"policy block", "blocklists", "policy_blocklists", "Manual Block (Policy: Privacy)", func() (int64, error) { return getOrCreatePolicyManualBlocklist(1) }},
		{"policy allow", "allowlists", "policy_allowlists", "Manual Allow (Policy: Privacy)", func() (int64, error) { return getOrCreatePolicyManualAllowlist(1) }},
	}
}

func seedManualOwners(t *testing.T) {
	t.Helper()
	for _, query := range []string{"INSERT INTO client_groups(id,name) VALUES(1,'Family')", "INSERT INTO policies(id,name) VALUES(1,'Privacy')"} {
		if _, err := db.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
}

func TestManualListStorePreservesAliasesAndDisabledReuse(t *testing.T) {
	for _, c := range manualStoreCases() {
		t.Run(c.name, func(t *testing.T) {
			defer setupTestDB(t)()
			seedManualOwners(t)
			id, err := c.create()
			if err != nil {
				t.Fatal(err)
			}
			var alias, url string
			var enabled bool
			if err := db.QueryRow("SELECT alias,url,enabled FROM "+c.lists+" WHERE id=?", id).Scan(&alias, &url, &enabled); err != nil {
				t.Fatal(err)
			}
			if alias != c.alias || url != "" || !enabled {
				t.Fatalf("created alias=%q url=%q enabled=%t", alias, url, enabled)
			}
			if _, err := db.Exec("UPDATE "+c.lists+" SET enabled=0,alias='User name' WHERE id=?", id); err != nil {
				t.Fatal(err)
			}
			again, err := c.create()
			if err != nil || again != id {
				t.Fatalf("existing list=%d,%v want%d", again, err, id)
			}
			if err := db.QueryRow("SELECT alias,enabled FROM "+c.lists+" WHERE id=?", id).Scan(&alias, &enabled); err != nil {
				t.Fatal(err)
			}
			if alias != "User name" || enabled {
				t.Fatalf("reuse changed saved metadata: %q,%t", alias, enabled)
			}
		})
	}
}

func TestManualListStoreFailureDoesNotLeaveOrphans(t *testing.T) {
	for _, c := range manualStoreCases() {
		for _, fault := range []string{"ignore list", "ignore assignment", "abort assignment", "commit"} {
			t.Run(c.name+"/"+fault, func(t *testing.T) {
				defer setupTestDB(t)()
				seedManualOwners(t)
				query := "CREATE TRIGGER fault BEFORE INSERT ON " + c.assignment + " BEGIN SELECT RAISE(IGNORE); END"
				switch fault {
				case "ignore list":
					query = "CREATE TRIGGER fault BEFORE INSERT ON " + c.lists + " BEGIN SELECT RAISE(IGNORE); END"
				case "abort assignment":
					query = "CREATE TRIGGER fault BEFORE INSERT ON " + c.assignment + " BEGIN SELECT RAISE(ABORT,'assignment unavailable'); END"
				case "commit":
					for _, schema := range []string{"CREATE TABLE fault_parent(id INTEGER PRIMARY KEY)", "CREATE TABLE fault_child(id INTEGER REFERENCES fault_parent(id) DEFERRABLE INITIALLY DEFERRED)"} {
						if _, err := db.Exec(schema); err != nil {
							t.Fatal(err)
						}
					}
					query = "CREATE TRIGGER fault AFTER INSERT ON " + c.assignment + " BEGIN INSERT INTO fault_child(id) VALUES(999); END"
				}
				if _, err := db.Exec(query); err != nil {
					t.Fatal(err)
				}
				id, err := c.create()
				if err == nil || id != 0 {
					t.Errorf("failed creation returned id=%d error=%v", id, err)
				}
				for _, table := range []string{c.lists, c.assignment} {
					var count int
					if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
						t.Fatal(err)
					}
					if count != 0 {
						t.Errorf("failed creation left %d rows in %s", count, table)
					}
				}
			})
		}
	}
}

func TestManualListStoreConcurrentCreationReusesOneList(t *testing.T) {
	for _, c := range manualStoreCases() {
		t.Run(c.name, func(t *testing.T) {
			defer setupTestDB(t)()
			seedManualOwners(t)
			type result struct {
				id  int64
				err error
			}
			results := make(chan result, 8)
			for n := 0; n < cap(results); n++ {
				go func() { id, err := c.create(); results <- result{id, err} }()
			}
			var first int64
			for n := 0; n < cap(results); n++ {
				got := <-results
				if got.err != nil {
					t.Errorf("creation failed: %v", got.err)
					continue
				}
				if first == 0 {
					first = got.id
				}
				if got.id != first {
					t.Errorf("concurrent creation IDs differ: %d != %d", got.id, first)
				}
			}
			for _, table := range []string{c.lists, c.assignment} {
				var count int
				if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
					t.Fatal(err)
				}
				if count != 1 {
					t.Errorf("concurrent creation left %d rows in %s", count, table)
				}
			}
		})
	}
}
