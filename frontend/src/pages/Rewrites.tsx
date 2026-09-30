import Toggle from '../components/Toggle';
import TopBar from '../components/TopBar';
import { useRewritesController } from './Rewrites.controller';

import '../styles/pages/rewrites.css';

export default function Rewrites() {
  const model = useRewritesController();
  return <RewritesView model={model} />;
}

function RewritesView({ model }: { model: NonNullable<ReturnType<typeof useRewritesController>> }) {
  const {
    action,
    domain,
    setDomain,
    ip,
    setIp,
    search,
    setSearch,
    editingId,
    setEditingId,
    editDomain,
    setEditDomain,
    editIp,
    setEditIp,
    statsMap,
    filtered,
    addRewrite,
    toggleRewrite,
    deleteRewrite,
    saveEdit,
    startEdit,
  } = model;
  return (
    <div className="rewrites-page">
      <TopBar title="DNS Rewrites" />

      <div className="rewrites-content">
        {/* Quick Add */}
        <div className="quick-add">
          <div className="input-group">
            <input
              className="form-input"
              value={domain}
              onChange={(e) => {
                setDomain(e.target.value);
              }}
              placeholder="app.local"
              onKeyDown={action(
                (e: React.KeyboardEvent<HTMLInputElement>) => e.key === 'Enter' && addRewrite(),
              )}
            />
            <span className="rewrite-arrow">→</span>
            <input
              className="form-input rewrite-input-purple"
              value={ip}
              onChange={(e) => {
                setIp(e.target.value);
              }}
              placeholder="192.168.1.100"
              onKeyDown={action(
                (e: React.KeyboardEvent<HTMLInputElement>) => e.key === 'Enter' && addRewrite(),
              )}
            />
          </div>
          <button className="btn btn-primary" onClick={action(addRewrite)}>
            + Add Record
          </button>
        </div>

        {/* Data Grid */}
        <div className="data-grid">
          <div className="data-grid-search">
            <input
              className="form-input"
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
              }}
              placeholder="Search domains..."
            />
          </div>
          <div className="rewrites-grid-header">
            <div style={{ textAlign: 'center' }}></div>
            <div>Domain</div>
            <div></div>
            <div>IP Address</div>
            <div>Activity (24h)</div>
            <div>Actions</div>
            <div style={{ textAlign: 'center' }}>On/Off</div>
          </div>

          {filtered.map((rw) => {
            const stat = statsMap.get(rw.domain);
            return (
              <div className={`rewrites-grid-row${!rw.enabled ? ' disabled' : ''}`} key={rw.id}>
                <div className="cell-status">
                  <div
                    className={`status-indicator ${rw.enabled ? 'status-active' : 'status-disabled'}`}
                  />
                </div>
                <div className="rewrite-domain">
                  {editingId === rw.id ? (
                    <input
                      className="form-input"
                      value={editDomain}
                      onChange={(e) => {
                        setEditDomain(e.target.value);
                      }}
                      style={{ width: '100%' }}
                    />
                  ) : (
                    rw.domain
                  )}
                </div>
                <div className="rewrite-grid-arrow">→</div>
                <div className="rewrite-ip">
                  {editingId === rw.id ? (
                    <input
                      className="form-input"
                      value={editIp}
                      onChange={(e) => {
                        setEditIp(e.target.value);
                      }}
                      style={{ width: '100%' }}
                    />
                  ) : (
                    rw.ip_addresses
                  )}
                </div>
                <div className="stats-cell">
                  <span className="stat-badge">
                    <span className="hit-count">{stat?.hits ?? 0}</span> hits
                  </span>
                  <div className="client-stat-wrapper">
                    <span className="stat-badge interactive">
                      <span className="client-count">{stat?.unique_clients ?? 0}</span> clients
                    </span>
                    {stat?.top_clients && stat.top_clients.length > 0 && (
                      <div className="client-popover">
                        <div className="popover-header">Top Clients</div>
                        {stat.top_clients.map((c) => (
                          <div className="popover-row" key={c.ip}>
                            <span>{c.ip}</span>
                            <span className="popover-hits">{c.count}</span>
                          </div>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
                <div className="cell-actions" style={{ opacity: 1 }}>
                  {editingId === rw.id ? (
                    <div style={{ display: 'flex', gap: 4 }}>
                      <button
                        className="btn btn-primary"
                        style={{ padding: '4px 8px', fontSize: '0.75rem' }}
                        onClick={action(() => saveEdit(rw.id))}
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
                    </div>
                  ) : (
                    <>
                      <button
                        className="btn-icon"
                        title="Edit"
                        aria-label="Edit rewrite"
                        onClick={() => {
                          startEdit(rw);
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
                        style={{ color: 'var(--red)' }}
                        title="Delete"
                        aria-label="Delete rewrite"
                        onClick={action(() => deleteRewrite(rw.id))}
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
                <div style={{ textAlign: 'center' }}>
                  <Toggle
                    checked={rw.enabled}
                    onChange={action((v: boolean) => toggleRewrite(rw.id, v))}
                  />
                </div>
              </div>
            );
          })}
          {filtered.length === 0 && (
            <div style={{ padding: 48, textAlign: 'center', color: 'var(--text-muted)' }}>
              {search ? 'No matching rewrites' : 'No rewrites configured'}
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
