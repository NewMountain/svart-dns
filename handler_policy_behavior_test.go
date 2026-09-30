package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// This fixture exercises the real handlers over HTTP and their real SQLite
// transactions. Authentication has its own admission tests; no transport or
// database behavior is replaced here.
func policyBehaviorServer(t *testing.T) *httptest.Server {
	t.Helper()
	t.Cleanup(setupTestDB(t))
	mux := http.NewServeMux()
	for path, handler := range map[string]http.HandlerFunc{
		"/api/policies": handleAPIPoliciesRouter, "/api/policies/": handleAPIPolicyAction,
		"/api/groups": handleAPIGroupsRouter, "/api/groups/": handleAPIGroupAction,
		"/api/ranges": handleAPIRangesRouter, "/api/ranges/": handleAPIRangeAction,
		"/api/clients/": handleAPIClient, "/api/rewrites": handleAPIRewritesRouter,
		"/api/rewrites/batch": handleAPIRewritesBatch, "/api/rewrites/": handleAPIRewriteAction,
	} {
		mux.HandleFunc(path, handler)
	}
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func policyBehaviorRequest[T any](t *testing.T, server *httptest.Server, method, path, body string, status int) T {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer checkTestClose(t, response.Body)
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != status {
		t.Fatalf("%s %s: HTTP %d want %d: %s", method, path, response.StatusCode, status, raw)
	}
	var envelope APIResponse[T]
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("%s %s: invalid envelope: %v; %s", method, path, err, raw)
	}
	if status < 400 && envelope.Error != nil {
		t.Fatalf("successful response has error: %s", *envelope.Error)
	}
	if status >= 400 && (envelope.Error == nil || *envelope.Error == "") {
		t.Fatalf("failed response lacks error: %s", raw)
	}
	return envelope.Data
}

func policyBehaviorWrite(t *testing.T, server *httptest.Server, method, path, body string) {
	t.Helper()
	result := policyBehaviorRequest[SuccessResponse](t, server, method, path, body, http.StatusOK)
	if !result.Success {
		t.Fatalf("%s %s did not confirm success", method, path)
	}
}

func policyBehaviorCount(t *testing.T, query string, want int, args ...any) {
	t.Helper()
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("persisted count %d want %d for %s", count, want, query)
	}
}

