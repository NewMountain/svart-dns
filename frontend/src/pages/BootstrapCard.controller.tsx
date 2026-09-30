import { postApiBootstrap, putApiBootstrap } from '../api/operations';
import { useAction } from '../hooks/useAction';
import { useApi } from '../hooks/useApi';
import { useUiField } from '../hooks/useUiField';

export function useBootstrapCardController() {
  const action = useAction();
  const { data: servers, refresh } = useApi('/api/bootstrap', 'getApiBootstrap');
  const { data: upstreams } = useApi('/api/upstreams', 'getApiUpstreams');
  const [newServer, setNewServer] = useUiField('BootstrapCard.newServer', '');
  const hasDoH = (upstreams ?? []).some(
    (u) => u.enabled && (u.upstream.startsWith('https://') || u.upstream.startsWith('tls://')),
  );
  const noBootstrap = (servers ?? []).length === 0;
  async function addServer() {
    const s = newServer.trim();
    if (!s) return;
    // Ensure it has a port
    const addr = s.includes(':') ? s : s + ':53';
    await postApiBootstrap({ server: addr });
    setNewServer('');
    refresh();
  }
  async function removeServer(id: number) {
    // PUT with remaining servers (no individual DELETE endpoint)
    const remaining = (servers ?? []).filter((s) => s.id !== id).map((s) => s.server);
    await putApiBootstrap({ servers: remaining });
    refresh();
  }
  return {
    action,
    servers,
    upstreams,
    newServer,
    setNewServer,
    hasDoH,
    noBootstrap,
    addServer,
    removeServer,
  };
}
