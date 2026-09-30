import { useCallback } from 'react';
import { errorMessage } from '../lib/errors';
import { useAppState } from './appStateContext';

export function useAction() {
  const { dispatch } = useAppState();
  return useCallback(
    <Args extends unknown[]>(effect: (...args: Args) => unknown) =>
      (...args: Args): void => {
        try {
          void Promise.resolve(effect(...args)).catch((error: unknown) => {
            dispatch({ type: 'operationFailed', message: errorMessage(error) });
          });
        } catch (error) {
          dispatch({ type: 'operationFailed', message: errorMessage(error) });
        }
      },
    [dispatch],
  );
}
