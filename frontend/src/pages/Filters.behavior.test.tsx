import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, useLocation } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { BlocklistHistoryView, BlocklistView } from '../api/generated';
import ApplicationStatus from '../components/ApplicationStatus';
import { AppStateProvider } from '../hooks/AppStateProvider';
import { call, reply, unavailable, type Call } from '../test/http';
import Filters from './Filters';
const fetchMock = vi.fn<typeof fetch>();
let lists: BlocklistView[];
let writes: Call[];
let failed: boolean;
const history: BlocklistHistoryView[] = [
  {
    id: 10,
    blocklist_id: 1,
    blocklist_alias: 'Primary',
    added_count: 1,
    removed_count: 1,
    new_count: 2,
    previous_count: 2,
    refreshed_at: '2026-09-01T12:00:00Z',
    sample_added: ['added.example'],
    sample_removed: ['removed.example'],
  },
  {
    id: 11,
    blocklist_id: 2,
    blocklist_alias: 'Secondary',
    added_count: 0,
    removed_count: 0,
    new_count: 0,
    previous_count: 0,
    refreshed_at: '2026-09-01T13:00:00Z',
    sample_added: [],
    sample_removed: [],
  },
];
beforeEach(() => {
  lists = [
    {
      id: 1,
      alias: 'Primary',
      url: 'https://lists.example/primary',
      enabled: true,
      domain_count: 2,
      compatibility: {
        assessed: false,
        assessed_at: '',
        lines: 0,
        applied: 0,
        unsupported: 0,
        invalid: 0,
        diagnostic_count: 0,
      },
      last_updated: '',
      refresh_interval: 86400,
    },
  ];
  writes = [];
  failed = false;
  fetchMock.mockImplementation((input, init) => {
    const request = call(input, init),
      { path, method, body, query } = request;
    if (method === 'GET') {
      if (path === '/api/blocklists') return reply(lists);
      if (path === '/api/blocklists/history') return reply(history);
      if (path === '/api/blocklists/1/domains' || path === '/api/blocklists/history/10/domains')
        return reply({
          blocklist_id: 1,
          domains: query.has('search')
            ? []
            : [path.includes('history') ? 'checkpoint.example' : 'current.example'],
          limit: 200,
          offset: 0,
          total: query.has('search') ? 0 : 1,
          history_id: 10,
          refreshed_at: '2026-09-01T12:00:00Z',
        });
    }
    writes.push(request);
    if (failed) return unavailable();
    if (path === '/api/blocklists' && method === 'POST') {
      if (typeof body.url !== 'string' || typeof body.alias !== 'string')
        throw Error('Invalid list');
      const record = {
        id: 2,
        alias: body.alias,
        url: body.url,
        enabled: true,
        domain_count: 0,
        compatibility: {
          assessed: false,
          assessed_at: '',
          lines: 0,
          applied: 0,
          unsupported: 0,
          invalid: 0,
          diagnostic_count: 0,
        },
        last_updated: '',
        refresh_interval: 86400,
      };
      lists = [...lists, record];
      return reply({ id: 2, alias: body.alias });
    }
    if (path === '/api/blocklists/1/toggle')
      lists = lists.map((l) => (l.id === 1 ? { ...l, enabled: !l.enabled } : l));
    else if (path === '/api/blocklists/1' && method === 'DELETE') lists = [];
    else if (path === '/api/blocklists/1' && method === 'PUT')
      lists = lists.map((l) => ({
        ...l,
        alias: typeof body.alias === 'string' ? body.alias : l.alias,
        refresh_interval:
          typeof body.refresh_interval === 'number' ? body.refresh_interval : l.refresh_interval,
      }));
    else if (path === '/api/blocklists/1/refresh') return reply({ message: 'Refresh queued' });
    else throw Error(`Unexpected ${method} ${path}`);
    return reply({ success: true });
  });
  vi.stubGlobal('fetch', fetchMock);
});
afterEach(() => vi.unstubAllGlobals());
function Location() {
  return <output aria-label="location">{useLocation().search}</output>;
}
function mount() {
  render(
    <AppStateProvider>
      <MemoryRouter>
        <ApplicationStatus />
        <Filters />
        <Location />
      </MemoryRouter>
    </AppStateProvider>,
  );
}
it('imports a list with retained input on failure and exact successful request', async () => {
  mount();
  await screen.findByText('Primary');
  const url = screen.getByPlaceholderText(
      'Paste Blocklist URL (e.g. https://raw.githubusercontent.com/...)',
    ),
    name = screen.getByPlaceholderText('Name (optional)');
  fireEvent.click(screen.getByRole('button', { name: 'Import List' }));
  expect(writes).toHaveLength(0);
  fireEvent.change(url, { target: { value: 'https://lists.example/new' } });
  fireEvent.change(name, { target: { value: 'New list' } });
  failed = true;
  fireEvent.keyDown(url, { key: 'Enter' });
  expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
  expect(url).toHaveValue('https://lists.example/new');
  expect(name).toHaveValue('New list');
  failed = false;
  fireEvent.keyDown(name, { key: 'Enter' });
  await screen.findByText('New list');
  expect(url).toHaveValue('');
  expect(name).toHaveValue('');
  expect(writes.map(({ path, method, body }) => ({ path, method, body }))).toEqual(
    Array.from({ length: 2 }, () => ({
      path: '/api/blocklists',
      method: 'POST',
      body: { url: 'https://lists.example/new', alias: 'New list', enabled: true },
    })),
  );
});
it('retains a failed rename for retry and supports cancellation, refresh, interval, toggle and deletion', async () => {
  mount();
  await screen.findByText('Primary');
  fireEvent.click(screen.getByRole('button', { name: 'Rename list' }));
  const editor = screen.getByDisplayValue('Primary');
  fireEvent.change(editor, { target: { value: 'Changed' } });
  failed = true;
  fireEvent.click(screen.getByRole('button', { name: 'Save' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
  expect(editor).toHaveValue('Changed');
  failed = false;
  fireEvent.keyDown(editor, { key: 'Enter' });
  await screen.findByText('Changed');
  expect(lists[0]?.alias).toBe('Changed');
  fireEvent.click(screen.getByRole('button', { name: 'Rename list' }));
  fireEvent.keyDown(screen.getByDisplayValue('Changed'), { key: 'Escape' });
  expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole('combobox'), { target: { value: '43200' } });
  await waitFor(() => {
    expect(lists[0]?.refresh_interval).toBe(43200);
  });
  fireEvent.click(screen.getByRole('checkbox'));
  await waitFor(() => {
    expect(lists[0]?.enabled).toBe(false);
  });
  fireEvent.click(screen.getByRole('button', { name: 'Refresh list' }));
  await waitFor(() => {
    expect(writes.some((w) => w.path === '/api/blocklists/1/refresh')).toBe(true);
  });
  fireEvent.click(screen.getByRole('button', { name: 'Delete list' }));
  expect(await screen.findByText('No blocklists configured')).toBeInTheDocument();
});
it('filters history by list and domain then browses current and historical domains with URL state', async () => {
  mount();
  await screen.findByText('Primary');
  fireEvent.click(screen.getByRole('button', { name: 'History & Changelog' }));
  await screen.findByText('+ added.example');
  const search = screen.getByPlaceholderText("Search domain history (e.g. 'tiktok.com')...");
  fireEvent.change(search, { target: { value: 'removed.example' } });
  expect(screen.getByText('- removed.example')).toBeInTheDocument();
  expect(screen.queryByText('Secondary')).not.toBeInTheDocument();
  fireEvent.change(screen.getByRole('combobox'), { target: { value: '1' } });
  fireEvent.change(search, { target: { value: 'missing.example' } });
  expect(screen.getByText('No history entries match your filters')).toBeInTheDocument();
  fireEvent.click(screen.getByRole('button', { name: 'List Details' }));
  expect(screen.getByText('Select a list to browse domains')).toBeInTheDocument();
  fireEvent.change(screen.getByRole('combobox'), { target: { value: '1' } });
  await screen.findByText('current.example');
  fireEvent.change(screen.getByDisplayValue('Current'), { target: { value: '10' } });
  await screen.findByText('checkpoint.example');
  expect(screen.getByText('Viewing historical checkpoint')).toBeInTheDocument();
  fireEvent.change(screen.getByPlaceholderText('Search domains within list...'), {
    target: { value: 'none' },
  });
  await screen.findByText('No matching domains');
  expect(screen.getByLabelText('location')).toHaveTextContent('tab=details&list=1&search=none');
  fireEvent.click(screen.getByRole('button', { name: 'Published Lists' }));
  expect(screen.getByLabelText('location')).toHaveTextContent('?tab=lists');
});