func TestHandlerPolicyAssignmentLifecycle(t *testing.T) {
	server := policyBehaviorServer(t)
	localSQL(t, `INSERT INTO blocklists(id,url,alias,domain_count) VALUES(101,'https://lists.invalid/block','Remote block',1)`)
	localSQL(t, `INSERT INTO blocked_domains(blocklist_id,domain) VALUES(101,'ads.example.test')`)
	localSQL(t, `INSERT INTO allowlists(id,url,alias,domain_count) VALUES(102,'https://lists.invalid/allow','Remote allow',1)`)
	localSQL(t, `INSERT INTO allowed_domains(allowlist_id,domain) VALUES(102,'allowed.example.test')`)
	policy := policyBehaviorRequest[CreatePolicyResponse](t, server, "POST", "/api/policies", `{"name":"  Family  ","description":"Original"}`, 201)
	group := policyBehaviorRequest[CreateGroupResponse](t, server, "POST", "/api/groups", `{"name":"  Devices  "}`, 201)
	ipRange := policyBehaviorRequest[CreateRangeResponse](t, server, "POST", "/api/ranges", `{"name":"LAN","cidr":"192.0.2.0/24"}`, 201)
	if policy.Name != "Family" || group.Name != "Devices" {
		t.Fatalf("names not normalized: %+v %+v", policy, group)
	}
	policyPath := fmt.Sprintf("/api/policies/%d", policy.ID)
	groupPath := fmt.Sprintf("/api/groups/%d", group.ID)
	rangePath := fmt.Sprintf("/api/ranges/%d", ipRange.ID)
	clientPath := "/api/clients/192.0.2.10"
	// Empty detail arrays remain usable by clients before any assignments exist.
	empty := policyBehaviorRequest[ClientDetailView](t, server, "GET", clientPath, "", 200)
	if empty.Policy != nil || empty.CustomBlocked == nil || empty.CustomAllowed == nil {
		t.Fatalf("invalid empty client detail: %+v", empty)
	}
	policyBehaviorWrite(t, server, "PUT", clientPath+"/alias", `{"alias":"Laptop"}`)
	policyBehaviorWrite(t, server, "POST", clientPath+"/groups/"+strconv.FormatInt(group.ID, 10), "")
	for _, path := range []string{policyPath, groupPath, rangePath, clientPath} {
		policyBehaviorWrite(t, server, "POST", path+"/blocklists/101", "")
		policyBehaviorWrite(t, server, "POST", path+"/allowlists/102", "")
		policyBehaviorWrite(t, server, "POST", path+"/block-domain", `{"domain":"manual-block.example.test"}`)
		policyBehaviorWrite(t, server, "POST", path+"/allow-domain", `{"domain":"manual-allow.example.test"}`)
	}
	for _, path := range []string{groupPath, rangePath, clientPath} {
		policyBehaviorWrite(t, server, "POST", path+"/policy/"+strconv.FormatInt(policy.ID, 10), "")
	}
	localSQL(t, `INSERT INTO query_logs(client_ip,query_name,query_type,latency_microseconds,coalesced_count,client_name,result_reason,result_tier,result_entity,result_list_name,block_list_name,range_entity,group_entity) VALUES('192.0.2.10','ads.example.test','A',125,1,'Historical laptop','list match','group','Devices','Remote block','Original block','Original LAN','Original devices')`)
	detail := policyBehaviorRequest[PolicyDetailView](t, server, "GET", policyPath, "", 200)
	if detail.Name != "Family" || detail.Description != "Original" || len(detail.Blocklists) != 1 || !detail.Blocklists[0].IsAssigned || len(detail.Allowlists) != 1 || !detail.Allowlists[0].IsAssigned {
		t.Fatalf("policy list assignments: %+v", detail)
	}
	if !reflect.DeepEqual(detail.CustomBlocked, []string{"manual-block.example.test"}) || !reflect.DeepEqual(detail.CustomAllowed, []string{"manual-allow.example.test"}) {
		t.Fatalf("policy manual rules: %+v", detail)
	}
	if !reflect.DeepEqual(detail.AssignedTo.Clients, []AssignedClient{{IP: "192.0.2.10", Alias: "Laptop"}}) || len(detail.AssignedTo.Groups) != 1 || int64(detail.AssignedTo.Groups[0].ID) != group.ID || len(detail.AssignedTo.Ranges) != 1 || detail.AssignedTo.Ranges[0].CIDR != "192.0.2.0/24" {
		t.Fatalf("assigned entities: %+v", detail.AssignedTo)
	}
	client := policyBehaviorRequest[ClientDetailView](t, server, "GET", clientPath, "", 200)
	if client.Alias != "Laptop" || client.TotalQueries != 1 || client.AvgLatencyMicroseconds != 125 || client.Policy == nil || int64(client.Policy.ID) != policy.ID || len(client.Groups) != 1 || !client.Groups[0].IsMember {
		t.Fatalf("client detail: %+v", client)
	}
	gd := policyBehaviorRequest[GroupDetailView](t, server, "GET", groupPath, "", 200)
	if gd.Name != "Devices" || len(gd.Members) != 1 || gd.TotalQueries != 1 || gd.Policy == nil || int64(gd.Policy.ID) != policy.ID || len(gd.RecentLogs) != 1 {
		t.Fatalf("group detail: %+v", gd)
	}
	rd := policyBehaviorRequest[RangeDetailView](t, server, "GET", rangePath, "", 200)
	if rd.CIDR != "192.0.2.0/24" || rd.TotalQueries != 1 || rd.Policy == nil || int64(rd.Policy.ID) != policy.ID || len(rd.Allowlists) != 2 || len(rd.RecentLogs) != 1 {
		t.Fatalf("range detail: %+v", rd)
	}
	for _, logs := range [][]RecentLogView{gd.RecentLogs, rd.RecentLogs} {
		entry := logs[0]
		for field, expected := range map[*string]string{entry.ClientAlias: "Historical laptop", entry.ResultReason: "list match", entry.ResultTier: "group", entry.ResultEntity: "Devices", entry.ResultListName: "Remote block", entry.BlockListName: "Original block", entry.RangeName: "Original LAN", entry.GroupName: "Original devices"} {
			if field == nil || *field != expected {
				t.Fatalf("historical log metadata lost: %+v; expected %q", entry, expected)
			}
		}
	}
	policies := policyBehaviorRequest[[]PolicyView](t, server, "GET", "/api/policies", "", 200)
	if len(policies) != 1 || policies[0].UsageCount != 3 || policies[0].BlocklistCount != 2 || policies[0].AllowlistCount != 2 {
		t.Fatalf("policy totals: %+v", policies)
	}
	policyBehaviorWrite(t, server, "PUT", policyPath, `{"name":" Updated policy ","description":"Updated description"}`)
	policyBehaviorWrite(t, server, "PUT", groupPath, `{"name":" Updated group "}`)
	policyBehaviorWrite(t, server, "PUT", rangePath, `{"name":"Updated range","cidr":"198.51.100.0/24"}`)
	policyBehaviorCount(t, `SELECT COUNT(*) FROM policies WHERE id=? AND name='Updated policy' AND description='Updated description'`, 1, policy.ID)
	policyBehaviorCount(t, `SELECT COUNT(*) FROM client_groups WHERE id=? AND name='Updated group'`, 1, group.ID)
	policyBehaviorCount(t, `SELECT COUNT(*) FROM ip_ranges WHERE id=? AND name='Updated range' AND cidr='198.51.100.0/24'`, 1, ipRange.ID)
	for _, path := range []string{policyPath, groupPath, rangePath, clientPath} {
		policyBehaviorWrite(t, server, "DELETE", path+"/blocklists/101", "")
		policyBehaviorWrite(t, server, "DELETE", path+"/allowlists/102", "")
		policyBehaviorWrite(t, server, "DELETE", path+"/block-domain/manual-block.example.test", "")
		policyBehaviorWrite(t, server, "DELETE", path+"/allow-domain/manual-allow.example.test", "")
	}
	for _, path := range []string{groupPath, rangePath, clientPath} {
		policyBehaviorWrite(t, server, "DELETE", path+"/policy", "")
	}
	policyBehaviorWrite(t, server, "DELETE", clientPath+"/groups/"+strconv.FormatInt(group.ID, 10), "")
	policyBehaviorCount(t, `SELECT COUNT(*) FROM client_policies`, 0)
	policyBehaviorCount(t, `SELECT COUNT(*) FROM client_group_members`, 0)
	detached := policyBehaviorRequest[PolicyDetailView](t, server, "GET", policyPath, "", 200)
	if len(detached.AssignedTo.Clients)+len(detached.AssignedTo.Groups)+len(detached.AssignedTo.Ranges)+len(detached.CustomAllowed)+len(detached.CustomBlocked) != 0 {
		t.Fatalf("detached relationships retained: %+v", detached)
	}
	for _, path := range []string{groupPath, rangePath, policyPath} {
		policyBehaviorWrite(t, server, "DELETE", path, "")
		policyBehaviorRequest[json.RawMessage](t, server, "GET", path, "", 404)
	}
	policyBehaviorCount(t, `SELECT COUNT(*) FROM policies`, 0)
	policyBehaviorCount(t, `SELECT COUNT(*) FROM client_groups`, 0)
	policyBehaviorCount(t, `SELECT COUNT(*) FROM ip_ranges`, 0)
}

