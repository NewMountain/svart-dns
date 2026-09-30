package svart

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/yeti/svart-dns/internal/policycore"
)

// mostSpecificRange is the most specific range the policy evaluates for
// clientIP (the first range entity), or "" when no range contains it.
func mostSpecificRange(clientIP string) string {
	r := evaluatePolicyFull(clientIP, "example.com", 1)
	if len(r.RangeEvaluation.Entities) == 0 {
		return ""
	}
	return r.RangeEvaluation.Entities[0].Name
}

func TestMatchRangeBasic(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "Home LAN", "10.42.1.0/24"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "IoT VLAN", "10.42.3.0/24"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	mustReloadPolicy(t)

	tests := []struct {
		name      string
		clientIP  string
		wantRange string
		wantNil   bool
	}{
		{"matches Home LAN", "10.42.1.42", "Home LAN", false},
		{"matches IoT VLAN", "10.42.3.50", "IoT VLAN", false},
		{"no match", "192.168.1.1", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name := mostSpecificRange(tt.clientIP)
			if tt.wantNil {
				if name != "" {
					t.Errorf("expected no range, got range %q", name)
				}
				return
			}
			if name != tt.wantRange {
				t.Errorf("expected range %q, got %q", tt.wantRange, name)
			}
		})
	}
}

func TestMatchRangeOverlappingCIDRs(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// /16 is broader, /24 is more specific
	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "Broad Network", "10.42.0.0/16"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "Specific Subnet", "10.42.1.0/24"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	mustReloadPolicy(t)

	// IP in the /24 should match the more specific range
	if name := mostSpecificRange("10.42.1.42"); name != "Specific Subnet" {
		t.Errorf("expected 'Specific Subnet' (most specific), got %q", name)
	}

	// IP outside the /24 but inside /16
	if name := mostSpecificRange("10.42.2.100"); name != "Broad Network" {
		t.Errorf("expected 'Broad Network', got %q", name)
	}
}

func TestMatchRangeInvalidIP(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()
	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "Everyone", "0.0.0.0/0"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	mustReloadPolicy(t)

	if name := mostSpecificRange("not-an-ip"); name != "" {
		t.Errorf("expected no range for an invalid IP, got %q", name)
	}
}

func TestLoadRangesWithBlockAndAllowData(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create range
	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "Test Range", "10.42.0.0/16"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// Create blocklist and assign to range
	blResult, fixtureErr3131 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)",
		"https://example.com/list.txt", "Range Blocklist", true)
	if fixtureErr3131 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr3131)
	}
	blID, fixtureErr3279 := blResult.LastInsertId()
	if fixtureErr3279 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr3279)
	}
	if _, err := db.Exec("INSERT INTO blocked_domains (blocklist_id, domain) VALUES (?, ?)", blID, "range-blocked.com"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO range_blocklists (range_id, blocklist_id) VALUES (1, ?)", blID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	// Create allowlist and assign to range
	alResult, fixtureErr3703 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)",
		"", "Range Allowlist", true)
	if fixtureErr3703 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr3703)
	}
	alID, fixtureErr3823 := alResult.LastInsertId()
	if fixtureErr3823 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr3823)
	}
	if _, err := db.Exec("INSERT INTO allowed_domains (allowlist_id, domain) VALUES (?, ?)", alID, "range-allowed.com"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO range_allowlists (range_id, allowlist_id) VALUES (1, ?)", alID); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	mustReloadPolicy(t)

	blocked := evaluatePolicyFull("10.42.7.7", "range-blocked.com", 1).RangeEvaluation
	wantBlock := policycore.EntityResult{Tier: "range", Name: "Test Range", Result: "block",
		PublishedList: &policycore.PublishedHit{Action: "block", Rule: "range-blocked.com", ListID: int(blID), ListName: "Range Blocklist"}}
	if len(blocked.Entities) != 1 || !reflect.DeepEqual(blocked.Entities[0], wantBlock) {
		t.Errorf("range-blocked.com: range entities %+v, want [%+v]", blocked.Entities, wantBlock)
	}

	allowed := evaluatePolicyFull("10.42.7.7", "range-allowed.com", 1).RangeEvaluation
	wantAllow := policycore.EntityResult{Tier: "range", Name: "Test Range", Result: "allow",
		PublishedList: &policycore.PublishedHit{Action: "allow", Rule: "range-allowed.com", ListID: int(alID), ListName: "Range Allowlist"}}
	if len(allowed.Entities) != 1 || !reflect.DeepEqual(allowed.Entities[0], wantAllow) {
		t.Errorf("range-allowed.com: range entities %+v, want [%+v]", allowed.Entities, wantAllow)
	}
}

func TestLoadRangesInvalidCIDR(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "Valid", "10.42.1.0/24"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "Invalid", "not-a-cidr"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "Also Valid", "10.42.2.0/24"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}

	mustReloadPolicy(t)

	ranges := policyState.Load().Ranges
	if len(ranges) != 2 {
		t.Errorf("expected 2 valid ranges (invalid CIDR skipped), got %d", len(ranges))
	}
}

