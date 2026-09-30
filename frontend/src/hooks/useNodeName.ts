import { useContext } from 'react';
import { NodeNameContext } from './nodeNameContext';

export function useNodeName() {
  return useContext(NodeNameContext);
}
