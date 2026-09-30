import type { BlocklistAssignment } from '../../api/generated';
import '../../styles/pages/logs.css';
import '../../styles/pages/tiers.css';

export type TierTab = 'policies' | 'ranges' | 'groups' | 'clients';

export const tabURLName: Record<TierTab, string> = {
  ranges: 'networks',
  groups: 'groups',
  clients: 'clients',
  policies: 'bundles',
};

export const tabAliases: Record<string, TierTab> = {
  networks: 'ranges',
  ranges: 'ranges',
  groups: 'groups',
  clients: 'clients',
  bundles: 'policies',
  policies: 'policies',
};

export function tabFromURL(value: string | null): TierTab {
  return (value && tabAliases[value]) || 'ranges';
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : 'Unknown error';
}

export type ListItem = Pick<BlocklistAssignment, 'id' | 'alias' | 'domain_count' | 'is_assigned'>;