func TestHandleAPIRangesCRUD(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// GET — empty
	req := httptest.NewRequest("GET", "/api/ranges", nil)
	w := httptest.NewRecorder()
	handleAPIRangesRouter(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET, got %d", w.Code)
	}
	data, _ := decodeAPIResponse(t, w.Body)
	ranges := requireFixtureType[[]interface{}](t, data)
	if len(ranges) != 0 {
		t.Errorf("expected 0 ranges, got %d", len(ranges))
	}

	// POST — create
	body := bytes.NewBufferString(`{"name":"Home LAN","cidr":"10.42.1.0/24"}`)
	req = httptest.NewRequest("POST", "/api/ranges", body)
	w = httptest.NewRecorder()
	handleAPIRangesRouter(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 on POST, got %d", w.Code)
	}

	// GET — should have 1
	req = httptest.NewRequest("GET", "/api/ranges", nil)
	w = httptest.NewRecorder()
	handleAPIRangesRouter(w, req)

	data, _ = decodeAPIResponse(t, w.Body)
	ranges = requireFixtureType[[]interface{}](t, data)
	if len(ranges) != 1 {
		t.Errorf("expected 1 range, got %d", len(ranges))
	}

	// PUT — update name
	body = bytes.NewBufferString(`{"name":"Updated LAN"}`)
	req = httptest.NewRequest("PUT", "/api/ranges/1", body)
	w = httptest.NewRecorder()
	handleAPIRangeAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on PUT, got %d", w.Code)
	}

	var name string
	if err := db.QueryRow("SELECT name FROM ip_ranges WHERE id = 1").Scan(&name); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if name != "Updated LAN" {
		t.Errorf("expected name 'Updated LAN', got %q", name)
	}

	// DELETE
	req = httptest.NewRequest("DELETE", "/api/ranges/1", nil)
	w = httptest.NewRecorder()
	handleAPIRangeAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on DELETE, got %d", w.Code)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM ip_ranges").Scan(&count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if count != 0 {
		t.Error("expected range to be deleted")
	}
}

func TestHandleAPICreateRangeInvalidCIDR(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	body := bytes.NewBufferString(`{"name":"Bad Range","cidr":"not-a-cidr"}`)
	req := httptest.NewRequest("POST", "/api/ranges", body)
	w := httptest.NewRecorder()
	handleAPIRangesRouter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid CIDR, got %d", w.Code)
	}
}

func TestHandleAPICreateRangeNoName(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	body := bytes.NewBufferString(`{"cidr":"10.42.0.0/16"}`)
	req := httptest.NewRequest("POST", "/api/ranges", body)
	w := httptest.NewRecorder()
	handleAPIRangesRouter(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing name, got %d", w.Code)
	}
}

func TestHandleAPIRangeBlocklistAssignment(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create range and blocklist
	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "Test Range", "10.42.1.0/24"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	blResult, fixtureErr9061 := db.Exec("INSERT INTO blocklists (url, alias, enabled) VALUES (?, ?, ?)",
		"https://example.com/list.txt", "Test BL", true)
	if fixtureErr9061 != nil {
		t.Fatalf("fixture operation failed: %v",

			fixtureErr9061)
	}
	blID, fixtureErr9201 := blResult.LastInsertId()
	if fixtureErr9201 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr9201)
	}

	// POST: assign
	req := httptest.NewRequest("POST", "/api/ranges/1/blocklists/"+itoa(int(blID)), nil)
	w := httptest.NewRecorder()
	handleAPIRangeAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on POST, got %d", w.Code)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM range_blocklists WHERE range_id = 1 AND blocklist_id = ?", blID).Scan(&count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if count != 1 {
		t.Error("expected blocklist assigned to range")
	}

	// DELETE: unassign
	req = httptest.NewRequest("DELETE", "/api/ranges/1/blocklists/"+itoa(int(blID)), nil)
	w = httptest.NewRecorder()
	handleAPIRangeAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on DELETE, got %d", w.Code)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM range_blocklists WHERE range_id = 1 AND blocklist_id = ?", blID).Scan(&count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if count != 0 {
		t.Error("expected blocklist unassigned from range")
	}
}

func TestHandleAPIRangeAllowlistAssignment(t *testing.T) {
	cleanup := setupTestDB(t)
	defer cleanup()

	// Create range and allowlist
	if _, err := db.Exec("INSERT INTO ip_ranges (name, cidr) VALUES (?, ?)", "Test Range", "10.42.1.0/24"); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	alResult, fixtureErr10583 := db.Exec("INSERT INTO allowlists (url, alias, enabled) VALUES (?, ?, ?)",
		"", "Test AL", true)
	if fixtureErr10583 != nil {
		t.Fatalf("fixture operation failed: %v",

			fixtureErr10583)
	}
	alID, fixtureErr10695 := alResult.LastInsertId()
	if fixtureErr10695 != nil {
		t.Errorf("fixture operation failed: %v", fixtureErr10695)
	}

	// POST: assign
	req := httptest.NewRequest("POST", "/api/ranges/1/allowlists/"+itoa(int(alID)), nil)
	w := httptest.NewRecorder()
	handleAPIRangeAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on POST, got %d", w.Code)
	}

	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM range_allowlists WHERE range_id = 1 AND allowlist_id = ?", alID).Scan(&count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if count != 1 {
		t.Error("expected allowlist assigned to range")
	}

	// DELETE: unassign
	req = httptest.NewRequest("DELETE", "/api/ranges/1/allowlists/"+itoa(int(alID)), nil)
	w = httptest.NewRecorder()
	handleAPIRangeAction(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on DELETE, got %d", w.Code)
	}

	if err := db.QueryRow("SELECT COUNT(*) FROM range_allowlists WHERE range_id = 1 AND allowlist_id = ?", alID).Scan(&count); err != nil {
		t.Errorf("fixture operation failed: %v", err)
	}
	if count != 0 {
		t.Error("expected allowlist unassigned from range")
	}
}
