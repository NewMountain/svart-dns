import { useCallback, useEffect, useId } from 'react';
import { get } from '../api/client';
import { apiView } from '../lib/apiState';
import {
  resourceDecoders,
  resourceState,
  updateResource,
  removeResource,
  type ResourceKey,
  type ResourceData,
} from '../lib/resources';
import { errorMessage } from '../lib/errors';
import { useAppState } from './appStateContext';
import { useUiField } from './useUiField';

interface UseApiResult<T> {
  data: T | null;
  error: string | null;
  loading: boolean;
  hasResult: boolean;
  refresh: () => void;
  reload: () => Promise<T>;
}
type RefreshDependency = string | number | boolean | null;

function useResource<K extends ResourceKey>(
  path: string | null,
  key: K,
  params: Record<string, string> | undefined,
  dependencyKey: string,
  intervalMs: number | null,
): UseApiResult<ResourceData[K]> {
  const {
    state: { resourceRevision, resources },
    dispatch: dispatchApp,
  } = useAppState();
  const decode = resourceDecoders[key];
  const viewInstance = useId();
  const instance = JSON.stringify([viewInstance, path, key]);
  const state = resourceState(resources, key, instance);
  const [tick, setTick] = useUiField('useResource.tick', 0);
  const refresh = useCallback(() => {
    setTick((value) => value + 1);
  }, [setTick]);
  useEffect(
    () => () => {
      dispatchApp({
        type: 'resourcesUpdated',
        update: (previous) => removeResource(previous, key, instance),
      });
    },
    [dispatchApp, key, instance],
  );
  const paramKey = params ? new URLSearchParams(params).toString() : '';
  const requestKey = JSON.stringify([path, paramKey, dependencyKey, resourceRevision, tick]);

  useEffect(() => {
    if (path === null) return;
    const requestPath = path;
    const lifetime = new AbortController();
    let timeout: ReturnType<typeof setTimeout> | undefined;
    const query = paramKey ? Object.fromEntries(new URLSearchParams(paramKey)) : undefined;
    async function fetchData() {
      const requestId = crypto.randomUUID();
      dispatchApp({
        type: 'resourcesUpdated',
        update: (previous) =>
          updateResource(previous, key, instance, { type: 'requested', requestKey, requestId }),
      });
      try {
        const data = await get(requestPath, decode, query);
        if (!lifetime.signal.aborted)
          dispatchApp({
            type: 'resourcesUpdated',
            update: (previous) =>
              updateResource(previous, key, instance, {
                type: 'received',
                requestKey,
                requestId,
                data,
              }),
          });
      } catch (error: unknown) {
        if (!lifetime.signal.aborted) {
          const failure = errorMessage(error);
          dispatchApp({
            type: 'resourcesUpdated',
            update: (previous) =>
              updateResource(previous, key, instance, {
                type: 'failed',
                requestKey,
                requestId,
                message: failure,
              }),
            failure,
          });
        }
      } finally {
        if (!lifetime.signal.aborted && intervalMs !== null)
          timeout = setTimeout(() => {
            void fetchData();
          }, intervalMs);
      }
    }
    void fetchData();
    return () => {
      lifetime.abort();
      if (timeout !== undefined) clearTimeout(timeout);
    };
  }, [path, decode, paramKey, requestKey, intervalMs, dispatchApp, instance, key]);

  async function reload(): Promise<ResourceData[K]> {
    if (path === null) throw new Error('Cannot reload a disabled resource');
    const requestId = crypto.randomUUID();
    dispatchApp({
      type: 'resourcesUpdated',
      update: (previous) =>
        updateResource(previous, key, instance, { type: 'requested', requestKey, requestId }),
    });
    try {
      const data = await get(path, decode, params);
      dispatchApp({
        type: 'resourcesUpdated',
        update: (previous) =>
          updateResource(previous, key, instance, {
            type: 'received',
            requestKey,
            requestId,
            data,
          }),
      });
      return data;
    } catch (error: unknown) {
      const failure = errorMessage(error);
      dispatchApp({
        type: 'resourcesUpdated',
        update: (previous) =>
          updateResource(previous, key, instance, {
            type: 'failed',
            requestKey,
            requestId,
            message: failure,
          }),
        failure,
      });
      throw error;
    }
  }
  return { ...apiView(state, requestKey, path !== null), refresh, reload };
}

export function useApi<K extends ResourceKey>(
  path: string | null,
  key: K,
  params?: Record<string, string>,
  deps: readonly RefreshDependency[] = [],
): UseApiResult<ResourceData[K]> {
  return useResource(path, key, params, JSON.stringify(deps), null);
}
export function usePolling<K extends ResourceKey>(
  path: string | null,
  key: K,
  intervalMs: number,
  params?: Record<string, string>,
): UseApiResult<ResourceData[K]> {
  return useResource(path, key, params, '', intervalMs);
}
