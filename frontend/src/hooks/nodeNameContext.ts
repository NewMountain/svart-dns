import { createContext } from 'react';

export const NodeNameContext = createContext<{
  nodeName: string;
  setNodeName: (name: string) => void;
}>({ nodeName: '', setNodeName: () => {} });
