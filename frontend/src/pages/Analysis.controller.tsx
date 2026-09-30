import { useCallback, useMemo } from 'react';
import { useSearchParams } from 'react-router-dom';
import type { EntityResult, PolicyResult, TierEvaluation } from '../api/generated';
import {
  getApiAnalysisDomains,
  postApiAnalysisMatrix,
  postApiAnalysisSimulate,
} from '../api/operations';
import { useAction } from '../hooks/useAction';
import { useUiField } from '../hooks/useUiField';
type Tab = 'matrix' | 'sim';
export function useAnalysisController() {
  const action = useAction();
  const [searchParams, setSearchParams] = useSearchParams();
  const urlTab = searchParams.get('tab');
  const tab: Tab = urlTab === 'sim' ? 'sim' : 'matrix';
  const [domains, setDomains] = useUiField(
    'Analysis.domains',
    'google.com\ntiktok.com\ndoubleclick.net\nanalytics.google.com\nfacebook.com\nreddit.com\napi.segment.io',
  );
  const [clientIp, setClientIp] = useUiField('Analysis.clientIp', '');
  const [recordType, setRecordType] = useUiField('Analysis.recordType', 'A');
  function setTab(newTab: Tab) {
    setSearchParams(newTab === 'matrix' ? {} : { tab: newTab }, { replace: true });
  }
  const [matrixResult, setMatrixResult] = useUiField('Analysis.matrixResult', null);
  const [simResult, setSimResult] = useUiField('Analysis.simResult', null);
  const [loading, setLoading] = useUiField('Analysis.loading', false);
  const [loadingSource, setLoadingSource] = useUiField('Analysis.loadingSource', null);
  const [sourceError, setSourceError] = useUiField('Analysis.sourceError', null);
  const [trafficWindow, setTrafficWindow] = useUiField('Analysis.trafficWindow', '7d');
  const [hoveredCol, setHoveredCol] = useUiField('Analysis.hoveredCol', null);
  const onColEnter = useCallback(
    (col: number) => {
      setHoveredCol(col);
    },
    [setHoveredCol],
  );
  const onColLeave = useCallback(() => {
    setHoveredCol(null);
  }, [setHoveredCol]);
  async function loadTrafficDomains() {
    if (!clientIp) return;
    setLoadingSource('client-history');
    setSourceError(null);
    try {
      const data = await getApiAnalysisDomains({
        source: 'client-history',
        client_ip: clientIp,
        window: trafficWindow,
        limit: '250',
      });
      if (data.domains?.length) {
        setDomains(data.domains.join('\n'));
      }
    } catch (error) {
      setSourceError(
        error instanceof Error ? error.message : 'Domain source unavailable. Try again.',
      );
    }
    setLoadingSource(null);
  }
  async function loadDomains(source: string, limit: number) {
    setLoadingSource(source);
    setSourceError(null);
    try {
      const data = await getApiAnalysisDomains({ source, limit: String(limit) });
      if (data.domains?.length) {
        setDomains(data.domains.join('\n'));
      }
    } catch (error) {
      setSourceError(
        error instanceof Error ? error.message : 'Domain source unavailable. Try again.',
      );
    }
    setLoadingSource(null);
  }
  function parseDomains(): string[] {
    return domains
      .split('\n')
      .map((d) => d.trim())
      .filter((d) => d.length > 0);
  }
  async function runMatrix() {
    const domainList = parseDomains();
    if (domainList.length === 0) return;
    setLoading(true);
    try {
      const data = await postApiAnalysisMatrix({
        domains: domainList,
        type: recordType,
        blocklist_ids: [],
      });
      setMatrixResult(data);
    } finally {
      setLoading(false);
    }
  }
  async function runSimulator() {
    const domainList = parseDomains();
    if (domainList.length === 0 || !clientIp) return;
    setLoading(true);
    try {
      const data = await postApiAnalysisSimulate({
        client_ip: clientIp,
        domains: domainList,
        type: recordType,
      });
      setSimResult(data);
    } finally {
      setLoading(false);
    }
  }
  const simColumns = useMemo(() => {
    if (!simResult || (simResult.results ?? []).length === 0) return null;
    const first = (simResult.results ?? [])[0];
    if (!first) return null;

    const rangeEntities = first.range_evaluation?.entities ?? [];
    const groupEntities = first.group_evaluation?.entities ?? [];
    const ipEntities = first.ip_evaluation?.entities ?? [];

    // Column counts: entities + TIER result per tier + FINAL
    const rangeCols = rangeEntities.length + (rangeEntities.length > 0 ? 1 : 0);
    const groupCols = groupEntities.length + (groupEntities.length > 0 ? 1 : 0);
    const ipCols = ipEntities.length + (ipEntities.length > 0 ? 1 : 0);

    // Build grid-template-columns: domain + range entities + range tier + group entities + group tier + ip entities + ip tier + final
    const cols: string[] = ['220px'];
    for (const _ of rangeEntities) cols.push('120px');
    if (rangeEntities.length > 0) cols.push('140px');
    for (const _ of groupEntities) cols.push('120px');
    if (groupEntities.length > 0) cols.push('140px');
    for (const _ of ipEntities) cols.push('120px');
    if (ipEntities.length > 0) cols.push('140px');
    cols.push('200px');

    return {
      rangeEntities,
      groupEntities,
      ipEntities,
      rangeCols: rangeCols || 0,
      groupCols: groupCols || 0,
      ipCols: ipCols || 0,
      gridTemplate: cols.join(' '),
      totalCols: cols.length,
    };
  }, [simResult]);
  function renderEntityBadge(result: string) {
    if (result === 'block') return <span className="badge-blk">BLK</span>;
    if (result === 'allow') return <span className="badge-ok">OK</span>;
    return <span className="badge-none">-</span>;
  }
  function renderTierBadge(result: string) {
    if (result === 'block') return <span className="badge-blk">BLOCK</span>;
    if (result === 'allow') return <span className="badge-ok">ALLOW</span>;
    return <span className="badge-none">NONE</span>;
  }
  function renderEntityDetail(entity: EntityResult) {
    if (entity.published_list) return entity.published_list.list_name;
    if (entity.custom_rule) return 'Custom Rule';
    return 'No Match';
  }
  function renderTierSource(tier: TierEvaluation) {
    if (tier.result_source) {
      const src = tier.result_source;
      const tierLabel =
        src.tier === 'range' ? 'Network' : src.tier === 'group' ? 'Group' : 'Client';
      return `${tierLabel}: ${src.name}`;
    }
    return '';
  }
  function renderFinalDecision(pr: PolicyResult) {
    const result = pr.result || 'allow';
    const badgeClass = result === 'block' ? 'badge-blk' : 'badge-ok';
    const label = result === 'block' ? 'BLOCK' : 'ALLOW';

    let tierLabel = '';
    let ruleDetail = 'Implicit Allow';
    if (pr.result_source) {
      const src = pr.result_source;
      const tierName =
        src.tier === 'range' ? '1: Network' : src.tier === 'group' ? '2: Group' : '3: Client';
      tierLabel = `Tier ${tierName}`;
      if (src.published_list)
        ruleDetail = `Rule: ${src.published_list.rule} (${src.published_list.list_name})`;
      else if (src.custom_rule) ruleDetail = `Rule: ${src.custom_rule.rule}`;
      else ruleDetail = '';
    }

    const tierColor = pr.result_source?.tier === 'ip' ? 'var(--orange)' : undefined;

    return (
      <div className="sim-cell final">
        <span className={badgeClass} style={{ fontSize: '0.85rem', marginBottom: 4 }}>
          {label}
        </span>
        {tierLabel && (
          <span
            className="cell-detail cell-winner"
            style={tierColor ? { color: tierColor } : undefined}
          >
            {tierLabel}
          </span>
        )}
        {ruleDetail && <span className="cell-detail">{ruleDetail}</span>}
      </div>
    );
  }
  return {
    action,
    tab,
    domains,
    setDomains,
    clientIp,
    setClientIp,
    recordType,
    setRecordType,
    setTab,
    matrixResult,
    simResult,
    loading,
    loadingSource,
    sourceError,
    trafficWindow,
    setTrafficWindow,
    hoveredCol,
    onColEnter,
    onColLeave,
    loadTrafficDomains,
    loadDomains,
    runMatrix,
    runSimulator,
    simColumns,
    renderEntityBadge,
    renderTierBadge,
    renderEntityDetail,
    renderTierSource,
    renderFinalDecision,
  };
}
