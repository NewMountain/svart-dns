import {
  parseGetApiSettingsData,
  parseGetApiQueryLogsData,
  type GetApiQueryLogsData,
} from '../api/generated';
import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import * as apiClient from '../api/client';
import { AppStateProvider } from './AppStateProvider';
import { useAppState } from './appStateContext';
import { useApi, usePolling } from './useApi';

vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client');
  return {
    ...actual,
    get: vi.fn(),
  };
});

const getMock = vi.mocked(apiClient.get);
describe('useApi', () => {
  beforeEach(() => {
    getMock.mockReset();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('fetches initial data and refreshes on demand', async () => {
    getMock.mockResolvedValueOnce({ cache_ttl: '1' }).mockResolvedValueOnce({ cache_ttl: '2' });

    const { result } = renderHook(() => useApi('/api/settings', 'getApiSettings'), {
      wrapper: AppStateProvider,
    });

    await waitFor(() => {
      expect(result.current.data).toEqual({ cache_ttl: '1' });
    });

    act(() => {
      result.current.refresh();
    });

    await waitFor(() => {
      expect(result.current.data).toEqual({ cache_ttl: '2' });
    });

    expect(result.current.loading).toBe(false);
    expect(getMock).toHaveBeenNthCalledWith(1, '/api/settings', parseGetApiSettingsData, undefined);
    expect(getMock).toHaveBeenNthCalledWith(2, '/api/settings', parseGetApiSettingsData, undefined);
  });

  it('polls repeatedly on the configured interval', async () => {
    vi.useFakeTimers();
    getMock.mockResolvedValue({ logs: [], total: 0, offset: 0, limit: 20 });

    renderHook(() => usePolling('/api/query-logs', 'getApiQueryLogs', 2000, { limit: '20' }), {
      wrapper: AppStateProvider,
    });

    await act(async () => {
      await Promise.resolve();
    });
    expect(getMock).toHaveBeenCalledWith('/api/query-logs', parseGetApiQueryLogsData, {
      limit: '20',
    });

    getMock.mockClear();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });

    await act(async () => {
      await Promise.resolve();
    });
    expect(getMock).toHaveBeenCalledTimes(1);
    expect(getMock).toHaveBeenCalledWith('/api/query-logs', parseGetApiQueryLogsData, {
      limit: '20',
    });
  });

  it('re-subscribes when polling params change', async () => {
    vi.useFakeTimers();
    getMock.mockResolvedValue({ logs: [], total: 0, offset: 0, limit: 20 });

    const { rerender } = renderHook(
      ({ limit }) => usePolling('/api/query-logs', 'getApiQueryLogs', 2000, { limit }),
      { initialProps: { limit: '20' }, wrapper: AppStateProvider },
    );

    await act(async () => {
      await Promise.resolve();
    });
    expect(getMock).toHaveBeenCalledWith('/api/query-logs', parseGetApiQueryLogsData, {
      limit: '20',
    });

    getMock.mockClear();

    rerender({ limit: '50' });

    await act(async () => {
      await Promise.resolve();
    });
    expect(getMock).toHaveBeenCalledTimes(1);
    expect(getMock).toHaveBeenCalledWith('/api/query-logs', parseGetApiQueryLogsData, {
      limit: '50',
    });

    getMock.mockClear();

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });

    await act(async () => {
      await Promise.resolve();
    });
    expect(getMock).toHaveBeenCalledTimes(1);
    expect(getMock).toHaveBeenCalledWith('/api/query-logs', parseGetApiQueryLogsData, {
      limit: '50',
    });
  });

  it('waits for the current poll request to finish before scheduling the next one', async () => {
    vi.useFakeTimers();

    let resolveFirst: ((value: GetApiQueryLogsData) => void) | undefined;
    getMock
      .mockImplementationOnce(
        () =>
          new Promise<GetApiQueryLogsData>((resolve) => {
            resolveFirst = resolve;
          }),
      )
      .mockResolvedValue({ logs: [], total: 0, offset: 0, limit: 20 });

    renderHook(() => usePolling('/api/query-logs', 'getApiQueryLogs', 2000, { limit: '20' }), {
      wrapper: AppStateProvider,
    });

    await act(async () => {
      await Promise.resolve();
    });
    expect(getMock).toHaveBeenCalledTimes(1);

    await act(async () => {
      await vi.advanceTimersByTimeAsync(8000);
    });
    expect(getMock).toHaveBeenCalledTimes(1);

    await act(async () => {
      resolveFirst?.({ logs: [], total: 0, offset: 0, limit: 20 });
      await Promise.resolve();
    });

    await act(async () => {
      await vi.advanceTimersByTimeAsync(2000);
    });
    expect(getMock).toHaveBeenCalledTimes(2);
    expect(getMock).toHaveBeenNthCalledWith(2, '/api/query-logs', parseGetApiQueryLogsData, {
      limit: '20',
    });
  });
});

