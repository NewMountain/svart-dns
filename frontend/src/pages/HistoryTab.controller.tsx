import type { BlocklistView as Blocklist } from '../api/generated';
import { useApi } from '../hooks/useApi';
import { useUiField } from '../hooks/useUiField';
export function useHistoryTabController({ blocklists }: { blocklists: Blocklist[] }) {
  const { data: history } = useApi('/api/blocklists/history', 'getApiBlocklistsHistory');
  const [listFilter, setListFilter] = useUiField('HistoryTab.listFilter', null);
  const [search, setSearch] = useUiField('HistoryTab.search', '');
  const needle = search.trim().toLowerCase();
  const filteredHistory = (history ?? []).filter((entry) => {
    if (listFilter !== null && entry.blocklist_id !== listFilter) return false;
    if (!needle) return true;
    return (
      entry.blocklist_alias.toLowerCase().includes(needle) ||
      (entry.sample_added ?? []).some((d) => d.toLowerCase().includes(needle)) ||
      (entry.sample_removed ?? []).some((d) => d.toLowerCase().includes(needle))
    );
  });
  return { blocklists, history, listFilter, setListFilter, search, setSearch, filteredHistory };
}
