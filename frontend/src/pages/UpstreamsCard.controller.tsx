import {
  deleteApiUpstreamsId,
  postApiUpstreams,
  postApiUpstreamsIdToggle,
} from '../api/operations';
import { useAction } from '../hooks/useAction';
import { useApi } from '../hooks/useApi';
import { useUiField } from '../hooks/useUiField';

export function useUpstreamsCardController() {
  const action = useAction();
  const { data: upstreams, refresh } = useApi('/api/upstreams', 'getApiUpstreams');
  const [newUpstream, setNewUpstream] = useUiField('UpstreamsCard.newUpstream', '');
  async function addUpstream() {
    if (!newUpstream) return;
    await postApiUpstreams({ upstream: newUpstream, enabled: true });
    setNewUpstream('');
    refresh();
  }
  async function toggleUpstream(id: number) {
    await postApiUpstreamsIdToggle(id);
    refresh();
  }
  async function deleteUpstream(id: number) {
    await deleteApiUpstreamsId(id);
    refresh();
  }
  function protocolBadge(addr: string) {
    if (addr.startsWith('https://')) return <span className="upstream-proto proto-doh">DoH</span>;
    if (addr.startsWith('tls://')) return <span className="upstream-proto proto-dot">DoT</span>;
    if (addr.includes(':853')) return <span className="upstream-proto proto-dot">DoT</span>;
    return <span className="upstream-proto proto-udp">UDP</span>;
  }
  return {
    action,
    upstreams,
    newUpstream,
    setNewUpstream,
    addUpstream,
    toggleUpstream,
    deleteUpstream,
    protocolBadge,
  };
}
