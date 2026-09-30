export interface ApiState<T> {
  phase: 'idle' | 'loading' | 'ready' | 'failed';
  data: T | null;
  hasResult: boolean;
  error: string | null;
  requestKey: string | null;
  requestId: string | null;
}
export type ApiMessage<T> =
  | { type: 'requested'; requestKey: string; requestId: string }
  | { type: 'received'; requestKey: string; requestId: string; data: T }
  | { type: 'failed'; requestKey: string; requestId: string; message: string };
export function initialApiState<T>(): ApiState<T> {
  return {
    phase: 'idle',
    hasResult: false,
    data: null,
    error: null,
    requestKey: null,
    requestId: null,
  };
}
export function apiReducer<T>(state: ApiState<T>, message: ApiMessage<T>): ApiState<T> {
  switch (message.type) {
    case 'requested':
      return {
        ...state,
        phase: 'loading',
        error: null,
        requestKey: message.requestKey,
        requestId: message.requestId,
      };
    case 'received':
      if (state.requestId !== message.requestId) return state;
      return {
        phase: 'ready',
        hasResult: true,
        data: message.data,
        error: null,
        requestKey: message.requestKey,
        requestId: message.requestId,
      };
    case 'failed':
      if (state.requestId !== message.requestId) return state;
      return {
        ...state,
        phase: 'failed',
        error: message.message,
        requestKey: message.requestKey,
        requestId: message.requestId,
      };
  }
}
export function apiView<T>(state: ApiState<T>, requestKey: string, enabled: boolean) {
  return {
    data: state.data,
    hasResult: state.hasResult,
    error: !enabled || state.requestKey === requestKey ? state.error : null,
    loading:
      enabled && !state.hasResult && (state.phase === 'loading' || state.requestKey !== requestKey),
  };
}
