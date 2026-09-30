import { createContext, useContext, type Dispatch } from 'react';
import { initialAppState, type AppMessage, type AppState } from '../lib/appState';

export const AppStateContext = createContext<{ state: AppState; dispatch: Dispatch<AppMessage> }>({
  state: initialAppState,
  dispatch: () => {},
});
export function useAppState() {
  return useContext(AppStateContext);
}
