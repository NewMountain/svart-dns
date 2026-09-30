import ConfigBackup from '../components/backup/ConfigBackup';
import TopBar from '../components/TopBar';
import '../styles/pages/admin.css';
import { ReplicationCard } from './admin/ReplicationCard';
import { TokensCard } from './admin/TokensCard';
import { useAdminController } from './admin/useAdminController';
import { UsersCard } from './admin/UsersCard';
export default function Admin() {
  const model = useAdminController();
  const { revealedToken, setRevealedToken, copyToken } = model;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, overflow: 'hidden' }}>
      <TopBar title="Admin" />

      <div className="admin-container">
        {/* Token reveal banner */}
        {revealedToken && (
          <div className="token-reveal">
            <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
              <svg
                width="24"
                height="24"
                viewBox="0 0 24 24"
                fill="none"
                stroke="currentColor"
                strokeWidth="2"
              >
                <polyline points="20 6 9 17 4 12" />
              </svg>
              <div>
                <div style={{ fontWeight: 600, fontSize: '1rem' }}>
                  API Token Created: "{revealedToken.name}"
                </div>
                <div className="text-muted text-sm">
                  Please copy this token now. It will not be shown again.
                </div>
              </div>
            </div>
            <div className="reveal-key">
              {revealedToken.token}
              <button
                className="btn-icon"
                style={{ color: 'var(--green)', border: 'none' }}
                title="Copy"
                aria-label="Copy"
                onClick={copyToken}
              >
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <rect x="9" y="9" width="13" height="13" rx="2" ry="2" />
                  <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
                </svg>
              </button>
            </div>
            <button
              className="btn btn-primary"
              style={{
                backgroundColor: 'transparent',
                border: '1px solid var(--green)',
                color: 'var(--green)',
                width: 'auto',
              }}
              onClick={() => {
                setRevealedToken(null);
              }}
            >
              I have copied it
            </button>
          </div>
        )}

        <ConfigBackup />

        {/* User Management Card */}
        <UsersCard model={model} />

        {/* API Access Keys Card */}
        <TokensCard model={model} />

        {/* Replication Card */}
        <ReplicationCard model={model} />
      </div>
    </div>
  );
}
