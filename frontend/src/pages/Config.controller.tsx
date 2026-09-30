import { postApiCacheClear, putApiSettingsKey } from '../api/operations';
import { useAction } from '../hooks/useAction';
import { useApi } from '../hooks/useApi';
import { useUiField } from '../hooks/useUiField';
import { errorMessage } from '../lib/errors';

export function useConfigController() {
  const action = useAction();
  const { data: settings, reload } = useApi('/api/settings', 'getApiSettings');
  const [draft, setDraft] = useUiField('Config.draft', { revision: 0, changes: {} });
  const local = {
    ...settings,
    ...Object.fromEntries(Object.entries(draft.changes).map(([key, edit]) => [key, edit.value])),
  };
  const [saving, setSaving] = useUiField('Config.saving', false);
  const [message, setMessage] = useUiField('Config.message', '');
  function updateLocal(key: string, value: string) {
    setDraft((prev) => ({
      revision: prev.revision + 1,
      changes: { ...prev.changes, [key]: { value, revision: prev.revision + 1 } },
    }));
  }
  async function saveAll() {
    const submitted = draft.changes;
    setSaving(true);
    setMessage('');
    try {
      const keys = [
        'timezone',
        'cache_ttl',
        'denied_ttl',
        'strategy',
        'bootstrap_ttl',
        'logging_enabled',
        'log_retention_days',
      ];
      for (const key of keys) {
        if (local[key] !== settings?.[key]) {
          await putApiSettingsKey(key, { value: local[key] });
        }
      }
      // This read follows this save's writes. An unrelated or earlier resource
      // refresh cannot acknowledge a newer edit that merely has the same value.
      const confirmed = await reload();
      for (const [key, edit] of Object.entries(submitted)) {
        if (confirmed?.[key] !== edit.value) {
          throw new Error(`Saved value for ${key} could not be verified; retry the retained draft`);
        }
      }
      setDraft((previous) => ({
        revision: previous.revision,
        changes: Object.fromEntries(
          Object.entries(previous.changes).filter(
            ([key, edit]) => submitted[key]?.revision !== edit.revision,
          ),
        ),
      }));
      setMessage('Submitted changes saved');
    } catch (err) {
      setMessage(`Error: ${errorMessage(err)}`);
    } finally {
      setSaving(false);
    }
  }
  async function clearCache() {
    try {
      await postApiCacheClear();
      setMessage('Cache cleared');
    } catch (err) {
      setMessage(`Error: ${errorMessage(err)}`);
    }
  }
  return { action, settings, local, saving, message, updateLocal, saveAll, clearCache };
}
