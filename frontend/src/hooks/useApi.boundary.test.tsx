import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { parseGetApiSettingsData } from '../api/generated';
import { AppStateProvider } from './AppStateProvider';
import { useAppState } from './appStateContext';
import { useApi } from './useApi';

vi.mock('../api/generated', async () => {
  const actual = await vi.importActual<typeof import('../api/generated')>('../api/generated');
  return { ...actual, parseGetApiSettingsData: vi.fn(actual.parseGetApiSettingsData) };
});
afterEach(() => {
  vi.unstubAllGlobals();
  vi.clearAllMocks();
});
it('decodes each actual HTTP response once and trusts the stored model on unrelated updates', async () => {
  vi.stubGlobal(
    'fetch',
    vi
      .fn<typeof fetch>()
      .mockResolvedValueOnce(Response.json({ data: { cache_ttl: '120' }, error: null }))
      .mockResolvedValueOnce(Response.json({ data: { cache_ttl: '3600' }, error: null })),
  );
  const { result, rerender } = renderHook(
    () => ({ resource: useApi('/api/settings', 'getApiSettings'), app: useAppState() }),
    { wrapper: AppStateProvider },
  );
  await waitFor(() => {
    expect(result.current.resource.data).toEqual({ cache_ttl: '120' });
  });
  expect(parseGetApiSettingsData).toHaveBeenCalledTimes(1);
  const stored = result.current.resource.data;
  act(() => {
    result.current.app.dispatch({ type: 'operationFailed', message: 'Unrelated action failed' });
  });
  rerender();
  expect(result.current.resource.data).toBe(stored);
  expect(parseGetApiSettingsData).toHaveBeenCalledTimes(1);
  act(() => {
    result.current.resource.refresh();
  });
  await waitFor(() => {
    expect(result.current.resource.data).toEqual({ cache_ttl: '3600' });
  });
  expect(parseGetApiSettingsData).toHaveBeenCalledTimes(2);
});
