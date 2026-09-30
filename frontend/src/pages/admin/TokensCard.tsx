import '../../styles/pages/admin.css';
import type { AdminController } from './useAdminController';
export function TokensCard({ model }: { model: AdminController }) {
  const {
    action,
    tokens,
    showCreateToken,
    setShowCreateToken,
    newTokenName,
    setNewTokenName,
    newTokenRole,
    setNewTokenRole,
    confirmRevokeToken,
    setConfirmRevokeToken,
    createToken,
    revokeToken,
    formatDate,
    formatLastUsed,
  } = model;
  return (
    <div className="admin-card">
      <div className="admin-card-header">
        <div>
          <div className="admin-card-title">API Tokens</div>
          <div className="card-desc">
            Authentication for scripts and integrations. Full tokens are only shown at creation
            time.
          </div>
        </div>
        <button
          className="btn btn-primary"
          onClick={() => {
            setShowCreateToken(!showCreateToken);
          }}
        >
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="3"
          >
            <line x1="12" y1="5" x2="12" y2="19" />
            <line x1="5" y1="12" x2="19" y2="12" />
          </svg>
          New Token
        </button>
      </div>

      {showCreateToken && (
        <div className="create-panel">
          <div className="admin-form-row">
            <div className="admin-input-group" style={{ flex: 2 }}>
              <div className="admin-label">Token Name</div>
              <input
                type="text"
                className="admin-input-field"
                placeholder="e.g. Grafana Dashboard"
                value={newTokenName}
                onChange={(e) => {
                  setNewTokenName(e.target.value);
                }}
              />
            </div>
            <div className="admin-input-group" style={{ flex: 1 }}>
              <div className="admin-label">Role</div>
              <select
                className="admin-select"
                value={newTokenRole}
                onChange={(e) => {
                  setNewTokenRole(e.target.value);
                }}
              >
                <option value="readonly">Read Only</option>
                <option value="admin">Admin</option>
              </select>
            </div>
            <button className="btn btn-primary" onClick={action(createToken)}>
              Generate
            </button>
            <button
              className="btn btn-ghost"
              onClick={() => {
                setShowCreateToken(false);
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      <table className="data-table">
        <thead>
          <tr>
            <th>Token Name</th>
            <th>Role</th>
            <th>Key</th>
            <th>Created</th>
            <th>Last Used</th>
            <th style={{ textAlign: 'right' }}>Actions</th>
          </tr>
        </thead>
        <tbody>
          {tokens && tokens.length > 0 ? (
            tokens.map((t) => (
              <tr key={t.id}>
                <td style={{ fontWeight: 500 }}>{t.name}</td>
                <td>
                  <span className={`role-badge ${t.role === 'admin' ? 'role-admin' : 'role-view'}`}>
                    {t.role === 'admin' ? 'Admin' : 'Read Only'}
                  </span>
                </td>
                <td>
                  <span className="key-fragment">{t.token_prefix}...</span>
                </td>
                <td>{formatDate(t.created_at)}</td>
                <td>{formatLastUsed(t.last_used_at)}</td>
                <td style={{ textAlign: 'right' }}>
                  {confirmRevokeToken === t.id ? (
                    <span className="confirm-inline">
                      <span className="text-sm" style={{ color: 'var(--red)' }}>
                        Revoke?
                      </span>
                      <button
                        className="btn btn-danger btn-sm"
                        onClick={action(() => revokeToken(t.id))}
                      >
                        Yes
                      </button>
                      <button
                        className="btn btn-ghost btn-sm"
                        onClick={() => {
                          setConfirmRevokeToken(null);
                        }}
                      >
                        No
                      </button>
                    </span>
                  ) : (
                    <button
                      className="admin-btn-icon"
                      title="Revoke"
                      aria-label="Revoke token"
                      onClick={() => {
                        setConfirmRevokeToken(t.id);
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
                        <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6" />
                        <path d="M10 11v6" />
                        <path d="M14 11v6" />
                        <path d="M9 6V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2" />
                      </svg>
                    </button>
                  )}
                </td>
              </tr>
            ))
          ) : (
            <tr>
              <td
                colSpan={6}
                style={{ textAlign: 'center', color: 'var(--text-muted)', padding: 32 }}
              >
                No API tokens created yet
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
