import type { BlocklistView as Blocklist } from '../api/generated';
import { useApi } from '../hooks/useApi';
import { useUiField } from '../hooks/useUiField';
export function useDetailsTabController({
  blocklists,
  selectedList,
  setSelectedList,
  detailSearch,
  setDetailSearch,
}: {
  blocklists: Blocklist[];
  selectedList: number | null;
  setSelectedList: (id: number | null) => void;
  detailSearch: string;
  setDetailSearch: (s: string) => void;
}) {
  const [selectedCheckpoint, setSelectedCheckpoint] = useUiField(
    'DetailsTab.selectedCheckpoint',
    null,
  );
  const { data: history } = useApi('/api/blocklists/history', 'getApiBlocklistsHistory');
  const listHistory = history?.filter((h) => h.blocklist_id === selectedList) ?? [];
  const apiPath = selectedList
    ? selectedCheckpoint
      ? `/api/blocklists/history/${String(selectedCheckpoint)}/domains`
      : `/api/blocklists/${String(selectedList)}/domains`
    : null;
  const params = selectedList
    ? { limit: '200', offset: '0', ...(detailSearch ? { search: detailSearch } : {}) }
    : undefined;
  const { data: domains } = useApi(
    apiPath,
    selectedCheckpoint ? 'getApiBlocklistsHistoryHistoryIdDomains' : 'getApiBlocklistsIdDomains',
    params,
    [selectedList, selectedCheckpoint, detailSearch],
  );
  function handleListChange(id: number | null) {
    setSelectedList(id);
    setSelectedCheckpoint(null);
  }
  return {
    blocklists,
    selectedList,
    detailSearch,
    setDetailSearch,
    selectedCheckpoint,
    setSelectedCheckpoint,
    listHistory,
    domains,
    handleListChange,
  };
}
