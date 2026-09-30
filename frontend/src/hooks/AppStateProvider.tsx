import { useReducer, type ReactNode } from 'react';
import { appReducer, initialAppState } from '../lib/appState';
import { AppStateContext } from './appStateContext';

export function AppStateProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(appReducer, initialAppState);
  return (
    <AppStateContext.Provider value={{ state, dispatch }}>{children}</AppStateContext.Provider>
  );
}
