import type { BlocklistView as Blocklist } from '../api/generated';
import { useDetailsTabController } from './DetailsTab.controller';
import { useFiltersController } from './Filters.controller';
import { useHistoryTabController } from './HistoryTab.controller';
import { useListsTabController } from './ListsTab.controller';

import Toggle from '../components/Toggle';
import ListCompatibility from './ListCompatibility';
import TopBar from '../components/TopBar';

import '../styles/pages/filters.css';

export default function Filters() {
  const model = useFiltersController();
  return <FiltersView model={model} />;
}

const REFRESH_OPTIONS = [
  { label: 'Off', value: 0 },
  { label: '12h', value: 43200 },
  { label: 'Daily', value: 86400 },
  { label: '3 days', value: 259200 },
  { label: 'Weekly', value: 604800 },
];

function ListsTab({
  blocklists,
  addUrl,
  setAddUrl,
  addName,
  setAddName,
  onAdd,
  onToggle,
  onRename,
  onRefresh,
  onDelete,
  onSetRefreshInterval,
}: {
  blocklists: Blocklist[];
  addUrl: string;
  setAddUrl: (v: string) => void;
  addName: string;
  setAddName: (v: string) => void;
  onAdd: () => void;
  onToggle: (id: number, enabled: boolean) => void;
  onRename: (id: number, alias: string) => Promise<void>;
  onRefresh: (id: number) => void;
  onDelete: (id: number) => void;
  onSetRefreshInterval: (id: number, interval: number) => void;
}) {
  const model = useListsTabController({
    blocklists,
    addUrl,
    setAddUrl,
    addName,
    setAddName,
    onAdd,
    onToggle,
    onRename,
    onRefresh,
    onDelete,
    onSetRefreshInterval,
  });
  return <ListsTabView model={model} />;
}

function HistoryTab({ blocklists }: { blocklists: Blocklist[] }) {
  const model = useHistoryTabController({ blocklists });
  return <HistoryTabView model={model} />;
}

function DetailsTab({
  blocklists,
  selectedList,
  setSelectedList,
  detailSearch,
  setDetailSearch,
}: {
  blocklists: Blocklist[];
  selectedList: number | null;
  setSelectedList: (id: number | null) => void;
  detailSearch: string;
  setDetailSearch: (s: string) => void;
}) {
  const model = useDetailsTabController({
    blocklists,
    selectedList,
    setSelectedList,
    detailSearch,
    setDetailSearch,
  });
  return <DetailsTabView model={model} />;
}

function FiltersView({ model }: { model: NonNullable<ReturnType<typeof useFiltersController>> }) {
  const {
    action,
    tab,
    blocklists,
    addUrl,
    setAddUrl,
    addName,
    setAddName,
    selectedList,
    detailSearch,
    setTab,
    setSelectedList,
    setDetailSearch,
    addList,
    toggleList,
    renameList,
    refreshList,
    deleteList,
    setRefreshInterval,
  } = model;
  return (
    <div className="filters-page">
      <TopBar title="Filter Lists" />

      <div className="sub-nav">
        <button
          className={`tab-item${tab === 'lists' ? ' active' : ''}`}
          onClick={() => {
            setTab('lists');
          }}
        >
          Published Lists
        </button>
        <button
          className={`tab-item${tab === 'history' ? ' active' : ''}`}
          onClick={() => {
            setTab('history');
          }}
        >
          History & Changelog
        </button>
        <button
          className={`tab-item${tab === 'details' ? ' active' : ''}`}
          onClick={() => {
            setTab('details');
          }}
        >
          List Details
        </button>
      </div>

      <div className="filters-content">
        {tab === 'lists' && (
          <ListsTab
            blocklists={blocklists ?? []}
            addUrl={addUrl}
            setAddUrl={setAddUrl}
            addName={addName}
            setAddName={setAddName}
            onAdd={action(addList)}
            onToggle={action(toggleList)}
            onRename={renameList}
            onRefresh={action(refreshList)}
            onDelete={action(deleteList)}
            onSetRefreshInterval={action(setRefreshInterval)}
          />
        )}
        {tab === 'history' && <HistoryTab blocklists={blocklists ?? []} />}
        {tab === 'details' && (
          <DetailsTab
            blocklists={blocklists ?? []}
            selectedList={selectedList}
            setSelectedList={setSelectedList}
            detailSearch={detailSearch}
            setDetailSearch={setDetailSearch}
          />
        )}
      </div>
    </div>
  );
}

