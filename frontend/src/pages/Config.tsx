import ConfigBackup from '../components/backup/ConfigBackup';
import Toggle from '../components/Toggle';
import TopBar from '../components/TopBar';
import { useBootstrapCardController } from './BootstrapCard.controller';
import { useConfigController } from './Config.controller';
import { useUpstreamsCardController } from './UpstreamsCard.controller';

import '../styles/pages/config.css';

export default function Config() {
  const model = useConfigController();
  return <ConfigView model={model} />;
}

function UpstreamsCard() {
  const model = useUpstreamsCardController();
  return <UpstreamsCardView model={model} />;
}

function BootstrapCard() {
  const model = useBootstrapCardController();
  return <BootstrapCardView model={model} />;
}

// Every IANA zone the browser knows, plus the saved value if it is not among
// them, so the current setting is never silently shown as something else.
function timeZoneOptions(current: string | undefined): string[] {
  const zones =
    typeof Intl.supportedValuesOf === 'function' ? Intl.supportedValuesOf('timeZone') : [];
  const all = new Set(['UTC', ...zones]);
  if (current) all.add(current);
  return [...all];
}

function ConfigView({ model }: { model: NonNullable<ReturnType<typeof useConfigController>> }) {
  const { action, local, saving, message, updateLocal, saveAll, clearCache } = model;
  return (
    <div className="config-page">
      <TopBar title="System Configuration" />

      <div className="config-container">
        {/* Upstream DNS Servers */}
        <UpstreamsCard />

        {/* Bootstrap DNS Servers */}
        <BootstrapCard />

        {/* General Preferences */}
        <div className="config-card">
          <div className="config-card-header">
            <div>
              <div className="config-card-title">General Preferences</div>
              <div className="config-card-subtitle">Display settings and privacy controls</div>
            </div>
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--blue)"
              strokeWidth="2"
            >
              <circle cx="12" cy="12" r="3" />
              <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1 0 2.83 2 2 0 0 1-2.83 0l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-2 2 2 2 0 0 1-2-2v-.09A1.65 1.65 0 0 0 9 19.4a1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83 0 2 2 0 0 1 0-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1-2-2 2 2 0 0 1 2-2h.09A1.65 1.65 0 0 0 4.6 9a1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 0-2.83 2 2 0 0 1 2.83 0l.06.06A1.65 1.65 0 0 0 9 4.68a1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 2-2 2 2 0 0 1 2 2v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 0 2 2 0 0 1 0 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 2 2 2 2 0 0 1-2 2h-.09a1.65 1.65 0 0 0-1.51 1z" />
            </svg>
          </div>

          <div className="form-grid">
            <div className="form-group">
              <label className="form-label">Timezone</label>
              <select
                className="form-input"
                value={local['timezone'] ?? 'UTC'}
                onChange={(e) => {
                  updateLocal('timezone', e.target.value);
                }}
              >
                {timeZoneOptions(local['timezone']).map((tz) => (
                  <option key={tz} value={tz}>
                    {tz}
                  </option>
                ))}
              </select>
              <div className="input-hint">Used for logs and charts.</div>
            </div>
          </div>
        </div>

        {/* DNS Core */}
        <div className="config-card">
          <div className="config-card-header">
            <div>
              <div className="config-card-title">DNS Core</div>
              <div className="config-card-subtitle">Performance tuning and behavior</div>
            </div>
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--orange)"
              strokeWidth="2"
            >
              <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
            </svg>
          </div>

          <div className="form-grid">
            <div className="form-group">
              <label className="form-label">Cache TTL (Seconds)</label>
              <input
                type="number"
                className="form-input"
                value={local['cache_ttl'] ?? '3600'}
                onChange={(e) => {
                  updateLocal('cache_ttl', e.target.value);
                }}
              />
              <div className="input-hint">Max time to hold records in RAM (Default: 3600)</div>
            </div>

            <div className="form-group">
              <label className="form-label">Denied Response TTL (Seconds)</label>
              <input
                type="number"
                className="form-input"
                value={local['denied_ttl'] ?? '3600'}
                onChange={(e) => {
                  updateLocal('denied_ttl', e.target.value);
                }}
              />
              <div className="input-hint">
                TTL for blocked/denied NXDOMAIN responses (Default: 3600)
              </div>
            </div>

            <div className="form-group">
              <label className="form-label">Upstream Strategy</label>
              <select
                className="form-input"
                value={local['strategy'] ?? 'weighted'}
                onChange={(e) => {
                  updateLocal('strategy', e.target.value);
                }}
              >
                <option value="weighted">Weighted Random (Performance)</option>
                <option value="blended">Blended (Performance + Privacy)</option>
                <option value="random">Pure Random (Privacy)</option>
              </select>
              <div className="input-hint">Blended = 50/50 weighted and random</div>
            </div>

            <div className="form-group">
              <label className="form-label">Bootstrap TTL</label>
              <input
                type="number"
                className="form-input"
                value={local['bootstrap_ttl'] ?? '86400'}
                onChange={(e) => {
                  updateLocal('bootstrap_ttl', e.target.value);
                }}
              />
            </div>

            <div className="toggle-row">
              <div className="toggle-label-group">
                <span className="form-label" style={{ color: 'var(--text-main)' }}>
                  Enable Query Logging
                </span>
                <span className="input-hint">Write all DNS queries to local DB</span>
              </div>
              <Toggle
                checked={local['logging_enabled'] !== 'false'}
                onChange={(v) => {
                  updateLocal('logging_enabled', v ? 'true' : 'false');
                }}
                size="lg"
              />
            </div>
          </div>
        </div>

        {/* Retention & Archival */}
        <div className="config-card">
          <div className="config-card-header">
            <div>
              <div className="config-card-title">Retention & Archival</div>
              <div className="config-card-subtitle">Long-term storage policy (Parquet)</div>
            </div>
            <svg
              width="20"
              height="20"
              viewBox="0 0 24 24"
              fill="none"
              stroke="var(--purple)"
              strokeWidth="2"
            >
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <polyline points="7 10 12 15 17 10" />
              <line x1="12" y1="15" x2="12" y2="3" />
            </svg>
          </div>

          <div className="form-grid">
            <div className="form-group">
              <label className="form-label">Log Retention (Days)</label>
              <input
                type="number"
                className="form-input"
                value={local['log_retention_days'] ?? '730'}
                onChange={(e) => {
                  updateLocal('log_retention_days', e.target.value);
                }}
              />
              <div className="input-hint">Logs older than this are deleted after archival</div>
            </div>

            <div className="form-group">
              <label className="form-label">Archive Path</label>
              <div style={{ display: 'flex', alignItems: 'center', position: 'relative' }}>
                <input
                  type="text"
                  className="form-input"
                  value={local['archive_path'] ?? '/mnt/data/archives'}
                  disabled
                  style={{ width: '100%', paddingRight: 30 }}
                />
                <svg
                  style={{ position: 'absolute', right: 10, opacity: 0.5 }}
                  width="14"
                  height="14"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <rect x="3" y="11" width="18" height="11" rx="2" ry="2" />
                  <path d="M7 11V7a5 5 0 0 1 10 0v4" />
                </svg>
              </div>
              <div className="input-hint">Set via ARCHIVE_PATH env var</div>
            </div>
          </div>
        </div>

        <ConfigBackup />

        {/* Action Bar */}
        <div className="action-bar">
          <button className="btn btn-danger" onClick={action(clearCache)}>
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path d="M3 6h18" />
              <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
            </svg>
            Clear Cache
          </button>
          <button className="btn btn-primary" onClick={action(saveAll)} disabled={saving}>
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z" />
              <polyline points="17 21 17 13 7 13 7 21" />
              <polyline points="7 3 7 8 15 8" />
            </svg>
            {saving ? 'Saving...' : 'Save Changes'}
          </button>
          {message && (
            <span style={{ fontSize: '0.8rem', color: 'var(--green)', marginLeft: 8 }}>
              {message}
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

function UpstreamsCardView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useUpstreamsCardController>>;
}) {
  const {
    action,
    upstreams,
    newUpstream,
    setNewUpstream,
    addUpstream,
    toggleUpstream,
    deleteUpstream,
    protocolBadge,
  } = model;
  return (
    <div className="config-card">
      <div className="config-card-header">
        <div>
          <div className="config-card-title">Upstream DNS Servers</div>
          <div className="config-card-subtitle">Where non-blocked queries get forwarded</div>
        </div>
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="var(--green)"
          strokeWidth="2"
        >
          <circle cx="12" cy="12" r="10" />
          <line x1="2" y1="12" x2="22" y2="12" />
          <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
        </svg>
      </div>

      <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        <input
          type="text"
          className="form-input"
          style={{ flex: 1 }}
          placeholder="9.9.9.9:53 or https://dns.quad9.net/dns-query"
          value={newUpstream}
          onChange={(e) => {
            setNewUpstream(e.target.value);
          }}
          onKeyDown={action(
            (e: React.KeyboardEvent<HTMLInputElement>) => e.key === 'Enter' && addUpstream(),
          )}
        />
        <button className="btn btn-primary" onClick={action(addUpstream)}>
          Add
        </button>
      </div>

      <div className="upstream-list">
        {(upstreams ?? []).map((u) => (
          <div className={`upstream-row${!u.enabled ? ' disabled' : ''}`} key={u.id}>
            <div className="upstream-status">
              <div
                className={`status-indicator ${u.enabled ? 'status-active' : 'status-disabled'}`}
              />
            </div>
            {protocolBadge(u.upstream)}
            <div className="upstream-addr">{u.upstream}</div>
            <div className="cell-actions">
              <button
                className="btn-icon"
                title={u.enabled ? 'Disable' : 'Enable'}
                aria-label={u.enabled ? 'Disable upstream' : 'Enable upstream'}
                onClick={action(() => toggleUpstream(u.id))}
              >
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  {u.enabled ? (
                    <>
                      <path d="M18.36 6.64a9 9 0 0 1 .02 12.73" />
                      <path d="M5.64 6.64a9 9 0 0 0-.02 12.73" />
                      <circle cx="12" cy="12" r="2" />
                    </>
                  ) : (
                    <>
                      <path d="M1 1l22 22" />
                      <path d="M16.72 11.06A10.94 10.94 0 0 1 19 12.55" />
                      <path d="M5 12.55a10.94 10.94 0 0 1 5.17-2.39" />
                    </>
                  )}
                </svg>
              </button>
              <button
                className="btn-icon"
                style={{ color: 'var(--red)' }}
                title="Remove"
                aria-label="Remove upstream"
                onClick={action(() => deleteUpstream(u.id))}
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
            </div>
          </div>
        ))}
        {(upstreams ?? []).length === 0 && (
          <div
            style={{
              padding: 24,
              textAlign: 'center',
              color: 'var(--text-muted)',
              fontSize: '0.85rem',
            }}
          >
            No upstreams configured — DNS queries will fail
          </div>
        )}
      </div>
    </div>
  );
}

