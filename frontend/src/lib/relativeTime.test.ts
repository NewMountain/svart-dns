import { expect, it } from 'vitest';
import { relativeTime } from './relativeTime';
import { appReducer, initialAppState } from './appState';
import { changeUiField, registerUiField } from './uiState';

it('renders every relative-time boundary from explicit timestamps', () => {
  const lastUsed = '2026-09-28T12:00:00Z';
  const now = Date.parse(lastUsed);
  expect(relativeTime(null)).toBe('Never');
  expect(relativeTime(now, '')).toBe('Never');
  expect(relativeTime(null, lastUsed)).toBe('-');
  for (const [minutes, label] of [
    [0, 'Just now'],
    [1, '1m ago'],
    [59, '59m ago'],
    [60, '1h ago'],
    [1439, '23h ago'],
    [1440, '1d ago'],
    [2880, '2d ago'],
  ] satisfies [number, string][]) {
    expect(relativeTime(now + minutes * 60000, lastUsed)).toBe(label);
  }
});
it('updates only the clock field through the immutable root transition', () => {
  const initial = {
    ...initialAppState,
    ui: registerUiField(initialAppState.ui, 'useAdminController.nowMs', 'admin', 1000),
  };
  const changed = appReducer(initial, {
    type: 'uiUpdated',
    update: (ui) => changeUiField(ui, 'useAdminController.nowMs', 'admin', 61000),
  });
  expect(changed).toEqual({
    ...initialAppState,
    ui: { ...initialAppState.ui, 'useAdminController.nowMs': { admin: 61000 } },
  });
  expect(initial.ui['useAdminController.nowMs']).toEqual({ admin: 1000 });
});
