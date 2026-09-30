import { expect, expectTypeOf, it } from 'vitest';
import type { GetApiSettingsData, GetApiTokensData } from '../api/generated';
import { appReducer, initialAppState } from './appState';
import { removeResource, resourceState, updateResource } from './resources';

it('composes typed tagged resource transitions without mutating prior state', () => {
  const requested = appReducer(initialAppState, {
    type: 'resourcesUpdated',
    update: (resources) =>
      updateResource(resources, 'getApiSettings', 'settings', {
        type: 'requested',
        requestKey: 'settings:1',
        requestId: 'settings:1',
      }),
  });
  expect(requested.resources).toEqual({
    getApiSettings: {
      settings: {
        phase: 'loading',
        hasResult: false,
        requestKey: 'settings:1',
        requestId: 'settings:1',
        data: null,
        error: null,
      },
    },
  });
  const ready = appReducer(requested, {
    type: 'resourcesUpdated',
    update: (resources) =>
      updateResource(resources, 'getApiSettings', 'settings', {
        type: 'received',
        requestKey: 'settings:1',
        requestId: 'settings:1',
        data: { cache_ttl: '120' },
      }),
  });
  const failed = appReducer(
    appReducer(ready, {
      type: 'resourcesUpdated',
      update: (resources) =>
        updateResource(resources, 'getApiSettings', 'settings', {
          type: 'requested',
          requestKey: 'settings:2',
          requestId: 'settings:2',
        }),
    }),
    {
      type: 'resourcesUpdated',
      update: (resources) =>
        updateResource(resources, 'getApiSettings', 'settings', {
          type: 'failed',
          requestKey: 'settings:2',
          requestId: 'settings:2',
          message: 'Database unavailable',
        }),
    },
  );
  expect(failed.resources).toEqual({
    getApiSettings: {
      settings: {
        phase: 'failed',
        hasResult: true,
        requestKey: 'settings:2',
        requestId: 'settings:2',
        data: { cache_ttl: '120' },
        error: 'Database unavailable',
      },
    },
  });
  expect(ready.resources.getApiSettings?.settings).toEqual({
    phase: 'ready',
    hasResult: true,
    requestKey: 'settings:1',
    requestId: 'settings:1',
    data: { cache_ttl: '120' },
    error: null,
  });
  expect(
    appReducer(failed, {
      type: 'resourcesUpdated',
      update: (resources) => removeResource(resources, 'getApiSettings', 'settings'),
    }),
  ).toEqual(initialAppState);
  expect(initialAppState.resources).toEqual({});
});

it('isolates sibling mounts and resource kinds and removes only departing state', () => {
  const first = updateResource(
    updateResource({}, 'getApiSettings', 'one', {
      type: 'requested',
      requestKey: 'one',
      requestId: 'one',
    }),
    'getApiSettings',
    'one',
    {
      type: 'received',
      requestKey: 'one',
      requestId: 'one',
      data: { cache_ttl: '120' },
    },
  );
  const second = updateResource(
    updateResource(first, 'getApiSettings', 'two', {
      type: 'requested',
      requestKey: 'two',
      requestId: 'two',
    }),
    'getApiSettings',
    'two',
    {
      type: 'received',
      requestKey: 'two',
      requestId: 'two',
      data: { cache_ttl: '3600' },
    },
  );
  const third = updateResource(
    updateResource(second, 'getApiTokens', 'one', {
      type: 'requested',
      requestKey: 'tokens',
      requestId: 'tokens',
    }),
    'getApiTokens',
    'one',
    {
      type: 'received',
      requestKey: 'tokens',
      requestId: 'tokens',
      data: [],
    },
  );
  const removed = removeResource(third, 'getApiSettings', 'one');
  expect(removed).toEqual({
    getApiSettings: {
      two: {
        phase: 'ready',
        hasResult: true,
        requestKey: 'two',
        requestId: 'two',
        data: { cache_ttl: '3600' },
        error: null,
      },
    },
    getApiTokens: {
      one: {
        phase: 'ready',
        hasResult: true,
        requestKey: 'tokens',
        requestId: 'tokens',
        data: [],
        error: null,
      },
    },
  });
  expect(resourceState(first, 'getApiSettings', 'one').data).toEqual({ cache_ttl: '120' });
  expect(removeResource(removed, 'getApiSettings', 'absent')).toBe(removed);
  expect(removeResource(removed, 'getApiUsers', 'absent')).toBe(removed);
  expect(
    appReducer(
      { ...initialAppState, resources: removed },
      { type: 'resourcesUpdated', update: (resources) => resources },
    ).resources,
  ).toBe(removed);
});

it('keeps concrete key/payload correlation in selectors and updates at compile time', () => {
  expectTypeOf(
    resourceState({}, 'getApiSettings', 'one').data,
  ).toEqualTypeOf<GetApiSettingsData | null>();
  expectTypeOf(resourceState({}, 'getApiTokens', 'one').data).toEqualTypeOf<GetApiTokensData>();
  expectTypeOf(() => {
    // @ts-expect-error Unknown resource keys must never enter the root registry.
    resourceState({}, 'misspelledSettings', 'one');
    updateResource({}, 'getApiTokens', 'one', {
      type: 'received',
      requestKey: 'one',
      requestId: 'one',
      // @ts-expect-error A settings payload cannot be written under the token key.
      data: { cache_ttl: '120' },
    });
    updateResource({}, 'getApiSettings', 'one', {
      type: 'received',
      requestKey: 'one',
      requestId: 'one',
      // @ts-expect-error A token collection cannot be written under the settings key.
      data: [],
    });
  }).toBeFunction();
});
