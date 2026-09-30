import type {
  Client,
  Group,
  PolicyView as PolicySummary,
  RangeView as Range,
} from '../../api/generated';
import '../../styles/pages/logs.css';
import '../../styles/pages/tiers.css';
import { useListPaneController } from './ListPane.controller';
import { TierTab, tabURLName } from './navigation';

export function ListPane({
  tab,
  search,
  setSearch,
  selectedId,
  setSelectedId,
  policies,
  ranges,
  groups,
  clients,
  refreshPolicies,
  refreshRanges,
  refreshGroups,
}: {
  tab: TierTab;
  search: string;
  setSearch: (s: string) => void;
  selectedId: string | null;
  setSelectedId: (id: string | null) => void;
  policies: PolicySummary[] | null;
  ranges: Range[] | null;
  groups: Group[] | null;
  clients: Client[] | null;
  refreshPolicies: () => void;
  refreshRanges: () => void;
  refreshGroups: () => void;
}) {
  const model = useListPaneController({
    tab,
    search,
    setSearch,
    selectedId,
    setSelectedId,
    policies,
    ranges,
    groups,
    clients,
    refreshPolicies,
    refreshRanges,
    refreshGroups,
  });
  return <ListPaneView model={model} />;
}

function ListPaneView({ model }: { model: NonNullable<ReturnType<typeof useListPaneController>> }) {
  const {
    tab,
    search,
    setSearch,
    selectedId,
    setSelectedId,
    action,
    showCreate,
    setShowCreate,
    newName,
    setNewName,
    newCidr,
    setNewCidr,
    creating,
    filteredPolicies,
    filteredRanges,
    filteredGroups,
    filteredClients,
    handleCreate,
    canCreate,
  } = model;
  return (
    <div className="list-pane">
      <div className="list-pane-search">
        <div style={{ display: 'flex', gap: 8 }}>
          <input
            className="form-input"
            style={{ flex: 1 }}
            value={search}
            onChange={(e) => {
              setSearch(e.target.value);
            }}
            placeholder={`Search ${tabURLName[tab]}...`}
          />
          {canCreate && (
            <button
              className="btn btn-primary"
              style={{ padding: '8px 12px', fontSize: '1rem', lineHeight: 1 }}
              onClick={() => {
                setShowCreate(!showCreate);
              }}
              title={`Create ${tab === 'policies' ? 'bundle' : tab === 'ranges' ? 'network' : 'group'}`}
              aria-label={`Create ${tab === 'policies' ? 'bundle' : tab === 'ranges' ? 'network' : 'group'}`}
            >
              +
            </button>
          )}
        </div>
      </div>
      {showCreate && canCreate && (
        <div className="list-pane-create">
          <input
            className="form-input"
            value={newName}
            onChange={(e) => {
              setNewName(e.target.value);
            }}
            placeholder="Name"
            onKeyDown={action(
              (e: React.KeyboardEvent<HTMLInputElement>) => e.key === 'Enter' && handleCreate(),
            )}
            autoFocus
          />
          {tab === 'ranges' && (
            <input
              className="form-input"
              value={newCidr}
              onChange={(e) => {
                setNewCidr(e.target.value);
              }}
              placeholder="CIDR (e.g. 10.0.0.0/24)"
              onKeyDown={action(
                (e: React.KeyboardEvent<HTMLInputElement>) => e.key === 'Enter' && handleCreate(),
              )}
            />
          )}
          <button className="btn btn-primary" onClick={action(handleCreate)} disabled={creating}>
            {creating ? 'Creating...' : 'Create'}
          </button>
        </div>
      )}
      <div className="list-pane-items">
        {tab === 'policies' &&
          filteredPolicies.map((p) => (
            <div
              key={p.id}
              className={`entity-row${selectedId === String(p.id) ? ' active' : ''}`}
              onClick={() => {
                setSelectedId(String(p.id));
              }}
            >
              <div className="entity-name">{p.name}</div>
              <div className="entity-meta">
                {p.blocklist_count} blocklists
                {p.allowlist_count > 0 && <span>&middot; {p.allowlist_count} allowlists</span>}
                {p.usage_count > 0 && <span>&middot; {p.usage_count} entities</span>}
              </div>
            </div>
          ))}
        {tab === 'ranges' &&
          filteredRanges.map((r) => (
            <div
              key={r.id}
              className={`entity-row${selectedId === String(r.id) ? ' active' : ''}`}
              onClick={() => {
                setSelectedId(String(r.id));
              }}
            >
              <div className="entity-name">{r.name}</div>
              <div className="entity-meta">{r.cidr}</div>
            </div>
          ))}
        {tab === 'groups' &&
          filteredGroups.map((g) => (
            <div
              key={g.id}
              className={`entity-row${selectedId === String(g.id) ? ' active' : ''}`}
              onClick={() => {
                setSelectedId(String(g.id));
              }}
            >
              <div className="entity-name">{g.name}</div>
              <div className="entity-meta">{g.member_count} members</div>
            </div>
          ))}
        {tab === 'clients' &&
          filteredClients.map((c) => (
            <div
              key={c.ip_address}
              className={`entity-row${selectedId === c.ip_address ? ' active' : ''}`}
              onClick={() => {
                setSelectedId(c.ip_address);
              }}
            >
              <div className="entity-name">{c.alias || c.ip_address}</div>
              <div className="entity-meta">{c.ip_address}</div>
            </div>
          ))}
      </div>
    </div>
  );
}