function BootstrapCardView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useBootstrapCardController>>;
}) {
  const { action, servers, newServer, setNewServer, hasDoH, noBootstrap, addServer, removeServer } =
    model;
  return (
    <div className="config-card">
      <div className="config-card-header">
        <div>
          <div className="config-card-title">Bootstrap DNS</div>
          <div className="config-card-subtitle">
            Plain-IP DNS servers used to resolve DoH/DoT upstream hostnames
          </div>
        </div>
        <svg
          width="20"
          height="20"
          viewBox="0 0 24 24"
          fill="none"
          stroke="var(--yellow)"
          strokeWidth="2"
        >
          <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" />
        </svg>
      </div>

      {hasDoH && noBootstrap && (
        <div className="bootstrap-warning">
          <svg
            width="16"
            height="16"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--yellow)"
            strokeWidth="2"
          >
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z" />
            <line x1="12" y1="9" x2="12" y2="13" />
            <line x1="12" y1="17" x2="12.01" y2="17" />
          </svg>
          <span>DoH/DoT upstreams require bootstrap servers to avoid circular DNS resolution</span>
        </div>
      )}

      <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        <input
          type="text"
          className="form-input"
          style={{ flex: 1 }}
          placeholder="9.9.9.9 or 1.1.1.1:53"
          value={newServer}
          onChange={(e) => {
            setNewServer(e.target.value);
          }}
          onKeyDown={action(
            (e: React.KeyboardEvent<HTMLInputElement>) => e.key === 'Enter' && addServer(),
          )}
        />
        <button className="btn btn-primary" onClick={action(addServer)}>
          Add
        </button>
      </div>

      <div className="upstream-list">
        {(servers ?? []).map((s) => (
          <div className="upstream-row" key={s.id}>
            <div className="upstream-status">
              <div className="status-indicator status-active" />
            </div>
            <span className="upstream-proto proto-udp">UDP</span>
            <div className="upstream-addr">{s.server}</div>
            <div className="cell-actions">
              <button
                className="btn-icon"
                style={{ color: 'var(--red)' }}
                title="Remove"
                aria-label="Remove server"
                onClick={action(() => removeServer(s.id))}
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
            </div>
          </div>
        ))}
        {noBootstrap && (
          <div
            style={{
              padding: 24,
              textAlign: 'center',
              color: 'var(--text-muted)',
              fontSize: '0.85rem',
            }}
          >
            No bootstrap servers — DoH/DoT hostnames will use system resolver
          </div>
        )}
      </div>
    </div>
  );
}