func TestHandlerBatchAtomicity(t *testing.T) {
	server := policyBehaviorServer(t)
	group := policyBehaviorRequest[CreateGroupResponse](t, server, "POST", "/api/groups", `{"name":"Batch"}`, 201)
	path := fmt.Sprintf("/api/groups/%d", group.ID)
	localSQL(t, `INSERT INTO blocklists(id,url,alias) VALUES(101,'https://lists.invalid/one','One'),(102,'https://lists.invalid/two','Two')`)
	lists := policyBehaviorRequest[AssignGroupBlocklistsResponse](t, server, "POST", path+"/blocklists/batch", `{"blocklist_ids":[101,102]}`, 200)
	members := policyBehaviorRequest[AddGroupMembersResponse](t, server, "POST", path+"/members/batch", `{"client_ips":["192.0.2.1","2001:db8::1"]}`, 200)
	if lists.Assigned != 2 || members.Added != 2 {
		t.Fatalf("batch counts: %+v %+v", lists, members)
	}
	policyBehaviorCount(t, `SELECT COUNT(*) FROM group_blocklists WHERE group_id=?`, 2, group.ID)
	policyBehaviorCount(t, `SELECT COUNT(*) FROM client_group_members WHERE group_id=?`, 2, group.ID)
	policyBehaviorWrite(t, server, "POST", path+"/members/192.0.2.2", "")
	policyBehaviorWrite(t, server, "DELETE", path+"/members/192.0.2.2", "")
	for _, test := range []struct {
		name, path, body, trigger string
		status                    int
	}{
		{"invalid member", path + "/members/batch", `{"client_ips":["192.0.2.3","invalid"]}`, "", 400},
		{"invalid list", path + "/blocklists/batch", `{"blocklist_ids":[101,0]}`, "", 400},
		{"later list foreign key", path + "/blocklists/batch", `{"blocklist_ids":[101,99999]}`, "", 503},
		{"later member insert", path + "/members/batch", `{"client_ips":["192.0.2.3","192.0.2.4"]}`, `CREATE TRIGGER batch_fail BEFORE INSERT ON client_group_members WHEN NEW.client_ip='192.0.2.4' BEGIN SELECT RAISE(ABORT,'injected member failure'); END`, 503},
		{"later rewrite invalid", "/api/rewrites/batch", `{"rewrites":[{"domain":"good.test","ip_addresses":"192.0.2.1","enabled":true},{"domain":"bad.test","ip_addresses":"bad","enabled":true}]}`, "", 400},
		{"later rewrite insert", "/api/rewrites/batch", `{"rewrites":[{"domain":"good.test","ip_addresses":"192.0.2.1","enabled":true},{"domain":"fail.test","ip_addresses":"192.0.2.2","enabled":true}]}`, `CREATE TRIGGER batch_fail BEFORE INSERT ON rewrites WHEN NEW.domain='fail.test' BEGIN SELECT RAISE(ABORT,'injected rewrite failure'); END`, 503},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.trigger != "" {
				localSQL(t, test.trigger)
				defer localSQL(t, "DROP TRIGGER batch_fail")
			}
			before := localStoredState(t)
			policyBehaviorRequest[json.RawMessage](t, server, "POST", test.path, test.body, test.status)
			if !reflect.DeepEqual(before, localStoredState(t)) {
				t.Fatal("rejected batch changed persistent database")
			}
		})
	}
	created := policyBehaviorRequest[[]CreatedRewriteView](t, server, "POST", "/api/rewrites/batch", `{"rewrites":[{"domain":"  GOOD.TEST  ","ip_addresses":"192.0.2.1","enabled":true},{"domain":"second.test","ip_addresses":"2001:db8::1","enabled":false}]}`, 201)
	if len(created) != 2 || created[0].Domain != "good.test" || !created[0].Enabled || created[1].Enabled {
		t.Fatalf("rewrite results: %+v", created)
	}
	policyBehaviorCount(t, `SELECT COUNT(*) FROM rewrites WHERE domain='good.test' AND ip_addresses='192.0.2.1' AND enabled=1`, 1)
	policyBehaviorCount(t, `SELECT COUNT(*) FROM rewrites WHERE domain='second.test' AND ip_addresses='2001:db8::1' AND enabled=0`, 1)
	policyBehaviorWrite(t, server, "DELETE", fmt.Sprintf("/api/rewrites/%d", created[0].ID), "")
	policyBehaviorCount(t, `SELECT COUNT(*) FROM rewrites WHERE domain='good.test'`, 0)
}

