import { useCallback, useEffect } from 'react';
import { useSearchParams } from 'react-router-dom';
import { useApi } from '../hooks/useApi';
import { TierTab, tabFromURL, tabURLName } from './assignments/navigation';

export function useTiersController() {
  const [searchParams, setSearchParams] = useSearchParams();
  const urlTab = searchParams.get('tab');
  const urlSelected = searchParams.get('selected');
  const urlSearch = searchParams.get('search');
  const tab = tabFromURL(urlTab);
  const selectedId = urlSelected;
  const search = urlSearch ?? '';
  const updateParams = useCallback(
    (updates: Record<string, string | null>) => {
      const next: Record<string, string> = {};
      const current = Object.fromEntries(searchParams.entries());
      for (const [k, v] of Object.entries({ ...current, ...updates })) {
        if (v !== null && v !== '') next[k] = v;
      }
      setSearchParams(next, { replace: true });
    },
    [searchParams, setSearchParams],
  );
  const setTab = useCallback(
    (newTab: TierTab) => {
      updateParams({ tab: tabURLName[newTab], selected: null, search: null });
    },
    [updateParams],
  );
  const setSelectedId = useCallback(
    (id: string | null) => {
      updateParams({ selected: id });
    },
    [updateParams],
  );
  const setSearch = useCallback(
    (s: string) => {
      updateParams({ search: s || null });
    },
    [updateParams],
  );
  const { data: policies, refresh: refreshPolicies } = useApi('/api/policies', 'getApiPolicies');
  const { data: ranges, refresh: refreshRanges } = useApi('/api/ranges', 'getApiRanges');
  const { data: groups, refresh: refreshGroups } = useApi('/api/groups', 'getApiGroups');
  const needClients = tab === 'groups' || tab === 'clients';
  const { data: clients } = useApi(needClients ? '/api/clients' : null, 'getApiClients');
  useEffect(() => {
    if (selectedId !== null) return;
    const first =
      tab === 'policies' && policies?.[0]
        ? String(policies[0].id)
        : tab === 'ranges' && ranges?.[0]
          ? String(ranges[0].id)
          : tab === 'groups' && groups?.[0]
            ? String(groups[0].id)
            : tab === 'clients' && clients?.[0]
              ? clients[0].ip_address
              : null;
    if (first) setSelectedId(first);
  }, [tab, policies, ranges, groups, clients, selectedId, setSelectedId]);
  return {
    tab,
    selectedId,
    search,
    setTab,
    setSelectedId,
    setSearch,
    policies,
    refreshPolicies,
    ranges,
    refreshRanges,
    groups,
    refreshGroups,
    clients,
  };
}
