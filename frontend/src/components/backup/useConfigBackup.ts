import { parseConfigExport } from '../../api/generated';
import { getApiConfigExport, postApiConfigImport } from '../../api/operations';
import { useAppState } from '../../hooks/appStateContext';
import { useUiReducer } from '../../hooks/useUiField';
import { backupReducer, initialBackupState, sameConfiguration } from '../../lib/configBackup';

function message(error: unknown): string {
  return error instanceof Error ? error.message : 'Unknown error';
}

export function useConfigBackup() {
  const { dispatch: dispatchApp } = useAppState();
  const [state, dispatch] = useUiReducer('ConfigBackup.model', backupReducer, initialBackupState);

  async function selectFile(file: File) {
    dispatch({ type: 'started', operation: 'read' });
    try {
      if (file.size > 8 * 1024 * 1024)
        throw new Error(
          'The server accepts backups up to 8 MiB. Choose a smaller complete backup; do not remove or truncate records.',
        );
      const data: unknown = JSON.parse(await file.text());
      const config = parseConfigExport(data);
      dispatch({ type: 'selected', filename: file.name, config });
    } catch (error) {
      dispatch({
        type: 'failed',
        message: `Cannot read this backup: ${message(error)}. Choose a complete Svart configuration JSON export.`,
      });
    }
  }

  async function exportConfig() {
    dispatch({ type: 'started', operation: 'export' });
    try {
      const config = await getApiConfigExport();
      const url = URL.createObjectURL(
        new Blob([JSON.stringify(config, null, 2) + '\n'], { type: 'application/json' }),
      );
      try {
        const link = document.createElement('a');
        link.href = url;
        link.download = `svart-config-${config.exported_at.replace(/:/g, '-')}.json`;
        link.click();
      } finally {
        URL.revokeObjectURL(url);
      }
      dispatch({ type: 'downloaded', exportedAt: config.exported_at });
    } catch (error) {
      dispatch({
        type: 'failed',
        message: `Export failed: ${message(error)}. Retry the download when the server is available.`,
      });
    }
  }

  async function importConfig() {
    if (!state.selection) return;
    dispatch({ type: 'started', operation: 'import' });
    try {
      const result = await postApiConfigImport(state.selection.config);
      if (!result.success) throw new Error('The server did not accept the import');
      dispatchApp({ type: 'configurationImported' });
      const persisted = await getApiConfigExport();
      if (!sameConfiguration(state.selection.config, persisted)) {
        throw new Error(
          'The server completed the merge, but its persisted configuration differs from this backup. Existing entries can be retained by import. Download the current configuration and inspect the differences before retrying',
        );
      }
      dispatch({ type: 'verified', exportedAt: persisted.exported_at });
    } catch (error) {
      dispatch({
        type: 'failed',
        message: `Import not verified: ${message(error)}. The selected file is retained.`,
      });
    }
  }

  return {
    state,
    selectFile: (file: File) => {
      void selectFile(file);
    },
    exportConfig: () => {
      void exportConfig();
    },
    importConfig: () => {
      void importConfig();
    },
  };
}
