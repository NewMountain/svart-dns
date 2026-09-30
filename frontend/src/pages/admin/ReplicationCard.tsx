import '../../styles/pages/admin.css';
import type { AdminController } from './useAdminController';
export function ReplicationCard({ model }: { model: AdminController }) {
  const {
    action,
    syncConfig,
    showAddPeer,
    setShowAddPeer,
    newPeerUrl,
    setNewPeerUrl,
    confirmRemovePeer,
    setConfirmRemovePeer,
    editingInterval,
    setEditingInterval,
    editIntervalValue,
    setEditIntervalValue,
    editingSecret,
    setEditingSecret,
    editSecretValue,
    setEditSecretValue,
    editingNodeName,
    setEditingNodeName,
    editNodeNameValue,
    setEditNodeNameValue,
    pairingMode,
    setPairingMode,
    pairingCode,
    setPairingCode,
    pairingSelfUrl,
    confirmPeerUrl,
    setConfirmPeerUrl,
    confirmCode,
    setConfirmCode,
    pairingStatus,
    setPairingStatus,
    isAdmin,
    formatLastUsed,
    addPeer,
    initiatePairing,
    confirmPairing,
    removePeer,
    saveInterval,
    saveSecret,
    saveNodeName,
  } = model;
  return (
    <div className="admin-card">
      <div className="admin-card-header">
        <div>
          <div className="admin-card-title">Replication</div>
          <div className="card-desc">Multi-instance sync via LWW registers.</div>
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button className="btn btn-primary" onClick={action(initiatePairing)}>
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71" />
              <path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71" />
            </svg>
            Pair
          </button>
          <button
            className="btn btn-ghost"
            onClick={() => {
              setPairingMode(pairingMode === 'confirm' ? 'none' : 'confirm');
            }}
          >
            Confirm Pair
          </button>
          <button
            className="btn btn-ghost"
            onClick={() => {
              setShowAddPeer(!showAddPeer);
            }}
          >
            Manual
          </button>
        </div>
      </div>

      {pairingMode === 'initiate' && pairingCode && (
        <div className="create-panel" style={{ borderLeft: '3px solid var(--green)' }}>
          <div style={{ marginBottom: 8, fontWeight: 600 }}>Pairing Code Generated</div>
          <div style={{ marginBottom: 8, color: 'var(--text-muted)', fontSize: '0.85rem' }}>
            Go to the remote node's Admin page, click "Confirm Pair", and enter this info:
          </div>
          <div className="admin-form-row">
            <div className="admin-input-group" style={{ flex: 2 }}>
              <div className="admin-label">This Node's URL</div>
              <input
                type="text"
                className="admin-input-field"
                readOnly
                value={pairingSelfUrl}
                onClick={(e) => {
                  e.currentTarget.select();
                  action(() => navigator.clipboard.writeText(pairingSelfUrl))();
                }}
                style={{ cursor: 'pointer', fontFamily: 'var(--font-mono)' }}
              />
            </div>
            <div className="admin-input-group" style={{ flex: 2 }}>
              <div className="admin-label">Pairing Code (expires in 10 min)</div>
              <input
                type="text"
                className="admin-input-field"
                readOnly
                value={pairingCode}
                onClick={(e) => {
                  e.currentTarget.select();
                  action(() => navigator.clipboard.writeText(pairingCode))();
                }}
                style={{
                  cursor: 'pointer',
                  fontFamily: 'var(--font-mono)',
                  letterSpacing: '0.05em',
                }}
              />
            </div>
            <button
              className="btn btn-ghost"
              onClick={() => {
                setPairingMode('none');
                setPairingCode('');
              }}
            >
              Done
            </button>
          </div>
        </div>
      )}

      {pairingMode === 'confirm' && (
        <div className="create-panel" style={{ borderLeft: '3px solid var(--blue)' }}>
          <div style={{ marginBottom: 8, fontWeight: 600 }}>Confirm Peer Pairing</div>
          <div style={{ marginBottom: 8, color: 'var(--text-muted)', fontSize: '0.85rem' }}>
            Enter the URL and pairing code from the initiating node.
          </div>
          <div className="admin-form-row">
            <div className="admin-input-group" style={{ flex: 2 }}>
              <div className="admin-label">Peer URL</div>
              <input
                type="text"
                className="admin-input-field"
                placeholder="https://192.168.1.3:3000"
                value={confirmPeerUrl}
                onChange={(e) => {
                  setConfirmPeerUrl(e.target.value);
                }}
              />
            </div>
            <div className="admin-input-group" style={{ flex: 2 }}>
              <div className="admin-label">Pairing Code</div>
              <input
                type="text"
                className="admin-input-field"
                placeholder="Paste pairing code"
                value={confirmCode}
                onChange={(e) => {
                  setConfirmCode(e.target.value);
                }}
                onKeyDown={action(
                  (e: React.KeyboardEvent<HTMLInputElement>) =>
                    e.key === 'Enter' && confirmPairing(),
                )}
                style={{ fontFamily: 'var(--font-mono)' }}
              />
            </div>
            <button className="btn btn-primary" onClick={action(confirmPairing)}>
              Confirm
            </button>
            <button
              className="btn btn-ghost"
              onClick={() => {
                setPairingMode('none');
                setConfirmPeerUrl('');
                setConfirmCode('');
                setPairingStatus('');
              }}
            >
              Cancel
            </button>
          </div>
          {pairingStatus && (
            <div
              style={{
                marginTop: 8,
                fontSize: '0.85rem',
                color: pairingStatus.includes('failed')
                  ? 'var(--red)'
                  : pairingStatus.includes('successful')
                    ? 'var(--green)'
                    : 'var(--text-muted)',
              }}
            >
              {pairingStatus}
            </div>
          )}
        </div>
      )}

      {showAddPeer && (
        <div className="create-panel">
          <div className="admin-form-row">
            <div className="admin-input-group" style={{ flex: 3 }}>
              <div className="admin-label">
                Peer URL (manual — peer must be added on both sides)
              </div>
              <input
                type="text"
                className="admin-input-field"
                placeholder="https://192.0.2.10:443"
                value={newPeerUrl}
                onChange={(e) => {
                  setNewPeerUrl(e.target.value);
                }}
                onKeyDown={action(
                  (e: React.KeyboardEvent<HTMLInputElement>) => e.key === 'Enter' && addPeer(),
                )}
              />
            </div>
            <button className="btn btn-primary" onClick={action(addPeer)}>
              Add
            </button>
            <button
              className="btn btn-ghost"
              onClick={() => {
                setShowAddPeer(false);
                setNewPeerUrl('');
              }}
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      <div className="create-panel">
        <div className="admin-form-row">
          <div className="admin-input-group" style={{ flex: 2 }}>
            <div className="admin-label">Node Name</div>
            <input
              type="text"
              className="admin-input-field"
              placeholder="e.g. svart-basement"
              value={editingNodeName ? editNodeNameValue : syncConfig?.node_name || ''}
              onFocus={() => {
                if (!editingNodeName) {
                  setEditingNodeName(true);
                  setEditNodeNameValue(syncConfig?.node_name || '');
                }
              }}
              onChange={(e) => {
                setEditNodeNameValue(e.target.value);
              }}
              onKeyDown={action(
                (e: React.KeyboardEvent<HTMLInputElement>) => e.key === 'Enter' && saveNodeName(),
              )}
            />
          </div>
          <div className="admin-input-group" style={{ flex: 1 }}>
            <div className="admin-label">Sync Interval</div>
            <input
              type="text"
              className="admin-input-field"
              placeholder="2s"
              value={editingInterval ? editIntervalValue : syncConfig?.sync_interval || ''}
              onFocus={() => {
                if (!editingInterval) {
                  setEditingInterval(true);
                  setEditIntervalValue(syncConfig?.sync_interval || '2s');
                }
              }}
              onChange={(e) => {
                setEditIntervalValue(e.target.value);
              }}
              onKeyDown={action(
                (e: React.KeyboardEvent<HTMLInputElement>) => e.key === 'Enter' && saveInterval(),
              )}
            />
          </div>
          {isAdmin && (
            <div className="admin-input-group" style={{ flex: 2 }}>
              <div className="admin-label">Shared Secret</div>
              <input
                type="password"
                className="admin-input-field"
                placeholder={
                  syncConfig?.has_secret
                    ? 'Configured — enter a new secret to rotate'
                    : 'Enter shared secret'
                }
                value={editSecretValue}
                onFocus={() => {
                  setEditingSecret(true);
                }}
                onChange={(e) => {
                  setEditSecretValue(e.target.value);
                }}
                onKeyDown={action(
                  (e: React.KeyboardEvent<HTMLInputElement>) => e.key === 'Enter' && saveSecret(),
                )}
                autoComplete="new-password"
                minLength={32}
              />
            </div>
          )}
          <div className="admin-input-group" style={{ flex: 1 }}>
            <div className="admin-label">TLS</div>
            <div style={{ padding: '10px 0' }}>
              <span
                className={`role-badge ${syncConfig?.tls_configured ? 'health-healthy' : 'health-unhealthy'}`}
              >
                {syncConfig?.tls_configured ? 'Configured' : 'Not Configured'}
              </span>
            </div>
          </div>
          {(editingNodeName || editingInterval || editingSecret) && (
            <button
              className="btn btn-primary"
              onClick={() => {
                if (editingNodeName) action(() => saveNodeName())();
                if (editingInterval) action(() => saveInterval())();
                if (editingSecret && editSecretValue) action(() => saveSecret())();
              }}
            >
              Save
            </button>
          )}
        </div>
      </div>

      <table className="data-table">
        <thead>
          <tr>
            <th>Peer</th>
            <th>Status</th>
            <th>Last Sync</th>
            <th>Changes (24h)</th>
            <th style={{ textAlign: 'right' }}>Actions</th>
          </tr>
        </thead>
        <tbody>
          {syncConfig && (syncConfig.peers ?? []).length > 0 ? (
            (syncConfig.peers ?? []).map((p) => (
              <tr key={p.url}>
                <td>
                  {p.node_name && <div style={{ fontWeight: 600 }}>{p.node_name}</div>}
                  <span
                    className="node-id"
                    style={{
                      fontSize: p.node_name ? '0.8rem' : undefined,
                      color: p.node_name ? 'var(--text-muted)' : undefined,
                    }}
                  >
                    {p.url.replace('https://', '')}
                  </span>
                </td>
                <td>
                  <span
                    className={`role-badge ${p.healthy ? 'health-healthy' : 'health-unhealthy'}`}
                  >
                    {p.healthy ? 'Healthy' : 'Unhealthy'}
                  </span>
                  {p.consecutive_errors > 0 && (
                    <span
                      className="text-sm"
                      style={{ color: 'var(--text-muted)', marginLeft: 6 }}
                      title={p.last_error || ''}
                    >
                      ({p.consecutive_errors} errors)
                    </span>
                  )}
                  {p.last_error && (
                    <div
                      className="text-sm"
                      style={{
                        color: 'var(--red)',
                        marginTop: 4,
                        fontSize: '0.75rem',
                        wordBreak: 'break-all',
                      }}
                    >
                      {p.last_error}
                    </div>
                  )}
                </td>
                <td>{formatLastUsed(p.last_sync_at)}</td>
                <td>{p.changes_24h.toLocaleString()}</td>
                <td style={{ textAlign: 'right' }}>
                  {confirmRemovePeer === p.url ? (
                    <span className="confirm-inline">
                      <span className="text-sm" style={{ color: 'var(--red)' }}>
                        Remove?
                      </span>
                      <button
                        className="btn btn-danger btn-sm"
                        onClick={action(() => removePeer(p.url))}
                      >
                        Yes
                      </button>
                      <button
                        className="btn btn-ghost btn-sm"
                        onClick={() => {
                          setConfirmRemovePeer(null);
                        }}
                      >
                        No
                      </button>
                    </span>
                  ) : (
                    <button
                      className="admin-btn-icon"
                      title="Remove peer"
                      aria-label="Remove peer"
                      onClick={() => {
                        setConfirmRemovePeer(p.url);
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
                colSpan={5}
                style={{ textAlign: 'center', color: 'var(--text-muted)', padding: 32 }}
              >
                No peers configured
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}
