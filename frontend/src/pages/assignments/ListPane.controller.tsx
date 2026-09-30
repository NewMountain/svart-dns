import { useEffect, useMemo, useRef } from 'react';
import type {
  Client,
  Group,
  PolicyView as PolicySummary,
  RangeView as Range,
} from '../../api/generated';
import { postApiGroups, postApiPolicies, postApiRanges } from '../../api/operations';
import { useAction } from '../../hooks/useAction';
import { useUiField } from '../../hooks/useUiField';
import { TierTab } from './navigation';

export function useListPaneController({
  tab,
  search,
  setSearch,
  selectedId,
  setSelectedId,
  policies,
  ranges,
  groups,
  clients,
  refreshPolicies,
  refreshRanges,
  refreshGroups,
}: {
  tab: TierTab;
  search: string;
  setSearch: (s: string) => void;
  selectedId: string | null;
  setSelectedId: (id: string | null) => void;
  policies: PolicySummary[] | null;
  ranges: Range[] | null;
  groups: Group[] | null;
  clients: Client[] | null;
  refreshPolicies: () => void;
  refreshRanges: () => void;
  refreshGroups: () => void;
}) {
  const action = useAction();
  const [showCreate, setShowCreate] = useUiField('ListPane.showCreate', false);
  const [newName, setNewName] = useUiField('ListPane.newName', '');
  const [newCidr, setNewCidr] = useUiField('ListPane.newCidr', '');
  const [creating, setCreating] = useUiField('ListPane.creating', false);
  const filteredPolicies = useMemo(
    () =>
      (policies ?? []).filter(
        (p) =>
          p.name.toLowerCase().includes(search.toLowerCase()) ||
          p.description.toLowerCase().includes(search.toLowerCase()),
      ),
    [policies, search],
  );
  const filteredRanges = useMemo(
    () =>
      (ranges ?? []).filter(
        (r) => r.name.toLowerCase().includes(search.toLowerCase()) || r.cidr.includes(search),
      ),
    [ranges, search],
  );
  const filteredGroups = useMemo(
    () => (groups ?? []).filter((g) => g.name.toLowerCase().includes(search.toLowerCase())),
    [groups, search],
  );
  const filteredClients = useMemo(
    () =>
      (clients ?? []).filter(
        (c) =>
          c.ip_address.includes(search) ||
          (c.alias && c.alias.toLowerCase().includes(search.toLowerCase())),
      ),
    [clients, search],
  );
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  useEffect(() => {
    if (!search) return;
    if (debounceRef.current) clearTimeout(debounceRef.current);
    debounceRef.current = setTimeout(() => {
      const visibleIds =
        tab === 'policies'
          ? filteredPolicies.map((p) => String(p.id))
          : tab === 'ranges'
            ? filteredRanges.map((r) => String(r.id))
            : tab === 'groups'
              ? filteredGroups.map((g) => String(g.id))
              : filteredClients.map((c) => c.ip_address);
      if (selectedId && visibleIds.includes(selectedId)) return;
      const firstVisible = visibleIds[0];
      if (firstVisible !== undefined) setSelectedId(firstVisible);
    }, 200);
    return () => {
      if (debounceRef.current) clearTimeout(debounceRef.current);
    };
  }, [
    search,
    tab,
    filteredPolicies,
    filteredRanges,
    filteredGroups,
    filteredClients,
    selectedId,
    setSelectedId,
  ]);
  async function handleCreate() {
    if (!newName.trim()) return;
    if (tab === 'ranges' && !newCidr.trim()) return;
    setCreating(true);
    try {
      if (tab === 'policies') {
        await postApiPolicies({ name: newName.trim() });
        refreshPolicies();
      } else if (tab === 'ranges') {
        await postApiRanges({ name: newName.trim(), cidr: newCidr.trim() });
        refreshRanges();
      } else {
        await postApiGroups({ name: newName.trim() });
        refreshGroups();
      }
      setNewName('');
      setNewCidr('');
      setShowCreate(false);
    } finally {
      setCreating(false);
    }
  }
  const canCreate = tab !== 'clients';
  return {
    tab,
    search,
    setSearch,
    selectedId,
    setSelectedId,
    policies,
    ranges,
    groups,
    clients,
    action,
    showCreate,
    setShowCreate,
    newName,
    setNewName,
    newCidr,
    setNewCidr,
    creating,
    filteredPolicies,
    filteredRanges,
    filteredGroups,
    filteredClients,
    handleCreate,
    canCreate,
  };
}
