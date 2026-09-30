import { usePolling } from '../../hooks/useApi';
export function useLiveQueryLogController(enabled: boolean) {
  const {
    data: liveData,
    hasResult,
    loading,
    error,
  } = usePolling(
    enabled ? '/api/query-logs' : null,
    'getApiQueryLogs',
    5000,
    enabled ? { limit: '20' } : undefined,
  );
  const rows = enabled ? (liveData?.logs ?? []) : [];

  return { rows, enabled, loading, error, hasResult };
}
