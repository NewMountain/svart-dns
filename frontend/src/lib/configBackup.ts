import type { ConfigExport } from '../api/generated';

export type BackupSelection = { filename: string; config: ConfigExport };
export type BackupState = {
  selection: BackupSelection | null;
  progress:
    | { status: 'idle' }
    | { status: 'busy'; operation: 'import' | 'export' | 'read' }
    | { status: 'downloaded'; exportedAt: string }
    | { status: 'verified'; exportedAt: string }
    | { status: 'error'; message: string };
};
export type BackupMessage =
  | ({ type: 'selected' } & BackupSelection)
  | { type: 'started'; operation: 'import' | 'export' | 'read' }
  | { type: 'failed'; message: string }
  | { type: 'downloaded'; exportedAt: string }
  | { type: 'verified'; exportedAt: string };

export const initialBackupState: BackupState = { selection: null, progress: { status: 'idle' } };

export function backupReducer(state: BackupState, message: BackupMessage): BackupState {
  switch (message.type) {
    case 'selected':
      return {
        selection: { filename: message.filename, config: message.config },
        progress: { status: 'idle' },
      };
    case 'started':
      return { ...state, progress: { status: 'busy', operation: message.operation } };
    case 'failed':
      return { ...state, progress: { status: 'error', message: message.message } };
    case 'downloaded':
      return { ...state, progress: { status: 'downloaded', exportedAt: message.exportedAt } };
    case 'verified':
      return { ...state, progress: { status: 'verified', exportedAt: message.exportedAt } };
  }
}

function canonical(value: unknown): string {
  if (Array.isArray(value)) return `[${value.map(canonical).sort().join(',')}]`;
  if (typeof value === 'object' && value !== null) {
    return `{${Object.entries(value)
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([key, item]) => `${JSON.stringify(key)}:${canonical(item)}`)
      .join(',')}}`;
  }
  return JSON.stringify(value);
}

export function sameConfiguration(expected: ConfigExport, actual: ConfigExport): boolean {
  const { exported_at: expectedTime, ...expectedState } = expected;
  const { exported_at: actualTime, ...actualState } = actual;
  void expectedTime;
  void actualTime;
  return (
    JSON.stringify(expected.bootstrap_servers) === JSON.stringify(actual.bootstrap_servers) &&
    canonical(expectedState) === canonical(actualState)
  );
}
