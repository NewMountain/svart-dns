import type { PolicyView as PolicySummary } from '../../api/generated';
import '../../styles/pages/logs.css';
import '../../styles/pages/tiers.css';
import { useRangeDetailViewController } from './RangeDetailView.controller';
import {
  CustomRuleEditor,
  DeleteButton,
  ListToggleSection,
  OctetVisualizer,
  PencilIcon,
  PolicyRules,
  PolicySelector,
  RecentActivityWidget,
  StatsWidget,
} from './widgets';

export function RangeDetailView({
  id,
  policies,
  onDelete,
}: {
  id: string;
  policies: PolicySummary[] | null;
  onDelete: () => void;
}) {
  const model = useRangeDetailViewController({ id, policies, onDelete });
  if (model === null)
    return (
      <div className="detail-pane">
        <div className="spinner" />
      </div>
    );
  return <RangeDetailViewView model={model} />;
}

function RangeDetailViewView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useRangeDetailViewController>>;
}) {
  const {
    policies,
    data,
    newRule,
    setNewRule,
    ruleType,
    setRuleType,
    confirmDelete,
    setConfirmDelete,
    editing,
    setEditing,
    editName,
    setEditName,
    editCidr,
    setEditCidr,
    policyData,
    policyBlocklistIds,
    toggleBlocklist,
    addRule,
    removeRule,
    assignPolicy,
    removePolicy,
    handleDelete,
    startEditing,
    saveRange,
  } = model;
  return (
    <div className="detail-pane">
      <div className="detail-header">
        <div style={{ flex: 1 }}>
          {editing ? (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              <input
                className="form-input"
                value={editName}
                onChange={(e) => {
                  setEditName(e.target.value);
                }}
                style={{ fontSize: '1.2rem', fontWeight: 700 }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') saveRange();
                }}
                autoFocus
              />
              <input
                className="form-input"
                value={editCidr}
                onChange={(e) => {
                  setEditCidr(e.target.value);
                }}
                placeholder="CIDR (e.g. 10.0.0.0/24)"
                style={{ fontFamily: 'JetBrains Mono, monospace', fontSize: '0.9rem' }}
                onKeyDown={(e) => {
                  if (e.key === 'Enter') saveRange();
                }}
              />
              <div style={{ display: 'flex', gap: 8 }}>
                <button
                  className="btn btn-primary"
                  style={{ padding: '6px 16px' }}
                  onClick={saveRange}
                >
                  Save
                </button>
                <button
                  className="btn"
                  style={{ padding: '6px 16px' }}
                  onClick={() => {
                    setEditing(false);
                  }}
                >
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <div className="editable-header" onClick={startEditing}>
              <PencilIcon />
              <div className="detail-title">{data.name}</div>
              <div className="detail-subtitle">{data.cidr}</div>
            </div>
          )}
        </div>
        <DeleteButton
          confirming={confirmDelete}
          onConfirm={handleDelete}
          onArm={() => {
            setConfirmDelete(true);
          }}
          onCancel={() => {
            setConfirmDelete(false);
          }}
        />
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
        <div className="widget-title">CIDR Scope</div>
        <OctetVisualizer cidr={data.cidr} />
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
        extra={<PolicyRules policyData={policyData} />}
        showEmptyState={!policyData}
      />

      <RecentActivityWidget logs={data.recent_logs ?? []} />
    </div>
  );
}
