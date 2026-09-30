import { useCallback, useId, useLayoutEffect, type SetStateAction } from 'react';
import type { UiFieldValues } from '../lib/uiState';
import { changeUiField, registerUiField, removeUiField } from '../lib/uiState';
import { useAppState } from './appStateContext';

/** A typed selector and update edge into the single application state tree. */
export function useUiField<K extends keyof UiFieldValues>(
  key: K,
  initial: UiFieldValues[K],
): [UiFieldValues[K], (update: SetStateAction<UiFieldValues[K]>) => void] {
  const { state, dispatch } = useAppState();
  const instance = useId();
  const stored = state.ui[key][instance];
  useLayoutEffect(() => {
    if (stored !== undefined) return;
    dispatch({ type: 'uiUpdated', update: (ui) => registerUiField(ui, key, instance, initial) });
  }, [dispatch, key, instance, initial, stored]);
  useLayoutEffect(
    () => () => {
      dispatch({ type: 'uiUpdated', update: (ui) => removeUiField(ui, key, instance) });
    },
    [dispatch, key, instance],
  );
  const set = useCallback(
    (update: SetStateAction<UiFieldValues[K]>) => {
      dispatch({ type: 'uiUpdated', update: (ui) => changeUiField(ui, key, instance, update) });
    },
    [dispatch, key, instance],
  );
  return [stored === undefined ? initial : stored, set];
}

export function useUiReducer<K extends keyof UiFieldValues, Message>(
  key: K,
  reducer: (state: UiFieldValues[K], message: Message) => UiFieldValues[K],
  initial: UiFieldValues[K],
): [UiFieldValues[K], (message: Message) => void] {
  const [state, set] = useUiField(key, initial);
  const dispatch = useCallback(
    (message: Message) => {
      set((previous) => reducer(previous, message));
    },
    [set, reducer],
  );
  return [state, dispatch];
}
