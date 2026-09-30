import { expect, it } from 'vitest';
import { appReducer, initialAppState } from './appState';
it('invalidates all resource projections after each confirmed configuration import', () => {
  const first = appReducer(initialAppState, { type: 'configurationImported' });
  expect(first).toEqual({ ...initialAppState, resourceRevision: 1, error: null });
  expect(appReducer(first, { type: 'configurationImported' })).toEqual({
    ...initialAppState,
    resourceRevision: 2,
    error: null,
  });
  expect(initialAppState).toEqual({ ...initialAppState, resourceRevision: 0, error: null });
});

it('preserves resource state when reporting and dismissing an operation failure', () => {
  const failed = appReducer(
    { ...initialAppState, resourceRevision: 3, error: null },
    { type: 'operationFailed', message: 'Invalid IP address' },
  );
  expect(failed).toEqual({ ...initialAppState, resourceRevision: 3, error: 'Invalid IP address' });
  expect(appReducer(failed, { type: 'dismissError' })).toEqual({
    ...initialAppState,
    resourceRevision: 3,
    error: null,
  });
});