it('refreshes active resource views after a configuration import message', async () => {
  getMock.mockResolvedValueOnce({ cache_ttl: '120' }).mockResolvedValueOnce({ cache_ttl: '3600' });
  const { result } = renderHook(
    () => ({ resource: useApi('/api/settings', 'getApiSettings'), app: useAppState() }),
    { wrapper: AppStateProvider },
  );
  await waitFor(() => {
    expect(result.current.resource.data).toEqual({ cache_ttl: '120' });
  });
  act(() => {
    result.current.app.dispatch({ type: 'configurationImported' });
  });
  await waitFor(() => {
    expect(result.current.resource.data).toEqual({ cache_ttl: '3600' });
  });
});

it('ignores a stale response after changing resource and preserves the newer data', async () => {
  let resolveOld: ((value: { cache_ttl: string }) => void) | undefined;
  getMock
    .mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          resolveOld = resolve;
        }),
    )
    .mockResolvedValueOnce({ cache_ttl: '2' });
  const { result, rerender } = renderHook(({ path }) => useApi(path, 'getApiSettings'), {
    initialProps: { path: '/old' },
    wrapper: AppStateProvider,
  });
  rerender({ path: '/new' });
  await waitFor(() => {
    expect(result.current.data).toEqual({ cache_ttl: '2' });
  });
  await act(async () => {
    resolveOld?.({ cache_ttl: '1' });
    await Promise.resolve();
  });
  expect(result.current.data).toEqual({ cache_ttl: '2' });
});

it('reports a failed refresh while retaining successful data and permits retry', async () => {
  getMock
    .mockResolvedValueOnce({ cache_ttl: '1' })
    .mockRejectedValueOnce(new Error('Database unavailable'))
    .mockResolvedValueOnce({ cache_ttl: '3' });
  const { result } = renderHook(
    () => ({ resource: useApi('/api/settings', 'getApiSettings'), app: useAppState() }),
    { wrapper: AppStateProvider },
  );
  await waitFor(() => {
    expect(result.current.resource.data).toEqual({ cache_ttl: '1' });
  });
  act(() => {
    result.current.resource.refresh();
  });
  await waitFor(() => {
    expect(result.current.resource.error).toBe('Database unavailable');
  });
  expect(result.current.resource.data).toEqual({ cache_ttl: '1' });
  expect(result.current.app.state.error).toBe('Database unavailable');
  act(() => {
    result.current.resource.refresh();
  });
  await waitFor(() => {
    expect(result.current.resource.data).toEqual({ cache_ttl: '3' });
  });
  expect(result.current.resource.error).toBeNull();
});

it('does not fetch disabled resources or restart polling after unmount', async () => {
  vi.useFakeTimers();
  getMock.mockResolvedValue({ logs: [], total: 0, offset: 0, limit: 20 });
  const disabled = renderHook(() => useApi(null, 'getApiQueryLogs'), { wrapper: AppStateProvider });
  expect(disabled.result.current).toMatchObject({ data: null, error: null, loading: false });
  expect(getMock).not.toHaveBeenCalled();
  const active = renderHook(() => usePolling('/api/query-logs', 'getApiQueryLogs', 100), {
    wrapper: AppStateProvider,
  });
  await act(async () => {
    await Promise.resolve();
  });
  active.unmount();
  await act(async () => {
    await vi.advanceTimersByTimeAsync(500);
  });
  expect(getMock).toHaveBeenCalledTimes(1);
  vi.useRealTimers();
});

