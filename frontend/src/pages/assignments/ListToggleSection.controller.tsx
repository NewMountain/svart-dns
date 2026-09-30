import type { BlocklistUniqueStats as BlocklistStats } from '../../api/generated';
import { useUiField } from '../../hooks/useUiField';
import { ListItem } from './navigation';

export function useListToggleSectionController({
  title,
  items,
  stats,
  onToggle,
  lockedIds,
  policyName,
}: {
  title: string;
  items: ListItem[];
  stats?: BlocklistStats;
  onToggle: (id: number, assigned: boolean) => void;
  lockedIds?: Set<number>;
  policyName?: string;
}) {
  const [collapsed, setCollapsed] = useUiField('ListToggleSection.collapsed', false);
  const assigned = items.filter((i) => i.is_assigned || lockedIds?.has(i.id));
  const rawTotal = assigned.reduce((sum, i) => sum + i.domain_count, 0);
  const newMap: Record<number, number> = {};
  if (stats?.lists) {
    for (const l of stats.lists) newMap[l.id] = l.unique_to_set;
  }
  return {
    title,
    items,
    stats,
    onToggle,
    lockedIds,
    policyName,
    collapsed,
    setCollapsed,
    assigned,
    rawTotal,
    newMap,
  };
}
