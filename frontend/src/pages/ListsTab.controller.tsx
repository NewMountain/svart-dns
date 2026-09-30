import type { BlocklistView as Blocklist } from '../api/generated';
import { useAction } from '../hooks/useAction';
import { useUiField } from '../hooks/useUiField';
export function useListsTabController({
  blocklists,
  addUrl,
  setAddUrl,
  addName,
  setAddName,
  onAdd,
  onToggle,
  onRename,
  onRefresh,
  onDelete,
  onSetRefreshInterval,
}: {
  blocklists: Blocklist[];
  addUrl: string;
  setAddUrl: (v: string) => void;
  addName: string;
  setAddName: (v: string) => void;
  onAdd: () => void;
  onToggle: (id: number, enabled: boolean) => void;
  onRename: (id: number, alias: string) => Promise<void>;
  onRefresh: (id: number) => void;
  onDelete: (id: number) => void;
  onSetRefreshInterval: (id: number, interval: number) => void;
}) {
  const action = useAction();
  const [editingId, setEditingId] = useUiField('ListsTab.editingId', null);
  const [editName, setEditName] = useUiField('ListsTab.editName', '');
  function startEdit(bl: Blocklist) {
    setEditingId(bl.id);
    setEditName(bl.alias || '');
  }
  async function saveEdit(id: number) {
    await onRename(id, editName);
    setEditingId(null);
  }
  return {
    blocklists,
    addUrl,
    setAddUrl,
    addName,
    setAddName,
    onAdd,
    onToggle,
    onRefresh,
    onDelete,
    onSetRefreshInterval,
    action,
    editingId,
    setEditingId,
    editName,
    setEditName,
    startEdit,
    saveEdit,
  };
}
