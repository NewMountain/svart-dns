package svart

import (
	"reflect"

	"github.com/yeti/svart-dns/internal/apigen"
	"github.com/yeti/svart-dns/internal/policycore"
)

func wire[T any]() reflect.Type { return reflect.TypeFor[T]() }

// Explicit operation metadata connects multiplexed handlers to their concrete
// wire types. The generator rejects unsupported types; coverage tests check the
// registered routes and every handler's success payload against this inventory.
func apiOperations() []apigen.Operation {
	var ops []apigen.Operation
	add := func(method, path, handler string, request reflect.Type, status int, response ...reflect.Type) {
		ops = append(ops, apigen.Operation{Method: method, Path: path, Handler: handler, Request: request, Status: status, Response: response})
	}
	get := func(path, handler string, response ...reflect.Type) { add("GET", path, handler, nil, 200, response...) }
	post := func(path, handler string, request, response reflect.Type) {
		add("POST", path, handler, request, 200, response)
	}
	mutation := func(method, path, handler string, request reflect.Type) {
		add(method, path, handler, request, 200, wire[SuccessResponse]())
	}
	get("/health", "handleHealth", wire[HealthResponse]())
	get("/api/setup", "handleAPISetup", wire[setupStatus]())
	add("POST", "/api/setup", "handleAPISetup", wire[setupRequest](), 201, wire[AuthenticatedIdentity]())
	post("/api/auth/login", "handleAPIAuthLogin", wire[AuthLoginRequest](), wire[AuthenticatedIdentity]())
	get("/api/auth/check", "handleAPIAuthCheck", wire[AuthenticatedIdentity](), wire[UnauthenticatedIdentity]())
	mutation("POST", "/api/auth/logout", "handleAPIAuthLogout", nil)
	mutation("POST", "/api/auth/sessions/revoke", "handleAPIRevokeSessions", nil)
	get("/api/stats", "handleAPIStats", wire[DNSStatsResponse]())
	get("/api/stats/dashboard", "handleAPIStatsDashboard", wire[DashboardSnapshot]())
	get("/api/stats/timeseries", "handleAPIStatsTimeseries", wire[DashboardSummary](), wire[[]DashboardTimeseriesPoint]())
	get("/api/stats/top-clients", "handleAPIStatsTopClients", wire[[]DashboardTopClient]())
	get("/api/stats/block-sources", "handleAPIStatsBlockSources", wire[[]DashboardBlockSource]())
	get("/api/stats/upstream-usage", "handleAPIStatsUpstreamUsage", wire[[]DashboardUpstreamUsage]())
	get("/api/stats/top-domains", "handleAPIStatsTopDomains", wire[DashboardTopDomains]())
	get("/api/stats/latency", "handleAPIStatsLatency", wire[[]DashboardLatencyPoint]())
	get("/api/stats/servfails", "handleAPIStatsServfails", wire[[]DashboardServfailClient]())
	get("/api/stats/system", "handleAPIStatsSystem", wire[[]SystemSample]())
	get("/api/upstreams", "handleAPIUpstreams", wire[[]Upstream]())
	add("POST", "/api/upstreams", "handleAPIUpstreams", wire[CreateUpstreamRequest](), 201, wire[Upstream]())
	mutation("PUT", "/api/upstreams/{id}", "handleAPIUpstreamAction", wire[UpdateUpstreamRequest]())
	mutation("DELETE", "/api/upstreams/{id}", "handleAPIUpstreamAction", nil)
	mutation("POST", "/api/upstreams/{id}/toggle", "handleAPIUpstreamAction", nil)
	get("/api/blocklists", "handleAPIGetBlocklists", wire[[]BlocklistView]())
	add("POST", "/api/blocklists", "handleAPIBlocklists", wire[CreateBlocklistRequest](), 201, wire[CreateBlocklistResponse]())
	get("/api/blocklists/{id}/domains", "handleAPIBlocklistAction", wire[BlocklistDomainsPage]())
	get("/api/blocklists/{id}/compatibility", "handleAPIBlocklistAction", wire[ListCompatibilityPage]())
	mutation("PUT", "/api/blocklists/{id}", "handleAPIBlocklistAction", wire[UpdateBlocklistRequest]())
	mutation("DELETE", "/api/blocklists/{id}", "handleAPIBlocklistAction", nil)
	mutation("POST", "/api/blocklists/{id}/toggle", "handleAPIBlocklistAction", nil)
	mutation("POST", "/api/blocklists/{id}/refresh", "handleAPIBlocklistAction", nil)
	get("/api/allowlists", "handleAPIGetAllowlists", wire[[]AllowlistView]())
	add("POST", "/api/allowlists", "handleAPICreateAllowlist", wire[CreateAllowlistRequest](), 201, wire[CreateAllowlistResponse]())
	get("/api/allowlists/{id}/domains", "handleAPIAllowlistAction", wire[AllowlistDomainsPage]())
	get("/api/allowlists/{id}/compatibility", "handleAPIAllowlistAction", wire[ListCompatibilityPage]())
	mutation("PUT", "/api/allowlists/{id}", "handleAPIAllowlistAction", wire[UpdateAllowlistRequest]())
	mutation("DELETE", "/api/allowlists/{id}", "handleAPIAllowlistAction", nil)
	mutation("POST", "/api/allowlists/{id}/toggle", "handleAPIAllowlistAction", nil)
	mutation("POST", "/api/allowlists/{id}/refresh", "handleAPIAllowlistAction", nil)
	get("/api/blocklists/history", "handleAPIBlocklistHistory", wire[[]BlocklistHistoryView]())
	get("/api/blocklists/history/{historyId}/domains", "handleAPIBlocklistCheckpointDomains", wire[CheckpointPage]())
	get("/api/blocklists/unique-domains", "handleAPIBlocklistsUniqueDomains", wire[BlocklistUniqueStats]())
	get("/api/settings", "handleAPIGetSettings", wire[map[string]string]())
	add("PUT", "/api/settings/{key}", "handleAPISetting", wire[UpdateSettingRequest](), 200, wire[SensitiveSettingUpdateResponse](), wire[SettingValueResponse]())
	get("/api/bootstrap", "handleAPIGetBootstrap", wire[[]BootstrapServerView]())
	mutation("POST", "/api/bootstrap", "handleAPIBootstrap", wire[AddBootstrapServerRequest]())
	mutation("PUT", "/api/bootstrap", "handleAPIBootstrap", wire[ReplaceBootstrapServersRequest]())
	mutation("POST", "/api/cache/clear", "handleAPICacheClear", nil)
	get("/api/clients", "handleAPIGetClients", wire[[]Client]())
	get("/api/clients/{ip}", "handleAPIClient", wire[ClientDetailView]())
	mutation("PUT", "/api/clients/{ip}/alias", "handleAPIClient", wire[UpdateClientAliasRequest]())
	get("/api/groups", "handleAPIGetGroups", wire[[]Group]())
	add("POST", "/api/groups", "handleAPIGroups", wire[CreateGroupRequest](), 201, wire[CreateGroupResponse]())
	get("/api/groups/{id}", "handleAPIGroupAction", wire[GroupDetailView]())
	mutation("PUT", "/api/groups/{id}", "handleAPIGroupAction", wire[UpdateGroupRequest]())
	mutation("DELETE", "/api/groups/{id}", "handleAPIGroupAction", nil)
	post("/api/groups/{id}/blocklists/batch", "handleAPIGroupBlocklistsBatch", wire[AssignGroupBlocklistsRequest](), wire[AssignGroupBlocklistsResponse]())
	post("/api/groups/{id}/members/batch", "handleAPIGroupMembersBatch", wire[AddGroupMembersRequest](), wire[AddGroupMembersResponse]())
	get("/api/ranges", "handleAPIGetRanges", wire[[]RangeView]())
	add("POST", "/api/ranges", "handleAPICreateRange", wire[CreateRangeRequest](), 201, wire[CreateRangeResponse]())
	get("/api/ranges/{id}", "handleAPIRangeAction", wire[RangeDetailView]())
	mutation("PUT", "/api/ranges/{id}", "handleAPIRangeAction", wire[UpdateRangeRequest]())
	mutation("DELETE", "/api/ranges/{id}", "handleAPIRangeAction", nil)
	get("/api/policies", "handleAPIGetPolicies", wire[[]PolicyView]())
	add("POST", "/api/policies", "handleAPICreatePolicy", wire[CreatePolicyRequest](), 201, wire[CreatePolicyResponse]())
	get("/api/policies/{id}", "handleAPIGetPolicyDetail", wire[PolicyDetailView]())
	mutation("PUT", "/api/policies/{id}", "handleAPIUpdatePolicy", wire[UpdatePolicyRequest]())
	mutation("DELETE", "/api/policies/{id}", "handleAPIDeletePolicy", nil)
	for _, method := range []string{"POST", "DELETE"} {
		for _, relation := range []string{"blocklists", "allowlists"} {
			mutation(method, "/api/clients/{ip}/"+relation+"/{listId}", "handleAPIClient", nil)
			mutation(method, "/api/groups/{id}/"+relation+"/{listId}", "handleAPIGroupAction", nil)
			mutation(method, "/api/ranges/{id}/"+relation+"/{listId}", "handleAPIRangeAction", nil)
		}
		mutation(method, "/api/clients/{ip}/groups/{groupId}", "handleAPIClient", nil)
		mutation(method, "/api/groups/{id}/members/{ip}", "handleAPIGroupAction", nil)
	}
	for _, entity := range []struct{ path, handler string }{{"clients/{ip}", "handleAPIClient"}, {"groups/{id}", "handleAPIGroupAction"}, {"ranges/{id}", "handleAPIRangeAction"}} {
		mutation("POST", "/api/"+entity.path+"/policy/{policyId}", entity.handler, nil)
		mutation("DELETE", "/api/"+entity.path+"/policy", entity.handler, nil)
	}
	for _, relation := range []struct{ path, add, remove string }{{"blocklists", "handleAPIPolicyAddBlocklist", "handleAPIPolicyRemoveBlocklist"}, {"allowlists", "handleAPIPolicyAddAllowlist", "handleAPIPolicyRemoveAllowlist"}} {
		mutation("POST", "/api/policies/{id}/"+relation.path+"/{listId}", relation.add, nil)
		mutation("DELETE", "/api/policies/{id}/"+relation.path+"/{listId}", relation.remove, nil)
	}
	for _, rule := range []struct {
		path, handler string
		request       reflect.Type
	}{
		{"clients/{ip}/block-domain", "handleAPIClientBlockDomain", wire[DomainRuleRequest]()},
		{"clients/{ip}/allow-domain", "handleAPIClientAllowDomain", wire[DomainRuleRequest]()},
		{"groups/{id}/block-domain", "handleAPIGroupBlockDomain", wire[DomainRuleRequest]()},
		{"groups/{id}/allow-domain", "handleAPIGroupAllowDomain", wire[DomainRuleRequest]()},
		{"policies/{id}/block-domain", "handleAPIPolicyBlockDomain", wire[DomainRuleRequest]()},
		{"policies/{id}/allow-domain", "handleAPIPolicyAllowDomain", wire[DomainRuleRequest]()},
	} {
		mutation("POST", "/api/"+rule.path, rule.handler, rule.request)
		mutation("DELETE", "/api/"+rule.path+"/{domain}", rule.handler, nil)
	}
	get("/api/rewrites", "handleAPIGetRewrites", wire[[]RewriteView]())
	add("POST", "/api/rewrites", "handleAPIRewrites", wire[CreateRewriteRequest](), 201, wire[CreateRewriteResponse]())
	add("PUT", "/api/rewrites/{id}", "handleAPIRewriteAction", wire[UpdateRewriteRequest](), 200, wire[RewriteView]())
	mutation("DELETE", "/api/rewrites/{id}", "handleAPIRewriteAction", nil)
	add("POST", "/api/rewrites/batch", "handleAPIRewritesBatch", wire[CreateRewritesBatchRequest](), 201, wire[[]CreatedRewriteView]())
	get("/api/rewrites/stats", "handleAPIRewritesStats", wire[[]RewriteStatsView]())
	get("/api/query-logs", "handleAPIQueryLogs", wire[QueryLogPage]())
	get("/api/query-logs/{id}", "handleAPIQueryLogDetail", wire[QueryLogDetailView]())
	get("/api/policy/evaluate", "handleAPIPolicyEvaluate", wire[policycore.PolicyResult]())
	post("/api/analysis/compare", "handleAPIAnalysisCompare", wire[compareRequest](), wire[compareResponse]())
	post("/api/analysis/matrix", "handleAPIAnalysisMatrix", wire[matrixRequest](), wire[matrixResponse]())
	post("/api/analysis/simulate", "handleAPIAnalysisSimulate", wire[simulateRequest](), wire[simulateResponse]())
	get("/api/analysis/domains", "handleAPIAnalysisDomains", wire[domainsResponse]())
	get("/api/tokens", "handleAPITokensRouter", wire[[]APIToken]())
	add("POST", "/api/tokens", "handleAPITokensRouter", wire[CreateAPITokenRequest](), 201, wire[CreatedAPITokenResponse]())
	mutation("DELETE", "/api/tokens/{id}", "handleAPITokenAction", nil)
	get("/api/users", "handleAPIUsersRouter", wire[[]AdminUser]())
	add("POST", "/api/users", "handleAPIUsersRouter", wire[CreateAdminUserRequest](), 201, wire[AdminUser]())
	mutation("PUT", "/api/users/{id}", "handleAPIUserAction", wire[UpdateAdminUserRequest]())
	mutation("DELETE", "/api/users/{id}", "handleAPIUserAction", nil)
	get("/api/config/export", "handleAPIConfigExport", wire[ConfigExport]())
	mutation("POST", "/api/config/import", "handleAPIConfigImport", wire[ConfigExport]())
	post("/api/archive", "handleAPIArchive", nil, wire[ArchiveStatusView]())
	get("/api/archive/status", "handleAPIArchiveStatus", wire[ArchiveStatusView]())
	get("/api/sync", "handleAPISync", wire[SyncResponse]())
	get("/api/peers", "handleAPIPeersGet", wire[PeersView]())
	add("POST", "/api/peers", "handleAPIPeersPost", wire[AddPeerRequest](), 201, wire[SuccessResponse]())
	mutation("DELETE", "/api/peers/{url}", "handleAPIPeers", nil)
	post("/api/peers/pair", "handleAPIPeersPair", nil, wire[PairingCodeResponse]())
	post("/api/peers/confirm", "handleAPIPeersConfirm", wire[ConfirmPeerRequest](), wire[ConfirmPeerResponse]())
	post("/api/sync/pair/complete", "handleAPISyncPairComplete", wire[SyncPairCompleteRequest](), wire[CompletePairingResponse]())
	post("/api/investigate", "handleAPIInvestigate", wire[InvestigateRequest](), wire[InvestigationView]())
	get("/api/investigate/schema", "handleAPIInvestigateSchema", wire[InvestigationSchemaView]())
	for _, action := range []string{"block-domain", "allow-domain"} {
		mutation("POST", "/api/ranges/{id}/"+action, "handleAPIRangeAction", wire[DomainRuleRequest]())
		mutation("DELETE", "/api/ranges/{id}/"+action+"/{domain}", "handleAPIRangeAction", nil)
	}
	return ops
}