func TestHandlerEmptyDetailsAndValidation(t *testing.T) {
	server := policyBehaviorServer(t)
	policy := policyBehaviorRequest[CreatePolicyResponse](t, server, "POST", "/api/policies", `{"name":"Fixture"}`, 201)
	group := policyBehaviorRequest[CreateGroupResponse](t, server, "POST", "/api/groups", `{"name":"Fixture"}`, 201)
	ipRange := policyBehaviorRequest[CreateRangeResponse](t, server, "POST", "/api/ranges", `{"name":"Fixture","cidr":"192.0.2.0/24"}`, 201)
	pp, gp, rp := fmt.Sprintf("/api/policies/%d", policy.ID), fmt.Sprintf("/api/groups/%d", group.ID), fmt.Sprintf("/api/ranges/%d", ipRange.ID)
	cp := "/api/clients/192.0.2.10"
	pd := policyBehaviorRequest[PolicyDetailView](t, server, "GET", pp, "", 200)
	gd := policyBehaviorRequest[GroupDetailView](t, server, "GET", gp, "", 200)
	rd := policyBehaviorRequest[RangeDetailView](t, server, "GET", rp, "", 200)
	cd := policyBehaviorRequest[ClientDetailView](t, server, "GET", cp, "", 200)
	if pd.Blocklists == nil || pd.Allowlists == nil || pd.AssignedTo.Clients == nil || pd.AssignedTo.Groups == nil || pd.AssignedTo.Ranges == nil || gd.Members == nil || gd.Blocklists == nil || gd.RecentLogs == nil || rd.Blocklists == nil || rd.Allowlists == nil || rd.RecentLogs == nil || cd.Groups == nil || cd.Blocklists == nil {
		t.Fatalf("empty detail arrays became null: policy=%+v group=%+v range=%+v client=%+v", pd, gd, rd, cd)
	}
	if gd.TotalQueries != 0 || gd.AvgLatencyMicroseconds != 0 || rd.TotalQueries != 0 || rd.AvgLatencyMicroseconds != 0 || cd.TotalQueries != 0 || cd.FirstSeen != "" || gd.FirstSeen != "" || rd.FirstSeen != "" {
		t.Fatal("empty history invented query statistics")
	}
	cases := []struct {
		method, path, body string
		status             int
	}{
		{"POST", "/api/policies", `{"name":"Fixture"}`, 409},
		{"POST", "/api/policies", `{"name":"   "}`, 400},
		{"PUT", pp, `{"name":" "}`, 400},
		{"POST", "/api/groups", `{"name":" "}`, 400},
		{"PUT", gp, `{"name":" "}`, 400},
		{"POST", "/api/ranges", `{"name":"bad","cidr":"not-cidr"}`, 400},
		{"POST", "/api/ranges", `{"name":"","cidr":"192.0.2.0/24"}`, 400},
		{"PUT", rp, `{"cidr":"bad"}`, 400},
		{"GET", "/api/clients/not-an-ip", "", 400},
		{"GET", "/api/clients/", "", 400},
		{"POST", cp, "", 405},
		{"POST", gp + "/members/not-an-ip", "", 400},
		{"POST", "/api/rewrites", `{"domain":"bad.test","ip_addresses":"bad"}`, 400},
		{"PUT", "/api/rewrites/1", `{}`, 400},
		{"PUT", "/api/rewrites/1", `{"domain":"*.bad.test"}`, 400},
		{"PUT", "/api/rewrites/1", `{"ip_addresses":"bad"}`, 400},
		{"PUT", "/api/rewrites/1", `{"enabled":false}`, 404},
		{"GET", "/api/rewrites/1/extra", "", 400},
		{"DELETE", "/api/policies/99999", "", 404},
		{"POST", cp + "/alias", `{}`, 400},
	}
	for _, path := range []string{pp, gp, rp, cp} {
		cases = append(cases, struct {
			method, path, body string
			status             int
		}{"GET", path + "/unknown", "", 400})
		for _, suffix := range []string{"blocklists", "allowlists"} {
			cases = append(cases, struct {
				method, path, body string
				status             int
			}{"GET", path + "/" + suffix + "/101", "", 405}, struct {
				method, path, body string
				status             int
			}{"POST", path + "/" + suffix + "/invalid", "", 400})
		}
	}
	for _, path := range []string{gp, rp, cp} {
		cases = append(cases, struct {
			method, path, body string
			status             int
		}{"POST", path + "/policy", "", 400}, struct {
			method, path, body string
			status             int
		}{"POST", path + "/policy/invalid", "", 400}, struct {
			method, path, body string
			status             int
		}{"GET", path + "/policy", "", 405})
	}
	for _, path := range []string{"/api/policies", "/api/ranges", pp, gp, rp, cp + "/groups/1", gp + "/members/192.0.2.1", "/api/rewrites/1", "/api/rewrites/batch"} {
		cases = append(cases, struct {
			method, path, body string
			status             int
		}{"PATCH", path, "", 405})
	}
	for _, path := range []string{pp, gp, rp, cp + "/alias"} {
		cases = append(cases, struct {
			method, path, body string
			status             int
		}{"PUT", path, "{", 400})
	}
	before := localStoredState(t)
	for _, test := range cases {
		t.Run(test.method+" "+test.path+" "+test.body, func(t *testing.T) {
			policyBehaviorRequest[json.RawMessage](t, server, test.method, test.path, test.body, test.status)
			if !reflect.DeepEqual(before, localStoredState(t)) {
				t.Fatal("rejected HTTP request modified durable data")
			}
		})
	}
}

