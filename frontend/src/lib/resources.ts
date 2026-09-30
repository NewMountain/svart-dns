import type { Decoder } from '../api/client';
import {
  parseGetApiBlocklistsData,
  parseGetApiBlocklistsHistoryData,
  parseGetApiBlocklistsHistoryHistoryIdDomainsData,
  parseGetApiBlocklistsIdDomainsData,
  parseGetApiBootstrapData,
  parseGetApiClientsData,
  parseGetApiClientsIpData,
  parseGetApiGroupsData,
  parseGetApiGroupsIdData,
  parseGetApiPeersData,
  parseGetApiPoliciesData,
  parseGetApiPoliciesIdData,
  parseGetApiQueryLogsData,
  parseGetApiRangesData,
  parseGetApiRangesIdData,
  parseGetApiRewritesData,
  parseGetApiRewritesStatsData,
  parseGetApiSettingsData,
  parseGetApiStatsBlockSourcesData,
  parseGetApiStatsLatencyData,
  parseGetApiStatsServfailsData,
  parseGetApiStatsSystemData,
  parseGetApiStatsTimeseriesVariant1Data,
  parseGetApiStatsTimeseriesVariant2Data,
  parseGetApiStatsTopClientsData,
  parseGetApiStatsTopDomainsData,
  parseGetApiStatsUpstreamUsageData,
  parseGetApiTokensData,
  parseGetApiUpstreamsData,
  parseGetApiUsersData,
} from '../api/generated';
import { apiReducer, initialApiState, type ApiMessage, type ApiState } from './apiState';

const codecs = {
  getApiBlocklists: parseGetApiBlocklistsData,
  getApiBlocklistsHistory: parseGetApiBlocklistsHistoryData,
  getApiBlocklistsHistoryHistoryIdDomains: parseGetApiBlocklistsHistoryHistoryIdDomainsData,
  getApiBlocklistsIdDomains: parseGetApiBlocklistsIdDomainsData,
  getApiBootstrap: parseGetApiBootstrapData,
  getApiClients: parseGetApiClientsData,
  getApiClientsIp: parseGetApiClientsIpData,
  getApiGroups: parseGetApiGroupsData,
  getApiGroupsId: parseGetApiGroupsIdData,
  getApiPeers: parseGetApiPeersData,
  getApiPolicies: parseGetApiPoliciesData,
  getApiPoliciesId: parseGetApiPoliciesIdData,
  getApiQueryLogs: parseGetApiQueryLogsData,
  getApiRanges: parseGetApiRangesData,
  getApiRangesId: parseGetApiRangesIdData,
  getApiRewrites: parseGetApiRewritesData,
  getApiRewritesStats: parseGetApiRewritesStatsData,
  getApiSettings: parseGetApiSettingsData,
  getApiStatsBlockSources: parseGetApiStatsBlockSourcesData,
  getApiStatsLatency: parseGetApiStatsLatencyData,
  getApiStatsServfails: parseGetApiStatsServfailsData,
  getApiStatsSystem: parseGetApiStatsSystemData,
  getApiStatsTimeseriesVariant1: parseGetApiStatsTimeseriesVariant1Data,
  getApiStatsTimeseriesVariant2: parseGetApiStatsTimeseriesVariant2Data,
  getApiStatsTopClients: parseGetApiStatsTopClientsData,
  getApiStatsTopDomains: parseGetApiStatsTopDomainsData,
  getApiStatsUpstreamUsage: parseGetApiStatsUpstreamUsageData,
  getApiTokens: parseGetApiTokensData,
  getApiUpstreams: parseGetApiUpstreamsData,
  getApiUsers: parseGetApiUsersData,
};
export type ResourceData = { [K in keyof typeof codecs]: ReturnType<(typeof codecs)[K]> };
export type ResourceKey = keyof ResourceData;
export const resourceDecoders: { [K in ResourceKey]: Decoder<ResourceData[K]> } = codecs;
export type Resources = { [K in ResourceKey]?: Record<string, ApiState<ResourceData[K]>> };

export function resourceState<K extends ResourceKey>(
  resources: Resources,
  key: K,
  instance: string,
): ApiState<ResourceData[K]> {
  return resources[key]?.[instance] ?? initialApiState<ResourceData[K]>();
}

/** Pure transition; NoInfer keeps the selected key authoritative for its payload. */
export function updateResource<K extends ResourceKey>(
  resources: Resources,
  key: K,
  instance: string,
  message: ApiMessage<ResourceData[NoInfer<K>]>,
): Resources {
  const previous = resourceState(resources, key, instance);
  const next = apiReducer(previous, message);
  if (next === previous) return resources;
  return {
    ...resources,
    [key]: {
      ...resources[key],
      [instance]: next,
    },
  };
}

export function removeResource(
  resources: Resources,
  key: ResourceKey,
  instance: string,
): Resources {
  const instances = resources[key];
  if (!instances || !(instance in instances)) return resources;
  const { [instance]: _removed, ...remaining } = instances;
  if (Object.keys(remaining).length) return { ...resources, [key]: remaining };
  const { [key]: _removedKind, ...others } = resources;
  return others;
}
