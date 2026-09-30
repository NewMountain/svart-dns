import { type ReactNode } from 'react';
import { useNodeNameProviderController } from './NodeNameProvider.controller';

import { NodeNameContext } from './nodeNameContext';

export function NodeNameProvider({ children }: { children: ReactNode }) {
  const model = useNodeNameProviderController({ children });
  return <NodeNameProviderView model={model} />;
}

function NodeNameProviderView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useNodeNameProviderController>>;
}) {
  const { children, nodeName, setNodeName } = model;
  return (
    <NodeNameContext.Provider value={{ nodeName, setNodeName }}>
      {children}
    </NodeNameContext.Provider>
  );
}
