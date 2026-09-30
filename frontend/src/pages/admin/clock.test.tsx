import { act, renderHook } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { AppStateProvider } from '../../hooks/AppStateProvider';
import { useAdminController } from './useAdminController';

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it('renders relative time from the captured model until the clock effect updates state', async () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-28T12:00:20Z'));
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>(() => new Promise<Response>(() => undefined)),
  );
  const { result } = renderHook(useAdminController, { wrapper: AppStateProvider });
  const captured = result.current;
  const lastUsed = '2026-09-28T12:00:00Z';
  expect(captured.formatLastUsed(lastUsed)).toBe('Just now');
  vi.setSystemTime(new Date('2026-09-28T12:10:20Z'));
  expect(captured.formatLastUsed(lastUsed)).toBe('Just now');
  act(() => {
    result.current.setShowCreateToken(true);
  });
  expect(result.current.formatLastUsed(lastUsed)).toBe('Just now');
  await act(async () => {
    await vi.advanceTimersByTimeAsync(60000);
  });
  expect(result.current.formatLastUsed(lastUsed)).toBe('11m ago');
  expect(captured.formatLastUsed(lastUsed)).toBe('Just now');
});
