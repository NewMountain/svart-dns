import { useCallback, useEffect, useRef } from 'react';
import { useSearchParams } from 'react-router-dom';
import type { QueryLogView as LogEntry, TierEvaluation as TierEval } from '../api/generated';
import {
  getApiQueryLogs,
  getApiQueryLogsId,
  postApiClientsIpAllowDomain,
  postApiClientsIpBlockDomain,
} from '../api/operations';
import Badge from '../components/Badge';
import { useAction } from '../hooks/useAction';
import { useApi } from '../hooks/useApi';
import { useUiField } from '../hooks/useUiField';

export function useLogsController() {
  const action = useAction();
  const [searchParams, setSearchParams] = useSearchParams();
  function initParam(key: string, fallback: string): string {
    return searchParams.get(key) ?? fallback;
  }
  const [refreshInterval, setRefreshInterval] = useUiField('Logs.refreshInterval', 30000);
  const search = initParam('domain', '');
  const clientFilter = initParam('client', '');
  const groupFilter = initParam('group', '');
  const rangeFilter = initParam('range', '');
  const resultFilter = initParam('result', '');
  const typeFilter = initParam('type', '');
  const limit = Number(initParam('limit', '100')) || 100;
  const filterKey = JSON.stringify([
    search,
    clientFilter,
    groupFilter,
    rangeFilter,
    resultFilter,
    typeFilter,
    limit,
  ]);
  const [page, setPage] = useUiField('Logs.page', { filterKey, offset: 0 });
  const offset = page.filterKey === filterKey ? page.offset : 0;
  function setOffset(next: number) {
    setPage({ filterKey, offset: next });
  }
  function updateParams(updates: Record<string, string | null>) {
    const next: Record<string, string> = {};
    const current = Object.fromEntries(searchParams.entries());
    for (const [k, v] of Object.entries({ ...current, ...updates })) {
      if (v !== null && v !== '') next[k] = v;
    }
    setSearchParams(next, { replace: true });
  }
  function setSearch(s: string) {
    updateParams({ domain: s || null });
  }
  function setClientFilter(v: string) {
    updateParams({ client: v || null });
  }
  function setGroupFilter(v: string) {
    updateParams({ group: v || null });
  }
  function setRangeFilter(v: string) {
    updateParams({ range: v || null });
  }
  function setResultFilter(v: string) {
    updateParams({ result: v || null });
  }
  function setTypeFilter(v: string) {
    updateParams({ type: v || null });
  }
  function setLimit(n: number) {
    setOffset(0);
    updateParams({ limit: n !== 100 ? String(n) : null });
  }
  const [logs, setLogs] = useUiField('Logs.logs', []);
  const [total, setTotal] = useUiField('Logs.total', 0);
  const [loading, setLoading] = useUiField('Logs.loading', true);
  const [error, setError] = useUiField('Logs.error', null);
  const pollingRef = useRef<number | null>(null);
  const [expandedId, setExpandedId] = useUiField('Logs.expandedId', null);
  const [evalDetail, setEvalDetail] = useUiField('Logs.evalDetail', null);
  const [evalLoading, setEvalLoading] = useUiField('Logs.evalLoading', false);
  const [contextMenu, setContextMenu] = useUiField('Logs.contextMenu', null);
  const { data: clients } = useApi('/api/clients', 'getApiClients');
  const { data: groups } = useApi('/api/groups', 'getApiGroups');
  const { data: ranges } = useApi('/api/ranges', 'getApiRanges');
  const buildParams = useCallback(() => {
    const params: Record<string, string> = { limit: String(limit), offset: String(offset) };
    if (search) params['domain'] = search;
    if (clientFilter) params['client_ip'] = clientFilter;
    if (groupFilter) params['group_id'] = groupFilter;
    if (rangeFilter) params['range_id'] = rangeFilter;
    if (resultFilter === 'blocked') params['blocked'] = 'true';
    if (resultFilter === 'allowed') params['blocked'] = 'false';
    if (resultFilter === 'rewrite') params['result_reason'] = 'rewrite';
    if (typeFilter) params['query_type'] = typeFilter;
    return params;
  }, [limit, offset, search, clientFilter, groupFilter, rangeFilter, resultFilter, typeFilter]);
  const fetchLogs = useCallback(async () => {
    try {
      const data = await getApiQueryLogs(buildParams());
      setError(null);
      setLogs(data.logs ?? []);
      setTotal(data.total);
      setLoading(false);
    } catch {
      setError('Could not load query logs. Check the connection and try again.');
      setLoading(false);
    }
  }, [buildParams, setError, setLoading, setLogs, setTotal]);
  useEffect(() => {
    action(() => fetchLogs())();
    pollingRef.current = window.setInterval(fetchLogs, refreshInterval);
    return () => {
      if (pollingRef.current) clearInterval(pollingRef.current);
    };
  }, [fetchLogs, refreshInterval, action]);
  async function toggleExpand(entryId: number) {
    if (expandedId === entryId) {
      setExpandedId(null);
      setEvalDetail(null);
      return;
    }
    setExpandedId(entryId);
    setEvalDetail(null);
    setEvalLoading(true);
    try {
      const detail = await getApiQueryLogsId(entryId);
      setEvalDetail(detail);
    } catch {
      setEvalDetail(null);
    }
    setEvalLoading(false);
  }
  function renderTierBlock(label: string, tier: TierEval | undefined, isWinner: boolean) {
    if (!tier || (tier.entities ?? []).length === 0) {
      return (
        <div className={`eval-tier${isWinner ? ' eval-winner' : ''}`}>
          <div className="eval-tier-header">
            <span className="eval-tier-label">{label}</span>
            <span className="eval-no-match">no match</span>
          </div>
        </div>
      );
    }
    return (
      <div className={`eval-tier${isWinner ? ' eval-winner' : ''}`}>
        <div className="eval-tier-header">
          <span className="eval-tier-label">{label}</span>
          {tier.result && (
            <Badge variant={tier.result === 'block' ? 'block' : 'allow'}>
              {tier.result.toUpperCase()}
            </Badge>
          )}
          {isWinner && <span className="eval-winner-tag">WINNER</span>}
        </div>
        {(tier.entities ?? []).map((ent, i) => (
          <div className="eval-entity" key={i}>
            <span className="eval-entity-name">{ent.name || '(unnamed)'}</span>
            {ent.result ? (
              <span className={`eval-entity-result eval-${ent.result}`}>{ent.result}</span>
            ) : (
              <span className="eval-entity-result eval-none">no decision</span>
            )}
            {ent.published_list && (
              <span className="eval-match">
                {ent.published_list.list_name} matched <code>{ent.published_list.rule}</code>
              </span>
            )}
            {ent.custom_rule && (
              <span className="eval-match">
                custom {ent.custom_rule.action}: <code>{ent.custom_rule.rule}</code>
              </span>
            )}
          </div>
        ))}
      </div>
    );
  }
  function openContextMenu(e: React.MouseEvent, entry: LogEntry) {
    e.preventDefault();
    e.stopPropagation();
    setContextMenu({ x: e.clientX, y: e.clientY, entry });
  }
  async function allowDomain(domain: string, clientIp: string) {
    await postApiClientsIpAllowDomain(clientIp, { domain });
  }
  async function blockDomain(domain: string, clientIp: string) {
    await postApiClientsIpBlockDomain(clientIp, { domain });
  }
  const COL_COUNT = 11;
  const hasFilters = Boolean(
    search || clientFilter || groupFilter || rangeFilter || resultFilter || typeFilter,
  );
  return {
    action,
    refreshInterval,
    setRefreshInterval,
    search,
    clientFilter,
    groupFilter,
    rangeFilter,
    resultFilter,
    typeFilter,
    limit,
    page,
    offset,
    setOffset,
    setSearch,
    setClientFilter,
    setGroupFilter,
    setRangeFilter,
    setResultFilter,
    setTypeFilter,
    setLimit,
    logs,
    total,
    loading,
    error,
    expandedId,
    evalDetail,
    evalLoading,
    contextMenu,
    setContextMenu,
    clients,
    groups,
    ranges,
    fetchLogs,
    toggleExpand,
    renderTierBlock,
    openContextMenu,
    allowDomain,
    blockDomain,
    COL_COUNT,
    hasFilters,
  };
}
