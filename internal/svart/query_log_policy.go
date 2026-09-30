package svart

import (
	"encoding/json"
	"github.com/yeti/svart-dns/internal/policycore"
)

type queryLogPolicySnapshot struct {
	Result          string                     `json:"result"`
	ResultSource    *policycore.EntityResult   `json:"result_source,omitempty"`
	RangeEvaluation *policycore.TierEvaluation `json:"range_evaluation,omitempty"`
	GroupEvaluation *policycore.TierEvaluation `json:"group_evaluation,omitempty"`
	IPEvaluation    *policycore.TierEvaluation `json:"ip_evaluation,omitempty"`
}

type legacyBlockFields struct {
	tier     string
	rule     string
	source   string
	listID   int
	listName string
}

func buildQueryLogPolicySnapshot(e queryLogEntry) *queryLogPolicySnapshot {
	result := e.result
	if result == "" {
		switch {
		case e.blocked:
			result = "block"
		case e.upstream == "rewrite":
			result = "rewrite"
		default:
			result = "allow"
		}
	}

	snapshot := &queryLogPolicySnapshot{
		Result:          result,
		ResultSource:    buildQueryLogPolicyEntity(e.resultTier, e.resultEntity, e.result, e.resultIsPublished, e.resultRule, e.resultListID, e.resultListName),
		RangeEvaluation: buildQueryLogPolicyTier("range", e.rangeEntity, e.rangeResult, e.rangeIsPublished, e.rangeRule, e.rangeListID, e.rangeListName),
		GroupEvaluation: buildQueryLogPolicyTier("group", e.groupEntity, e.groupResult, e.groupIsPublished, e.groupRule, e.groupListID, e.groupListName),
		IPEvaluation:    buildQueryLogPolicyTier("ip", e.ipEntity, e.ipResult, e.ipIsPublished, e.ipRule, e.ipListID, e.ipListName),
	}

	if snapshot.ResultSource == nil && result == "allow" {
		snapshot.ResultSource = &policycore.EntityResult{
			Tier:   "default",
			Name:   "no matching rule",
			Result: "allow",
		}
	}

	return snapshot
}

func buildQueryLogPolicyTier(tier, entityName, result string, isPublished bool, rule string, listID int, listName string) *policycore.TierEvaluation {
	entity := buildQueryLogPolicyEntity(tier, entityName, result, isPublished, rule, listID, listName)
	if entity == nil {
		return nil
	}
	return &policycore.TierEvaluation{
		Entities: []policycore.EntityResult{*entity},
		Result:   result,
	}
}

func buildQueryLogPolicyEntity(tier, entityName, result string, isPublished bool, rule string, listID int, listName string) *policycore.EntityResult {
	if entityName == "" && result == "" && rule == "" && listName == "" && listID == 0 {
		return nil
	}

	entity := &policycore.EntityResult{
		Tier:   tier,
		Name:   entityName,
		Result: result,
	}

	if isPublished && (rule != "" || listID > 0 || listName != "") {
		entity.PublishedList = &policycore.PublishedHit{
			Action:   result,
			Rule:     rule,
			ListID:   listID,
			ListName: listName,
		}
	} else if rule != "" {
		entity.CustomRule = &policycore.CustomHit{
			Action: result,
			Rule:   rule,
		}
	}

	return entity
}

func shouldPersistQueryLogPolicyJSON(e queryLogEntry) bool {
	if e.result != "" && e.result != "allow" {
		return true
	}
	if e.resultReason != "" && e.resultReason != "default_allow" {
		return true
	}
	if e.resultTier != "" && e.resultTier != "default" {
		return true
	}
	if e.rangeResult != "" || e.groupResult != "" || e.ipResult != "" {
		return true
	}
	return false
}

func buildQueryLogPolicyJSON(e queryLogEntry) string {
	if !shouldPersistQueryLogPolicyJSON(e) {
		return ""
	}

	jsonBytes, err := json.Marshal(buildQueryLogPolicySnapshot(e))
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

func policySnapshotToPolicyResult(snapshot *queryLogPolicySnapshot) *policycore.PolicyResult {
	if snapshot == nil {
		return nil
	}
	return &policycore.PolicyResult{
		Result:          snapshot.Result,
		ResultSource:    snapshot.ResultSource,
		RangeEvaluation: snapshot.RangeEvaluation,
		GroupEvaluation: snapshot.GroupEvaluation,
		IPEvaluation:    snapshot.IPEvaluation,
	}
}

func deriveLegacyBlockFields(e queryLogEntry) legacyBlockFields {
	if !e.blocked {
		return legacyBlockFields{}
	}

	fields := legacyBlockFields{
		tier:   e.resultTier,
		rule:   e.resultRule,
		source: e.resultEntity,
	}
	if e.resultIsPublished {
		fields.listID = e.resultListID
		fields.listName = e.resultListName
	}
	return fields
}
