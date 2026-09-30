import type { Client, PolicyView as PolicySummary } from '../../api/generated';
import '../../styles/pages/logs.css';
import '../../styles/pages/tiers.css';
import { useGroupDetailViewController } from './GroupDetailView.controller';
import {
  CustomRuleEditor,
  DeleteButton,
  ListToggleSection,
  PolicyRules,
  PolicySelector,
  RecentActivityWidget,
  StatsWidget,
} from './widgets';

export function GroupDetailView({
  id,
  clients,
  policies,
  onDelete,
}: {
  id: string;
  clients: Client[] | null;
  policies: PolicySummary[] | null;
  onDelete: () => void;
}) {
  const model = useGroupDetailViewController({ id, clients, policies, onDelete });
  if (model === null)
    return (
      <div className="detail-pane">
        <div className="spinner" />
      </div>
    );
  return <GroupDetailViewView model={model} />;
}

function GroupDetailViewView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useGroupDetailViewController>>;
}) {
  const {
    policies,
    data,
    newRule,
    setNewRule,
    ruleType,
    setRuleType,
    newMemberIp,
    setNewMemberIp,
    confirmDelete,
    setConfirmDelete,
    ruleError,
    policyData,
    availableClients,
    policyBlocklistIds,
    toggleBlocklist,
    addMember,
    removeMember,
    addRule,
    removeRule,
    assignPolicy,
    removePolicy,
    handleDelete,
  } = model;
  return (
    <div className="detail-pane">
      <div className="detail-header">
        <div>
          <div className="detail-title">{data.name}</div>
          <div className="detail-subtitle">{(data.members ?? []).length} members</div>
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
        <div className="widget-title">Members</div>
        <div style={{ display: 'flex', gap: 8, marginBottom: 12 }}>
          <select
            className="form-input"
            value={newMemberIp}
            onChange={(e) => {
              setNewMemberIp(e.target.value);
            }}
            style={{ flex: 1 }}
          >
            <option value="">Add a client...</option>
            {availableClients.map((c) => (
              <option key={c.ip_address} value={c.ip_address}>
                {c.alias ? `${c.alias} (${c.ip_address})` : c.ip_address}
              </option>
            ))}
          </select>
          <button className="btn btn-primary" style={{ padding: '8px 16px' }} onClick={addMember}>
            Add
          </button>
        </div>
        <div className="chip-grid">
          {(data.members ?? []).map((m) => (
            <span className="chip" key={m.ip_address}>
              {m.alias || m.ip_address}
              <button
                className="chip-remove"
                onClick={() => {
                  removeMember(m.ip_address);
                }}
              >
                x
              </button>
            </span>
          ))}
          {(data.members ?? []).length === 0 && (
            <span className="text-sm text-muted">No members</span>
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

      <RecentActivityWidget logs={data.recent_logs ?? []} />
    </div>
  );
}
