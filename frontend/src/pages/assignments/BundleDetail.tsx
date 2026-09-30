import '../../styles/pages/logs.css';
import '../../styles/pages/tiers.css';
import { usePolicyDetailViewController } from './PolicyDetailView.controller';
import { CustomRuleEditor, DeleteButton, ListToggleSection, PencilIcon } from './widgets';

export function PolicyDetailView({ id, onDelete }: { id: string; onDelete: () => void }) {
  const model = usePolicyDetailViewController({ id, onDelete });
  if (model === null)
    return (
      <div className="detail-pane">
        <div className="spinner" />
      </div>
    );
  return <PolicyDetailViewView model={model} />;
}

function PolicyDetailViewView({
  model,
}: {
  model: NonNullable<ReturnType<typeof usePolicyDetailViewController>>;
}) {
  const {
    data,
    editName,
    setEditName,
    editDesc,
    setEditDesc,
    editing,
    setEditing,
    newRule,
    setNewRule,
    ruleType,
    setRuleType,
    confirmDelete,
    setConfirmDelete,
    saveMeta,
    toggleBlocklist,
    toggleAllowlist,
    addRule,
    removeRule,
    ranges,
    groups,
    clients,
    totalEntities,
    handleDelete,
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
                  if (e.key === 'Enter') saveMeta();
                }}
                autoFocus
              />
              <input
                className="form-input"
                value={editDesc}
                onChange={(e) => {
                  setEditDesc(e.target.value);
                }}
                placeholder="Description (optional)"
                onKeyDown={(e) => {
                  if (e.key === 'Enter') saveMeta();
                }}
              />
              <div style={{ display: 'flex', gap: 8 }}>
                <button
                  className="btn btn-primary"
                  style={{ padding: '6px 16px' }}
                  onClick={saveMeta}
                >
                  Save
                </button>
                <button
                  className="btn"
                  style={{ padding: '6px 16px' }}
                  onClick={() => {
                    setEditing(false);
                    setEditName(data.name);
                    setEditDesc(data.description);
                  }}
                >
                  Cancel
                </button>
              </div>
            </div>
          ) : (
            <div
              className="editable-header"
              onClick={() => {
                setEditName(data.name);
                setEditDesc(data.description);
                setEditing(true);
              }}
            >
              <PencilIcon />
              <div className="detail-title">{data.name}</div>
              {!data.description && (
                <div className="text-sm text-muted" style={{ marginTop: 4, opacity: 0.5 }}>
                  Add description...
                </div>
              )}
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

      {totalEntities > 0 && (
        <div className="widget">
          <div className="widget-title">Assigned Entities</div>
          <div className="chip-grid">
            {(ranges ?? []).map((r) => (
              <span className="chip" key={`r-${String(r.id)}`}>
                {r.name}{' '}
                <span className="text-muted" style={{ fontSize: '0.7rem' }}>
                  {r.cidr}
                </span>
              </span>
            ))}
            {(groups ?? []).map((g) => (
              <span className="chip" key={`g-${String(g.id)}`}>
                {g.name}
              </span>
            ))}
            {(clients ?? []).map((c) => (
              <span className="chip" key={`c-${c.ip}`}>
                {c.alias || c.ip}
              </span>
            ))}
          </div>
        </div>
      )}

      <ListToggleSection
        title="Blocklists"
        items={data.blocklists ?? []}
        stats={data.blocklist_stats}
        onToggle={toggleBlocklist}
      />

      <ListToggleSection
        title="Allowlists"
        items={data.allowlists ?? []}
        onToggle={toggleAllowlist}
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
      />
    </div>
  );
}
