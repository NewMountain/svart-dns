import { useMemo } from 'react';
import type { RewriteView as Rewrite } from '../api/generated';
import { deleteApiRewritesId, postApiRewrites, putApiRewritesId } from '../api/operations';
import { useAction } from '../hooks/useAction';
import { useApi } from '../hooks/useApi';
import { useUiField } from '../hooks/useUiField';

export function useRewritesController() {
  const action = useAction();
  const { data: rewrites, refresh } = useApi('/api/rewrites', 'getApiRewrites');
  const { data: stats } = useApi('/api/rewrites/stats', 'getApiRewritesStats', {
    window: '24h',
  });
  const [domain, setDomain] = useUiField('Rewrites.domain', '');
  const [ip, setIp] = useUiField('Rewrites.ip', '');
  const [search, setSearch] = useUiField('Rewrites.search', '');
  const [editingId, setEditingId] = useUiField('Rewrites.editingId', null);
  const [editDomain, setEditDomain] = useUiField('Rewrites.editDomain', '');
  const [editIp, setEditIp] = useUiField('Rewrites.editIp', '');
  const statsMap = new Map((stats ?? []).map((s) => [s.domain, s]));
  const filtered = useMemo(() => {
    const q = search.toLowerCase();
    if (!q) return rewrites ?? [];
    return (rewrites ?? []).filter(
      (rw) => rw.domain.toLowerCase().includes(q) || rw.ip_addresses.includes(q),
    );
  }, [rewrites, search]);
  async function addRewrite() {
    if (!domain || !ip) return;
    await postApiRewrites({ domain, ip_addresses: ip, enabled: true });
    setDomain('');
    setIp('');
    refresh();
  }
  async function toggleRewrite(id: number, enabled: boolean) {
    await putApiRewritesId(id, { enabled });
    refresh();
  }
  async function deleteRewrite(id: number) {
    await deleteApiRewritesId(id);
    refresh();
  }
  async function saveEdit(id: number) {
    await putApiRewritesId(id, { domain: editDomain, ip_addresses: editIp });
    setEditingId(null);
    refresh();
  }
  function startEdit(rw: Rewrite) {
    setEditingId(rw.id);
    setEditDomain(rw.domain);
    setEditIp(rw.ip_addresses);
  }
  return {
    action,
    rewrites,
    stats,
    domain,
    setDomain,
    ip,
    setIp,
    search,
    setSearch,
    editingId,
    setEditingId,
    editDomain,
    setEditDomain,
    editIp,
    setEditIp,
    statsMap,
    filtered,
    addRewrite,
    toggleRewrite,
    deleteRewrite,
    saveEdit,
    startEdit,
  };
}
