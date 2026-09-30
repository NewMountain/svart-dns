import type { BackupState } from '../../lib/configBackup';
import '../../styles/pages/config.css';
import { useConfigBackup } from './useConfigBackup';

export function ConfigBackupView({
  state,
  selectFile,
  exportConfig,
  importConfig,
}: {
  state: BackupState;
  selectFile: (file: File) => void;
  exportConfig: () => void;
  importConfig: () => void;
}) {
  const busy = state.progress.status === 'busy';
  return (
    <section className="config-card" aria-labelledby="configuration-backup-title">
      <div className="config-card-header">
        <div>
          <div id="configuration-backup-title" className="config-card-title">
            Configuration Backup
          </div>
          <div className="config-card-subtitle">Download or import the server configuration</div>
        </div>
      </div>
      <p className="input-hint">
        Includes upstreams, bootstrap servers, settings, rewrites, filter list definitions, bundles,
        networks, groups, clients, disabled assignments, and manual rules. Excludes accounts, API
        tokens, shared secrets, query logs, and downloaded list contents.
      </p>
      <p className="input-hint">
        Import merges with existing configuration; it does not remove entries absent from the file.
        Existing upstreams and rewrites may be retained. Verification compares the complete exported
        configuration after import with your file.
      </p>
      <div className="form-group">
        <label className="form-label" htmlFor="configuration-backup-file">
          Configuration JSON file
        </label>
        <input
          id="configuration-backup-file"
          type="file"
          accept="application/json,.json"
          className="form-input"
          disabled={busy}
          onChange={(event) => {
            const file = event.currentTarget.files?.[0];
            if (file) selectFile(file);
          }}
        />
        {state.selection && <div className="input-hint">Selected: {state.selection.filename}</div>}
      </div>
      <div className="action-bar">
        <button className="btn btn-primary" disabled={busy} onClick={exportConfig}>
          Download Configuration
        </button>
        <button
          className="btn btn-primary"
          disabled={busy || !state.selection}
          onClick={importConfig}
        >
          Import Configuration
        </button>
      </div>
      {state.progress.status === 'busy' && (
        <p role="status" className="input-hint">
          {state.progress.operation === 'import'
            ? 'Importing and verifying persisted configuration…'
            : state.progress.operation === 'read'
              ? 'Reading the complete backup…'
              : 'Preparing configuration download…'}
        </p>
      )}
      {state.progress.status === 'error' && (
        <p role="alert" className="input-hint" style={{ marginTop: 16, color: 'var(--red)' }}>
          {state.progress.message}
        </p>
      )}
      {state.progress.status === 'downloaded' && (
        <p role="status" className="input-hint">
          Configuration download prepared from server export {state.progress.exportedAt}.
        </p>
      )}
      {state.progress.status === 'verified' && (
        <p role="status" className="input-hint" style={{ marginTop: 16, color: 'var(--green)' }}>
          Import verified: the persisted configuration matches your backup. Read back at{' '}
          {state.progress.exportedAt}.
        </p>
      )}
    </section>
  );
}

export default function ConfigBackup() {
  return <ConfigBackupView {...useConfigBackup()} />;
}