function ListsTabView({ model }: { model: NonNullable<ReturnType<typeof useListsTabController>> }) {
  const {
    blocklists,
    addUrl,
    setAddUrl,
    addName,
    setAddName,
    onAdd,
    onToggle,
    onRefresh,
    onDelete,
    onSetRefreshInterval,
    action,
    editingId,
    setEditingId,
    editName,
    setEditName,
    startEdit,
    saveEdit,
  } = model;
  return (
    <>
      <div className="add-bar">
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="var(--text-muted)"
          strokeWidth="2"
        >
          <rect x="3" y="3" width="18" height="18" rx="2" ry="2" />
          <line x1="12" y1="8" x2="12" y2="16" />
          <line x1="8" y1="12" x2="16" y2="12" />
        </svg>
        <input
          type="text"
          className="add-input"
          style={{ flex: 1 }}
          placeholder="Paste Blocklist URL (e.g. https://raw.githubusercontent.com/...)"
          value={addUrl}
          onChange={(e) => {
            setAddUrl(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') onAdd();
          }}
        />
        <input
          type="text"
          className="add-input"
          style={{ width: 200, flex: 'none' }}
          placeholder="Name (optional)"
          value={addName}
          onChange={(e) => {
            setAddName(e.target.value);
          }}
          onKeyDown={(e) => {
            if (e.key === 'Enter') onAdd();
          }}
        />
        <button className="btn btn-primary" onClick={onAdd}>
          Import List
        </button>
      </div>

      <div className="data-grid">
        <div className="filters-grid-header">
          <div style={{ textAlign: 'center' }}>St</div>
          <div>List Name / Source</div>
          <div>Domains</div>
          <div>Updated</div>
          <div>Refresh</div>
          <div>Actions</div>
          <div style={{ textAlign: 'center' }}>On/Off</div>
        </div>

        {blocklists.map((bl) => (
          <div className="filters-grid-row" key={bl.id}>
            <div className="cell-status">
              <div
                className={`status-indicator ${bl.enabled ? 'status-active' : 'status-disabled'}`}
              />
            </div>
            <div className="cell-name">
              {editingId === bl.id ? (
                <input
                  className="form-input"
                  style={{ width: '100%', fontSize: '0.9rem' }}
                  value={editName}
                  onChange={(e) => {
                    setEditName(e.target.value);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') action(() => saveEdit(bl.id))();
                    if (e.key === 'Escape') setEditingId(null);
                  }}
                  autoFocus
                />
              ) : (
                <>
                  <div className="list-name">{bl.alias || 'Unnamed'}</div>
                  <div className="list-url">{bl.url}</div>
                  <ListCompatibility
                    key={`${String(bl.id)}-${bl.compatibility.assessed_at}`}
                    id={bl.id}
                    summary={bl.compatibility}
                  />
                </>
              )}
            </div>
            <div className="domain-count">{bl.domain_count.toLocaleString()}</div>
            <div className="list-updated">
              {bl.last_updated ? new Date(bl.last_updated).toLocaleString() : 'Never'}
            </div>
            <div
              onClick={(e) => {
                e.stopPropagation();
              }}
            >
              <select
                className="select-input"
                style={{ fontSize: '0.8rem', padding: '2px 4px', width: '100%' }}
                value={bl.refresh_interval}
                onChange={(e) => {
                  onSetRefreshInterval(bl.id, Number(e.target.value));
                }}
              >
                {REFRESH_OPTIONS.map((opt) => (
                  <option key={opt.value} value={opt.value}>
                    {opt.label}
                  </option>
                ))}
              </select>
            </div>
            <div
              className="cell-actions"
              onClick={(e) => {
                e.stopPropagation();
              }}
            >
              {editingId === bl.id ? (
                <>
                  <button
                    className="btn btn-primary"
                    style={{ padding: '4px 8px', fontSize: '0.75rem' }}
                    onClick={() => {
                      action(() => saveEdit(bl.id))();
                    }}
                  >
                    Save
                  </button>
                  <button
                    className="btn-icon"
                    onClick={() => {
                      setEditingId(null);
                    }}
                  >
                    Cancel
                  </button>
                </>
              ) : (
                <>
                  <button
                    className="btn-icon"
                    title="Rename"
                    aria-label="Rename list"
                    onClick={() => {
                      startEdit(bl);
                    }}
                  >
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                    >
                      <path d="M12 20h9" />
                      <path d="M16.5 3.5a2.121 2.121 0 0 1 3 3L7 19l-4 1 1-4L16.5 3.5z" />
                    </svg>
                  </button>
                  <button
                    className="btn-icon"
                    title="Refresh"
                    aria-label="Refresh list"
                    onClick={() => {
                      onRefresh(bl.id);
                    }}
                  >
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                    >
                      <path d="M23 4v6h-6" />
                      <path d="M1 20v-6h6" />
                      <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
                    </svg>
                  </button>
                  <button
                    className="btn-icon"
                    style={{ color: 'var(--red)' }}
                    title="Delete"
                    aria-label="Delete list"
                    onClick={() => {
                      onDelete(bl.id);
                    }}
                  >
                    <svg
                      width="16"
                      height="16"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="2"
                    >
                      <polyline points="3 6 5 6 21 6" />
                      <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
                    </svg>
                  </button>
                </>
              )}
            </div>
            <div
              style={{ textAlign: 'center' }}
              onClick={(e) => {
                e.stopPropagation();
              }}
            >
              <Toggle
                checked={bl.enabled}
                onChange={(v) => {
                  onToggle(bl.id, v);
                }}
              />
            </div>
          </div>
        ))}
        {blocklists.length === 0 && (
          <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>
            No blocklists configured
          </div>
        )}
      </div>
    </>
  );
}

function HistoryTabView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useHistoryTabController>>;
}) {
  const { blocklists, history, listFilter, setListFilter, search, setSearch, filteredHistory } =
    model;
  return (
    <div>
      <div className="history-toolbar">
        <select
          className="select-input"
          value={listFilter ?? ''}
          onChange={(e) => {
            setListFilter(e.target.value ? Number(e.target.value) : null);
          }}
        >
          <option value="">All Lists</option>
          {blocklists.map((bl) => (
            <option key={bl.id} value={bl.id}>
              {bl.alias || bl.url}
            </option>
          ))}
        </select>
        <input
          type="text"
          className="select-input"
          style={{ flex: 1 }}
          placeholder="Search domain history (e.g. 'tiktok.com')..."
          value={search}
          onChange={(e) => {
            setSearch(e.target.value);
          }}
        />
      </div>

      <div className="timeline-container">
        {filteredHistory.length === 0 && (
          <div className="empty-state">
            <p>
              {(history ?? []).length === 0
                ? 'No history entries yet'
                : 'No history entries match your filters'}
            </p>
          </div>
        )}
        {filteredHistory.map((entry) => (
          <div className="timeline-item" key={entry.id}>
            <div className="timeline-line" />
            <div className="timeline-marker">
              <svg
                width="20"
                height="20"
                viewBox="0 0 24 24"
                fill="none"
                stroke="var(--green)"
                strokeWidth="2"
              >
                <path d="M23 4v6h-6" />
                <path d="M1 20v-6h6" />
                <path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15" />
              </svg>
            </div>
            <div className="timeline-content">
              <div
                style={{
                  display: 'flex',
                  justifyContent: 'space-between',
                  marginBottom: 8,
                  borderBottom: '1px solid var(--bg-hover)',
                  paddingBottom: 8,
                }}
              >
                <span className="timeline-source">{entry.blocklist_alias}</span>
                <span className="timeline-date">
                  {new Date(entry.refreshed_at).toLocaleString()}
                </span>
              </div>
              <div style={{ marginBottom: 8 }}>
                <span className="diff-stat add">+{entry.added_count}</span>
                <span className="diff-stat del">-{entry.removed_count}</span>
                <span className="text-sm text-muted">Auto-refresh complete</span>
              </div>
              {entry.sample_added &&
                entry.sample_added.map((d) => (
                  <div
                    key={d}
                    style={{
                      fontFamily: 'JetBrains Mono',
                      fontSize: '0.85rem',
                      marginTop: 4,
                      color: 'var(--text-muted)',
                    }}
                  >
                    <span style={{ color: 'var(--green)' }}>+ {d}</span>
                  </div>
                ))}
              {entry.sample_removed &&
                entry.sample_removed.map((d) => (
                  <div
                    key={d}
                    style={{
                      fontFamily: 'JetBrains Mono',
                      fontSize: '0.85rem',
                      marginTop: 4,
                      color: 'var(--text-muted)',
                    }}
                  >
                    <span style={{ color: 'var(--red)' }}>- {d}</span>
                  </div>
                ))}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

function DetailsTabView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useDetailsTabController>>;
}) {
  const {
    blocklists,
    selectedList,
    detailSearch,
    setDetailSearch,
    selectedCheckpoint,
    setSelectedCheckpoint,
    listHistory,
    domains,
    handleListChange,
  } = model;
  return (
    <div className="domain-browser">
      <div className="browser-header">
        <select
          className="select-input"
          value={selectedList ?? ''}
          onChange={(e) => {
            handleListChange(e.target.value ? Number(e.target.value) : null);
          }}
        >
          <option value="" disabled>
            Select List...
          </option>
          {blocklists.map((bl) => (
            <option key={bl.id} value={bl.id}>
              {bl.alias || bl.url} ({bl.domain_count.toLocaleString()})
            </option>
          ))}
        </select>
        {selectedList && listHistory.length > 0 && (
          <select
            className="select-input"
            value={selectedCheckpoint ?? ''}
            onChange={(e) => {
              setSelectedCheckpoint(e.target.value ? Number(e.target.value) : null);
            }}
          >
            <option value="">Current</option>
            {listHistory.map((h) => (
              <option key={h.id} value={h.id}>
                {new Date(h.refreshed_at).toLocaleString()} ({h.new_count.toLocaleString()} domains)
              </option>
            ))}
          </select>
        )}
        <input
          type="text"
          className="select-input"
          style={{ flex: 1 }}
          placeholder="Search domains within list..."
          value={detailSearch}
          onChange={(e) => {
            setDetailSearch(e.target.value);
          }}
        />
      </div>

      {selectedCheckpoint && <div className="checkpoint-badge">Viewing historical checkpoint</div>}

      <div style={{ flex: 1, overflowY: 'auto' }}>
        {selectedList && domains ? (
          <>
            <div className="domain-count-bar">
              {domains.total.toLocaleString()} domains{detailSearch ? ' matching' : ''}
            </div>
            {(domains.domains ?? []).map((d) => (
              <div className="domain-row" key={d}>
                <span>{d}</span>
              </div>
            ))}
            {(domains.domains ?? []).length === 0 && (
              <div className="empty-state">
                <p>No matching domains</p>
              </div>
            )}
          </>
        ) : (
          <div className="empty-state">
            <p>Select a list to browse domains</p>
          </div>
        )}
      </div>
    </div>
  );
}
