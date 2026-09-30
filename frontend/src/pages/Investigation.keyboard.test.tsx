import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { AppStateProvider } from '../hooks/AppStateProvider';
import Investigation from './Investigation';
import templates from './investigation-templates.json';

const rectsDescriptor = Object.getOwnPropertyDescriptor(Range.prototype, 'getClientRects');
const rectDescriptor = Object.getOwnPropertyDescriptor(Range.prototype, 'getBoundingClientRect');

afterEach(() => {
  vi.unstubAllGlobals();
  for (const [name, descriptor] of [
    ['getClientRects', rectsDescriptor],
    ['getBoundingClientRect', rectDescriptor],
  ] as const) {
    if (descriptor) Object.defineProperty(Range.prototype, name, descriptor);
    else Reflect.deleteProperty(Range.prototype, name);
  }
});

it('sends the root SQL draft and current timeout on Ctrl+Enter', async () => {
  // jsdom has no text geometry; this supplies only CodeMirror's measurement boundary.
  Object.defineProperty(Range.prototype, 'getClientRects', { configurable: true, value: () => [] });
  Object.defineProperty(Range.prototype, 'getBoundingClientRect', {
    configurable: true,
    value: () => new DOMRect(),
  });
  const requests: { sql: unknown; timeout: unknown }[] = [];
  vi.stubGlobal(
    'fetch',
    vi.fn<typeof fetch>((input, options) => {
      const path =
        typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
      if (path === '/api/investigate/schema')
        return Promise.resolve(Response.json({ data: { tables: [] }, error: null }));
      if (path === '/api/investigate') {
        if (typeof options?.body !== 'string') throw new Error('Expected JSON request text');
        const body: unknown = JSON.parse(options.body);
        if (typeof body !== 'object' || body === null || !('sql' in body) || !('timeout' in body))
          throw new Error('Missing query fields');
        requests.push({ sql: body.sql, timeout: body.timeout });
        return Promise.resolve(
          Response.json({
            data: { columns: ['proof'], rows: [[42]], row_count: 1, duration_ms: 1 },
            error: null,
          }),
        );
      }
      throw new Error(`Unexpected request ${path}`);
    }),
  );
  const { container } = render(
    <AppStateProvider>
      <Investigation />
    </AppStateProvider>,
  );
  const editor = container.querySelector('.cm-content');
  if (!(editor instanceof HTMLElement)) throw new Error('Editor was not mounted');
  const templateSelect = container.querySelector('.template-select');
  const timeout = container.querySelector('.timeout-select');
  if (!(templateSelect instanceof HTMLSelectElement) || !(timeout instanceof HTMLSelectElement))
    throw new Error('Editor controls missing');
  fireEvent.change(templateSelect, { target: { value: '3' } });
  fireEvent.change(timeout, { target: { value: '10' } });
  const sql = templates[3]?.sql;
  expect(sql).toBeDefined();
  expect(editor.textContent).toContain('SELECT client_ip');
  fireEvent.keyDown(editor, { key: 'Enter', code: 'Enter', keyCode: 13, ctrlKey: true });
  await waitFor(() => {
    expect(requests).toEqual([{ sql, timeout: 10 }]);
  });
  expect(await screen.findByRole('cell', { name: '42' })).toBeVisible();
});
