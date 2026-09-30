import { expect, it } from 'vitest';
import { appReducer, initialAppState } from './appState';
import { changeUiField, initialUiState, registerUiField, removeUiField } from './uiState';
it('registers isolated typed fields and preserves an existing draft during re-registration', () => {
  const first = registerUiField(initialUiState, 'Rewrites.domain', 'first', 'first.example');
  const second = registerUiField(first, 'Rewrites.domain', 'second', 'second.example');
  expect(second).toEqual({
    ...initialUiState,
    'Rewrites.domain': { first: 'first.example', second: 'second.example' },
  });
  expect(registerUiField(second, 'Rewrites.domain', 'first', 'reset.example')).toBe(second);
  expect(initialUiState['Rewrites.domain']).toEqual({});
});
it('applies sequential pure root changes without overwriting sibling fields or prior state', () => {
  const initial = {
    ...initialAppState,
    ui: registerUiField(initialUiState, 'Logs.page', 'logs', {
      filterKey: 'client=one',
      offset: 0,
    }),
  };
  const first = appReducer(initial, {
    type: 'uiUpdated',
    update: (ui) =>
      changeUiField(ui, 'Logs.page', 'logs', (page) => ({ ...page, offset: page.offset + 100 })),
  });
  const second = appReducer(first, {
    type: 'uiUpdated',
    update: (ui) =>
      changeUiField(ui, 'Logs.page', 'logs', (page) => ({ ...page, offset: page.offset + 100 })),
  });
  expect(second).toEqual({
    ...initialAppState,
    ui: { ...initialUiState, 'Logs.page': { logs: { filterKey: 'client=one', offset: 200 } } },
  });
  expect(initial.ui['Logs.page']).toEqual({ logs: { filterKey: 'client=one', offset: 0 } });
  expect(changeUiField(second.ui, 'Logs.page', 'logs', (page) => page)).toBe(second.ui);
});
it('removes only the departing view and ignores its later completion', () => {
  const mounted = registerUiField(
    registerUiField(initialUiState, 'Login.username', 'one', 'operator'),
    'Login.username',
    'two',
    'reviewer',
  );
  const removed = removeUiField(mounted, 'Login.username', 'one');
  expect(removed).toEqual({ ...initialUiState, 'Login.username': { two: 'reviewer' } });
  expect(changeUiField(removed, 'Login.username', 'one', 'late result')).toBe(removed);
  expect(mounted['Login.username']).toEqual({ one: 'operator', two: 'reviewer' });
});
it('owns the complete SQL draft without losing content or mutating prior query state', () => {
  const sql = 'SELECT domain FROM query_logs;\n-- preserved draft'.repeat(1000);
  const initial = {
    ...initialAppState,
    ui: registerUiField(initialUiState, 'Investigation.sql', 'editor', 'SELECT 1'),
  };
  const changed = appReducer(initial, {
    type: 'uiUpdated',
    update: (ui) => changeUiField(ui, 'Investigation.sql', 'editor', sql),
  });
  expect(changed.ui['Investigation.sql']).toEqual({ editor: sql });
  expect(initial.ui['Investigation.sql']).toEqual({ editor: 'SELECT 1' });
  const failed = appReducer(changed, { type: 'operationFailed', message: 'Query failed' });
  expect(failed.ui['Investigation.sql']).toEqual({ editor: sql });
});