it('surfaces actual response decoding failures without replacing the last good root resource', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client');
  getMock.mockReset().mockImplementation(actual.get);
  const fetchMock = vi
    .fn<typeof fetch>()
    .mockResolvedValueOnce(Response.json({ data: { cache_ttl: '11' }, error: null }))
    .mockResolvedValueOnce(Response.json({ data: { cache_ttl: 1 }, error: null }))
    .mockResolvedValueOnce(Response.json({ data: { cache_ttl: '22' }, error: null }));
  vi.stubGlobal('fetch', fetchMock);
  try {
    const { result } = renderHook(
      () => ({ resource: useApi('/api/settings', 'getApiSettings'), app: useAppState() }),
      { wrapper: AppStateProvider },
    );
    await waitFor(() => {
      expect(result.current.resource.data).toEqual({ cache_ttl: '11' });
    });
    act(() => {
      result.current.resource.refresh();
    });
    await waitFor(() => {
      expect(result.current.resource.error).toBe('Invalid API response: GetApiSettingsData');
    });
    expect(result.current.resource.data).toEqual({ cache_ttl: '11' });
    expect(result.current.app.state.error).toBe('Invalid API response: GetApiSettingsData');
    expect(Object.values(result.current.app.state.resources.getApiSettings ?? {})).toEqual([
      expect.objectContaining({
        phase: 'failed',
        hasResult: true,
        data: { cache_ttl: '11' },
        error: 'Invalid API response: GetApiSettingsData',
      }),
    ]);
    act(() => {
      result.current.resource.refresh();
    });
    await waitFor(() => {
      expect(result.current.resource.data).toEqual({ cache_ttl: '22' });
    });
    expect(result.current.resource.error).toBeNull();
    expect(fetchMock).toHaveBeenCalledTimes(3);
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/settings',
      expect.objectContaining({ credentials: 'same-origin', cache: 'no-store' }),
    );
  } finally {
    vi.unstubAllGlobals();
  }
});

it('publishes reloads, reports their errors, and continues accepting later polls', async () => {
  vi.useFakeTimers();
  getMock
    .mockReset()
    .mockResolvedValueOnce({ cache_ttl: '1' })
    .mockResolvedValueOnce({ cache_ttl: '2' })
    .mockRejectedValueOnce(new Error('Reload unavailable'))
    .mockResolvedValueOnce({ cache_ttl: '3' });
  const { result } = renderHook(
    () => ({ resource: usePolling('/api/settings', 'getApiSettings', 2000), app: useAppState() }),
    { wrapper: AppStateProvider },
  );
  await act(async () => {
    await Promise.resolve();
  });
  expect(result.current.resource.data).toEqual({ cache_ttl: '1' });
  await act(async () => {
    expect(await result.current.resource.reload()).toEqual({ cache_ttl: '2' });
  });
  expect(result.current.resource.data).toEqual({ cache_ttl: '2' });
  await act(async () => {
    await expect(result.current.resource.reload()).rejects.toThrow('Reload unavailable');
  });
  expect(result.current.resource.error).toBe('Reload unavailable');
  expect(result.current.app.state.error).toBe('Reload unavailable');
  expect(result.current.resource.data).toEqual({ cache_ttl: '2' });
  await act(async () => {
    await vi.advanceTimersByTimeAsync(2000);
  });
  expect(result.current.resource.data).toEqual({ cache_ttl: '3' });
  expect(result.current.resource.error).toBeNull();
  vi.useRealTimers();
});

it.each(['response', 'failure'])(
  'ignores an earlier %s after reload without raising an obsolete alert',
  async (kind) => {
    let resolveOld: ((value: { cache_ttl: string }) => void) | undefined;
    let rejectOld: ((error: Error) => void) | undefined;
    getMock
      .mockReset()
      .mockImplementationOnce(
        () =>
          new Promise((resolve, reject) => {
            resolveOld = resolve;
            rejectOld = reject;
          }),
      )
      .mockResolvedValueOnce({ cache_ttl: '2' });
    const { result } = renderHook(
      () => ({ resource: useApi('/api/settings', 'getApiSettings'), app: useAppState() }),
      { wrapper: AppStateProvider },
    );
    await act(async () => {
      await result.current.resource.reload();
    });
    expect(result.current.resource.data).toEqual({ cache_ttl: '2' });
    await act(async () => {
      if (kind === 'response') resolveOld?.({ cache_ttl: '1' });
      else rejectOld?.(new Error('Obsolete failure'));
      await Promise.resolve();
    });
    expect(result.current.resource.data).toEqual({ cache_ttl: '2' });
    expect(result.current.resource.error).toBeNull();
    expect(result.current.app.state.error).toBeNull();
  },
);
