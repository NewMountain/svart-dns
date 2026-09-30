import { useEffect, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useApi } from '../hooks/useApi';
import { useUiField } from '../hooks/useUiField';
import { validWindows } from './Dashboard.shared';
type TimeWindow = '5m' | '1h' | '24h' | '7d';
export function useDashboardController() {
  const [searchParams, setSearchParams] = useSearchParams();
  const urlWindow = searchParams.get('window');
  const timeWindow = validWindows.find((value) => value === urlWindow) ?? '24h';
  const [refreshTick, setRefreshTick] = useUiField('Dashboard.refreshTick', 0);
  const [animateCharts, setAnimateCharts] = useUiField('Dashboard.animateCharts', true);
  const [observedLowerSections, setShowLowerSections] = useUiField(
    'Dashboard.observedLowerSections',
    false,
  );
  const showLowerSections = observedLowerSections || typeof IntersectionObserver === 'undefined';
  const [lowerSectionsGateNode, setLowerSectionsGateNode] = useState<HTMLDivElement | null>(null);
  useEffect(() => {
    const id = setInterval(() => {
      setRefreshTick((tick) => tick + 1);
    }, 30000);
    return () => {
      clearInterval(id);
    };
  }, [setRefreshTick]);
  useEffect(() => {
    const id = window.setTimeout(() => {
      setAnimateCharts(false);
    }, 0);
    return () => {
      window.clearTimeout(id);
    };
  }, [setAnimateCharts]);
  useEffect(() => {
    if (showLowerSections) {
      return undefined;
    }

    if (!lowerSectionsGateNode) {
      return undefined;
    }

    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry?.isIntersecting) {
          setShowLowerSections(true);
          observer.disconnect();
        }
      },
      { rootMargin: '320px 0px' },
    );

    observer.observe(lowerSectionsGateNode);
    return () => {
      observer.disconnect();
    };
  }, [showLowerSections, lowerSectionsGateNode, setShowLowerSections]);
  const windowParams = useMemo(
    () => ({
      window: timeWindow,
    }),
    [timeWindow],
  );
  const {
    data: summary,
    hasResult: summaryHasResult,
    loading: summaryLoading,
    error: summaryError,
  } = useApi(
    '/api/stats/timeseries',
    'getApiStatsTimeseriesVariant1',
    { ...windowParams, buckets: '1' },
    [refreshTick],
  );
  const {
    data: timeseriesData,
    hasResult: timeseriesHasResult,
    loading: timeseriesLoading,
    error: timeseriesError,
  } = useApi(
    '/api/stats/timeseries',
    'getApiStatsTimeseriesVariant2',
    { ...windowParams, buckets: '24' },
    [refreshTick],
  );
  const {
    data: latencyData,
    hasResult: latencyHasResult,
    loading: latencyLoading,
    error: latencyError,
  } = useApi('/api/stats/latency', 'getApiStatsLatency', { ...windowParams, buckets: '24' }, [
    refreshTick,
  ]);
  const {
    data: topClientsData,
    hasResult: topClientsHasResult,
    loading: topClientsLoading,
    error: topClientsError,
  } = useApi(
    showLowerSections ? '/api/stats/top-clients' : null,
    'getApiStatsTopClients',
    { ...windowParams, limit: '8' },
    [refreshTick, showLowerSections],
  );
  const {
    data: blockSourcesData,
    hasResult: blockSourcesHasResult,
    loading: blockSourcesLoading,
    error: blockSourcesError,
  } = useApi('/api/stats/block-sources', 'getApiStatsBlockSources', windowParams, [refreshTick]);
  const {
    data: upstreamUsageData,
    hasResult: upstreamHasResult,
    loading: upstreamLoading,
    error: upstreamError,
  } = useApi('/api/stats/upstream-usage', 'getApiStatsUpstreamUsage', windowParams, [refreshTick]);
  const {
    data: topPermitted,
    hasResult: topPermittedHasResult,
    loading: topPermittedLoading,
    error: topPermittedError,
  } = useApi(
    showLowerSections ? '/api/stats/top-domains' : null,
    'getApiStatsTopDomains',
    { ...windowParams, blocked: 'false' },
    [refreshTick, showLowerSections],
  );
  const {
    data: topBlocked,
    hasResult: topBlockedHasResult,
    loading: topBlockedLoading,
    error: topBlockedError,
  } = useApi(
    showLowerSections ? '/api/stats/top-domains' : null,
    'getApiStatsTopDomains',
    { ...windowParams, blocked: 'true' },
    [refreshTick, showLowerSections],
  );
  const {
    data: servfailsData,
    hasResult: servfailsHasResult,
    loading: servfailsLoading,
    error: servfailsError,
  } = useApi(
    showLowerSections ? '/api/stats/servfails' : null,
    'getApiStatsServfails',
    windowParams,
    [refreshTick, showLowerSections],
  );
  const {
    data: systemData,
    hasResult: systemHasResult,
    loading: systemLoading,
    error: systemError,
  } = useApi(showLowerSections ? '/api/stats/system' : null, 'getApiStatsSystem', undefined, [
    refreshTick,
    showLowerSections,
  ]);
  const timeseries = timeseriesData ?? [];
  const latency = latencyData ?? [];
  const topClients = topClientsData ?? [];
  const blockSources = blockSourcesData ?? [];
  const upstreamUsage = upstreamUsageData ?? [];
  const servfails = servfailsData ?? [];
  const system = systemData ?? [];
  function setTimeWindow(windowValue: TimeWindow) {
    setSearchParams({ window: windowValue });
  }
  return {
    timeWindow,
    animateCharts,
    showLowerSections,
    setLowerSectionsGateNode,
    summary,
    summaryStatus: { hasResult: summaryHasResult, loading: summaryLoading, error: summaryError },
    timeseriesStatus: {
      hasResult: timeseriesHasResult,
      loading: timeseriesLoading,
      error: timeseriesError,
    },
    latencyStatus: {
      hasResult: latencyHasResult,
      loading: latencyLoading,
      error: latencyError,
    },
    topClientsStatus: {
      hasResult: topClientsHasResult,
      loading: !showLowerSections || topClientsLoading,
      error: topClientsError,
    },
    blockSourcesStatus: {
      hasResult: blockSourcesHasResult,
      loading: blockSourcesLoading,
      error: blockSourcesError,
    },
    upstreamStatus: {
      hasResult: upstreamHasResult,
      loading: upstreamLoading,
      error: upstreamError,
    },
    topPermitted,
    topPermittedStatus: {
      hasResult: topPermittedHasResult,
      loading: !showLowerSections || topPermittedLoading,
      error: topPermittedError,
    },
    topBlocked,
    topBlockedStatus: {
      hasResult: topBlockedHasResult,
      loading: !showLowerSections || topBlockedLoading,
      error: topBlockedError,
    },
    servfailsStatus: {
      hasResult: servfailsHasResult,
      loading: !showLowerSections || servfailsLoading,
      error: servfailsError,
    },
    systemStatus: {
      hasResult: systemHasResult,
      loading: !showLowerSections || systemLoading,
      error: systemError,
    },
    timeseries,
    latency,
    topClients,
    blockSources,
    upstreamUsage,
    servfails,
    system,
    setTimeWindow,
  };
}
