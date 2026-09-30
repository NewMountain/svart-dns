import type { PolicyView as PolicySummary } from '../../api/generated';
import '../../styles/pages/logs.css';
import '../../styles/pages/tiers.css';
import { useClientDetailViewController } from './ClientDetailView.controller';
import {
  CustomRuleEditor,
  ListToggleSection,
  PencilIcon,
  PolicyRules,
  PolicySelector,
  RecentActivityWidget,
  StatsWidget,
} from './widgets';

export function ClientDetailView({
  ip,
  policies,
}: {
  ip: string;
  policies: PolicySummary[] | null;
}) {
  const model = useClientDetailViewController({ ip, policies });
  if (model === null)
    return (
      <div className="detail-pane">
        <div className="spinner" />
      </div>
    );
  return <ClientDetailViewView model={model} />;
}

function ClientDetailViewView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useClientDetailViewController>>;
}) {
  const {
    policies,
    data,
    newRule,
    setNewRule,
    ruleType,
    setRuleType,
    editingAlias,
    setEditingAlias,
    aliasValue,
    setAliasValue,
    ruleError,
    recentLogs,
    policyData,
    policyBlocklistIds,
    toggleBlocklist,
    addRule,
    removeRule,
    assignPolicy,
    removePolicy,
    saveAlias,
  } = model;
  const memberGroups = (data.groups ?? []).filter((group) => group.is_member);
  return (
    <div className="detail-pane">
      <div className="detail-header">
        <div>
          {editingAlias ? (
            <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <input
                className="form-input"
                value={aliasValue}
                onChange={(e) => {
                  setAliasValue(e.target.value);
                }}
                style={{ fontSize: '1.2rem', fontWeight: 700, width: 250 }}
                placeholder="Device name..."
                onKeyDown={(e) => {
                  if (e.key === 'Enter') saveAlias();
                  if (e.key === 'Escape') setEditingAlias(false);
                }}
                autoFocus
              />
              <button
                className="btn btn-primary"
                style={{ padding: '6px 12px' }}
                onClick={saveAlias}
              >
                Save
              </button>
              <button
                className="btn"
                style={{ padding: '6px 12px' }}
                onClick={() => {
                  setEditingAlias(false);
                }}
              >
                Cancel
              </button>
            </div>
          ) : (
            <div
              className="editable-header"
              onClick={() => {
                setAliasValue(data.alias || '');
                setEditingAlias(true);
              }}
            >
              <PencilIcon />
              <div className="detail-title">{data.alias || data.ip}</div>
            </div>
          )}
          <div className="detail-subtitle">{data.ip}</div>
        </div>
      </div>

      <PolicySelector
        current={data.policy}
        policies={policies}
        onAssign={assignPolicy}
        onRemove={removePolicy}
      />

      <StatsWidget
        totalQueries={data.total_queries}
        avgLatency={data.avg_latency_microseconds}
        firstSeen={data.first_seen}
        lastSeen={data.last_seen}
      />

      <div className="widget">
        <div className="widget-title">Groups</div>
        <div className="chip-grid">
          {memberGroups.map((g) => (
            <span className="chip" key={g.id}>
              {g.name}
            </span>
          ))}
          {memberGroups.length === 0 && (
            <span className="text-sm text-muted">Not in any groups</span>
          )}
        </div>
      </div>

      <ListToggleSection
        title="Blocklists"
        items={data.blocklists ?? []}
        stats={data.blocklist_stats}
        onToggle={toggleBlocklist}
        lockedIds={policyBlocklistIds}
        policyName={data.policy?.name}
      />

      <CustomRuleEditor
        newRule={newRule}
        setNewRule={setNewRule}
        ruleType={ruleType}
        setRuleType={setRuleType}
        onAdd={addRule}
        customBlocked={data.custom_blocked ?? []}
        customAllowed={data.custom_allowed ?? []}
        onRemove={removeRule}
        error={ruleError}
        extra={<PolicyRules policyData={policyData} />}
        showEmptyState={!policyData}
      />

      <RecentActivityWidget logs={recentLogs?.logs ?? []} />
    </div>
  );
}
