import { useEffect, type ReactNode } from 'react';
import { getApiPeers } from '../api/operations';
import { useAppState } from './appStateContext';
import { useAction } from './useAction';
import { useUiField } from './useUiField';

export function useNodeNameProviderController({ children }: { children: ReactNode }) {
  const {
    state: { resourceRevision },
  } = useAppState();
  const action = useAction();
  const [nodeName, setNodeName] = useUiField('NodeNameProvider.nodeName', '');
  useEffect(() => {
    action(() =>
      getApiPeers().then((data) => {
        setNodeName(data.node_name || '');
      }),
    )();
  }, [resourceRevision, action, setNodeName]);
  return { children, nodeName, setNodeName };
}