func TestHandlerReadFailuresDoNotReturnPartialDetails(t *testing.T) {
	for _, test := range []struct{ path, fault string }{
		{"/api/clients/192.0.2.10", `DROP TABLE client_groups`},
		{"/api/clients/192.0.2.10", `DROP TABLE blocklists`},
		{"/api/clients/192.0.2.10", `DROP TABLE allowed_domains`},
		{"/api/groups/900", `DROP TABLE blocklists`},
		{"/api/groups/900", `DROP TABLE allowed_domains`},
		{"/api/groups/900", `DROP TABLE settings`},
		{"/api/ranges/900", `DROP TABLE blocklists`},
		{"/api/ranges/900", `DROP TABLE allowlists`},
		{"/api/ranges/900", `DROP TABLE settings`},
		{"/api/policies/900", `DROP TABLE blocklists`},
		{"/api/policies/900", `DROP TABLE allowlists`},
		{"/api/policies/900", `DROP TABLE blocked_domains`},
		{"/api/policies/900", `DROP TABLE allowed_domains`},
		{"/api/policies/900", `DROP TABLE client_groups`},
		{"/api/policies/900", `DROP TABLE client_aliases`},
		{"/api/policies", `DROP TABLE policy_blocklists`},
		{"/api/ranges", `DROP TABLE ip_ranges`},
	} {
		t.Run(test.path+" "+test.fault, func(t *testing.T) {
			server := policyBehaviorServer(t)
			localSQL(t, `INSERT INTO policies(id,name,description) VALUES(900,'Policy','Visible description')`)
			localSQL(t, `INSERT INTO client_groups(id,name) VALUES(900,'Group')`)
			localSQL(t, `INSERT INTO ip_ranges(id,name,cidr) VALUES(900,'Range','192.0.2.0/24')`)
			localSQL(t, test.fault)
			data := policyBehaviorRequest[json.RawMessage](t, server, "GET", test.path, "", 503)
			if string(data) != "null" {
				t.Fatalf("database failure returned partial payload: %s", data)
			}
		})
	}
}
