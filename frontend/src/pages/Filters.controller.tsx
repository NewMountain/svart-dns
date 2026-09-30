import { useSearchParams } from 'react-router-dom';
import {
  deleteApiBlocklistsId,
  postApiBlocklists,
  postApiBlocklistsIdRefresh,
  postApiBlocklistsIdToggle,
  putApiBlocklistsId,
} from '../api/operations';
import { useAction } from '../hooks/useAction';
import { useApi } from '../hooks/useApi';
import { useUiField } from '../hooks/useUiField';
type Tab = 'lists' | 'history' | 'details';
export function useFiltersController() {
  const action = useAction();
  const [searchParams, setSearchParams] = useSearchParams();
  const urlTab = searchParams.get('tab');
  const urlList = searchParams.get('list');
  const urlSearch = searchParams.get('search');
  const tab: Tab = urlTab === 'history' || urlTab === 'details' ? urlTab : 'lists';
  const { data: blocklists, refresh } = useApi('/api/blocklists', 'getApiBlocklists');
  const [addUrl, setAddUrl] = useUiField('Filters.addUrl', '');
  const [addName, setAddName] = useUiField('Filters.addName', '');
  const selectedList = urlList ? Number(urlList) : null;
  const detailSearch = urlSearch ?? '';
  function updateParams(updates: Record<string, string | null>) {
    const next: Record<string, string> = {};
    const current = Object.fromEntries(searchParams.entries());
    for (const [k, v] of Object.entries({ ...current, ...updates })) {
      if (v !== null && v !== '') next[k] = v;
    }
    setSearchParams(next, { replace: true });
  }
  function setTab(newTab: Tab) {
    updateParams({ tab: newTab, list: null, search: null });
  }
  function setSelectedList(id: number | null) {
    updateParams({ list: id !== null ? String(id) : null });
  }
  function setDetailSearch(s: string) {
    updateParams({ search: s || null });
  }
  async function addList() {
    if (!addUrl) return;
    await postApiBlocklists({ url: addUrl, alias: addName, enabled: true });
    setAddUrl('');
    setAddName('');
    refresh();
  }
  async function toggleList(id: number, _enabled: boolean) {
    await postApiBlocklistsIdToggle(id);
    refresh();
  }
  async function renameList(id: number, alias: string) {
    await putApiBlocklistsId(id, { alias });
    refresh();
  }
  async function refreshList(id: number) {
    await postApiBlocklistsIdRefresh(id);
    refresh();
  }
  async function deleteList(id: number) {
    await deleteApiBlocklistsId(id);
    refresh();
  }
  async function setRefreshInterval(id: number, refresh_interval: number) {
    await putApiBlocklistsId(id, { refresh_interval });
    refresh();
  }
  return {
    action,
    tab,
    blocklists,
    addUrl,
    setAddUrl,
    addName,
    setAddName,
    selectedList,
    detailSearch,
    setTab,
    setSelectedList,
    setDetailSearch,
    addList,
    toggleList,
    renameList,
    refreshList,
    deleteList,
    setRefreshInterval,
  };
}
