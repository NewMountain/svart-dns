import { expect, it } from 'vitest';
import { apiReducer, apiView, initialApiState } from './apiState';
it('projects initial, disabled, received, refreshed, and failed request states exactly', () => {
  const empty = initialApiState<{ name: string }>();
  expect(apiView(empty, 'first', true)).toEqual({
    data: null,
    hasResult: false,
    error: null,
    loading: true,
  });
  expect(apiView(empty, 'first', false)).toEqual({
    data: null,
    hasResult: false,
    error: null,
    loading: false,
  });
  const loaded = apiReducer(
    apiReducer(empty, { type: 'requested', requestKey: 'first', requestId: 'first' }),
    {
      type: 'received',
      requestKey: 'first',
      requestId: 'first',
      data: { name: 'Living room' },
    },
  );
  expect(loaded).toEqual({
    phase: 'ready',
    hasResult: true,
    data: { name: 'Living room' },
    error: null,
    requestKey: 'first',
    requestId: 'first',
  });
  expect(apiView(loaded, 'refresh', true)).toEqual({
    data: { name: 'Living room' },
    error: null,
    loading: false,
    hasResult: true,
  });
  const failed = apiReducer(
    apiReducer(loaded, { type: 'requested', requestKey: 'refresh', requestId: 'refresh' }),
    {
      type: 'failed',
      requestKey: 'refresh',
      requestId: 'refresh',
      message: 'Database unavailable',
    },
  );
  expect(apiView(failed, 'refresh', true)).toEqual({
    data: { name: 'Living room' },
    error: 'Database unavailable',
    loading: false,
    hasResult: true,
  });
  expect(apiView(failed, 'retry', true)).toEqual({
    data: { name: 'Living room' },
    error: null,
    loading: false,
    hasResult: true,
  });
  expect(empty).toEqual({
    phase: 'idle',
    hasResult: false,
    data: null,
    error: null,
    requestKey: null,
    requestId: null,
  });
});
it('ends loading on an initial failure or a successful null result', () => {
  expect(
    apiView(
      apiReducer(
        apiReducer(initialApiState(), {
          type: 'requested',
          requestKey: 'first',
          requestId: 'first',
        }),
        { type: 'failed', requestKey: 'first', requestId: 'first', message: 'Denied' },
      ),
      'first',
      true,
    ),
  ).toEqual({ data: null, hasResult: false, error: 'Denied', loading: false });
  expect(
    apiView(
      apiReducer(
        apiReducer(initialApiState(), {
          type: 'requested',
          requestKey: 'first',
          requestId: 'first',
        }),
        { type: 'received', requestKey: 'first', requestId: 'first', data: null },
      ),
      'first',
      true,
    ),
  ).toEqual({ data: null, hasResult: true, error: null, loading: false });
});
