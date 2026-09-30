import type { RecentLogView as RecentLog } from '../../api/generated';
import {
  getApiQueryLogsId,
  postApiClientsIpAllowDomain,
  postApiClientsIpBlockDomain,
} from '../../api/operations';
import { useAction } from '../../hooks/useAction';
import { useUiField } from '../../hooks/useUiField';

export function useRecentActivityWidgetController({ logs }: { logs: RecentLog[] }) {
  const action = useAction();
  const [expandedId, setExpandedId] = useUiField('RecentActivityWidget.expandedId', null);
  const [evalDetail, setEvalDetail] = useUiField('RecentActivityWidget.evalDetail', null);
  const [evalLoading, setEvalLoading] = useUiField('RecentActivityWidget.evalLoading', false);
  const [contextMenu, setContextMenu] = useUiField('RecentActivityWidget.contextMenu', null);
  async function toggleExpand(entryId: number) {
    if (expandedId === entryId) {
      setExpandedId(null);
      setEvalDetail(null);
      return;
    }
    setExpandedId(entryId);
    setEvalDetail(null);
    setEvalLoading(true);
    try {
      const detail = await getApiQueryLogsId(entryId);
      setEvalDetail(detail);
    } catch {
      setEvalDetail(null);
    }
    setEvalLoading(false);
  }
  function openContextMenu(e: React.MouseEvent, log: RecentLog) {
    e.preventDefault();
    e.stopPropagation();
    setContextMenu({ x: e.clientX, y: e.clientY, log });
  }
  async function allowDomain(domain: string, clientIp: string) {
    await postApiClientsIpAllowDomain(clientIp, { domain });
  }
  async function blockDomain(domain: string, clientIp: string) {
    await postApiClientsIpBlockDomain(clientIp, { domain });
  }
  const COL_COUNT = 11;
  return {
    logs,
    action,
    expandedId,
    evalDetail,
    evalLoading,
    contextMenu,
    setContextMenu,
    toggleExpand,
    openContextMenu,
    allowDomain,
    blockDomain,
    COL_COUNT,
  };
}
