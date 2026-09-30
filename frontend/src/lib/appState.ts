import type { Resources } from './resources';
import { initialUiState, type UiState } from './uiState';
export interface AppState {
  ui: UiState;
  resources: Resources;
  resourceRevision: number;
  error: string | null;
}
export type AppMessage =
  | { type: 'configurationImported' }
  | { type: 'operationFailed'; message: string }
  | { type: 'dismissError' }
  | { type: 'uiUpdated'; update: (ui: UiState) => UiState }
  | { type: 'resourcesUpdated'; update: (resources: Resources) => Resources; failure?: string };
export const initialAppState: AppState = {
  resourceRevision: 0,
  error: null,
  ui: initialUiState,
  resources: {},
};
export function appReducer(state: AppState, message: AppMessage): AppState {
  switch (message.type) {
    case 'uiUpdated': {
      const ui = message.update(state.ui);
      return ui === state.ui ? state : { ...state, ui };
    }
    case 'resourcesUpdated': {
      const resources = message.update(state.resources);
      return resources === state.resources
        ? state
        : { ...state, resources, error: message.failure ?? state.error };
    }
    case 'configurationImported':
      return { ...state, resourceRevision: state.resourceRevision + 1 };
    case 'operationFailed':
      return { ...state, error: message.message };
    case 'dismissError':
      return { ...state, error: null };
  }
}
