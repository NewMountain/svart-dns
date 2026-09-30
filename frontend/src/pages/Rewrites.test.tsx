import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { RewriteView } from '../api/generated';
import ApplicationStatus from '../components/ApplicationStatus';
import { AppStateProvider } from '../hooks/AppStateProvider';
import Rewrites from './Rewrites';

const initial: RewriteView[] = [
  {
    id: 7,
    domain: 'nas.example',
    ip_addresses: '192.0.2.10 2001:db8::10',
    enabled: true,
  },
  {
    id: 9,
    domain: 'printer.example',
    ip_addresses: '192.0.2.20',
    enabled: false,
  },
];
type Write = { method: string; path: string; body: unknown };
const success = (data: unknown) => Response.json({ data, error: null });

function backend(seed: RewriteView[] = initial) {
  let rows = structuredClone(seed);
  const writes: Write[] = [];
  const failures: ('http' | 'network')[] = [];
  const fetch = vi.fn((path: string, options?: RequestInit): Promise<Response> => {
    const method = options?.method ?? 'GET';
    if (method === 'GET') {
      if (path === '/api/rewrites') return Promise.resolve(success(rows));
      if (path === '/api/rewrites/stats?window=24h')
        return Promise.resolve(
          success([
            {
              domain: 'nas.example',
              hits: 33,
              unique_clients: 2,
              top_clients: [
                { ip: '192.0.2.101', count: 21 },
                { ip: '192.0.2.102', count: 12 },
              ],
            },
          ]),
        );
      throw new Error(`Unexpected GET ${path}`);
    }
    const body: unknown = typeof options?.body === 'string' ? JSON.parse(options.body) : null;
    writes.push({ method, path, body });
    const failure = failures.shift();
    if (failure === 'network') return Promise.reject(new Error('Connection interrupted'));
    if (failure === 'http')
      return Promise.resolve(
        Response.json(
          { data: null, error: 'Storage unavailable', error_code: 'unavailable' },
          { status: 503 },
        ),
      );
    if (method === 'DELETE') {
      rows = rows.filter((row) => path !== `/api/rewrites/${String(row.id)}`);
      return Promise.resolve(success({ success: true }));
    }
    if (typeof body !== 'object' || body === null) throw new Error('Expected JSON object');
    if (method === 'POST') {
      if (
        !('domain' in body) ||
        typeof body.domain !== 'string' ||
        !('ip_addresses' in body) ||
        typeof body.ip_addresses !== 'string'
      )
        throw new Error('Missing rewrite fields');
      const created = {
        id: 11,
        domain: body.domain,
        ip_addresses: body.ip_addresses,
        enabled: true,
      };
      rows.push(created);
      return Promise.resolve(success(created));
    }
    const row = rows.find((entry) => path === `/api/rewrites/${String(entry.id)}`);
    if (!row) throw new Error('Unknown rewrite');
    if ('domain' in body && typeof body.domain === 'string') row.domain = body.domain;
    if ('ip_addresses' in body && typeof body.ip_addresses === 'string')
      row.ip_addresses = body.ip_addresses;
    if ('enabled' in body && typeof body.enabled === 'boolean') row.enabled = body.enabled;
    return Promise.resolve(success(row));
  });
  vi.stubGlobal('fetch', fetch);
  return {
    writes,
    read: () => structuredClone(rows),
    failNext: (failure: 'http' | 'network') => {
      failures.push(failure);
    },
  };
}

afterEach(() => {
  vi.unstubAllGlobals();
});

function rewriteRow(domain: string) {
  const row = screen.getByText(domain).closest('.rewrites-grid-row');
  if (!(row instanceof HTMLElement)) throw new Error(`Missing visible row ${domain}`);
  return within(row);
}

function renderWithStatus() {
  return render(
    <AppStateProvider>
      <ApplicationStatus />
      <Rewrites />
    </AppStateProvider>,
  );
}

describe('DNS rewrite controls through generated HTTP operations', () => {
  it.each(['http', 'network'] as const)(
    'preserves both edited values after %s failure and retries the same atomic save',
    async (failure) => {
      const server = backend();
      renderWithStatus();
      await screen.findByText('nas.example');
      fireEvent.click(rewriteRow('nas.example').getByRole('button', { name: 'Edit rewrite' }));
      fireEvent.change(screen.getByDisplayValue('nas.example'), {
        target: { value: 'storage.example' },
      });
      fireEvent.change(screen.getByDisplayValue('192.0.2.10 2001:db8::10'), {
        target: { value: '192.0.2.11 2001:db8::11' },
      });
      server.failNext(failure);
      fireEvent.click(screen.getByRole('button', { name: 'Save' }));
      expect(await screen.findByRole('alert')).toHaveTextContent(
        failure === 'http' ? 'Storage unavailable' : 'Connection interrupted',
      );
      expect(server.read()).toEqual(initial);
      expect(screen.getByDisplayValue('storage.example')).toBeInTheDocument();
      expect(screen.getByDisplayValue('192.0.2.11 2001:db8::11')).toBeInTheDocument();
      fireEvent.click(screen.getByRole('button', { name: 'Dismiss error' }));
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
      fireEvent.click(screen.getByRole('button', { name: 'Save' }));
      await screen.findByText('storage.example');
      const expected = {
        method: 'PUT',
        path: '/api/rewrites/7',
        body: { domain: 'storage.example', ip_addresses: '192.0.2.11 2001:db8::11' },
      };
      expect(server.writes).toEqual([expected, expected]);
      expect(server.read()).toEqual([
        { ...initial[0], domain: 'storage.example', ip_addresses: '192.0.2.11 2001:db8::11' },
        initial[1],
      ]);
      expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument();
    },
  );

  it('keeps a rejected new record draft and creates it exactly once on retry', async () => {
    const server = backend([]);
    renderWithStatus();
    await screen.findByText('No rewrites configured');
    fireEvent.change(screen.getByPlaceholderText('app.local'), {
      target: { value: 'router.example' },
    });
    fireEvent.change(screen.getByPlaceholderText('192.168.1.100'), {
      target: { value: '192.0.2.1 2001:db8::1' },
    });
    server.failNext('http');
    fireEvent.click(screen.getByRole('button', { name: '+ Add Record' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
    expect(server.read()).toEqual([]);
    expect(screen.getByPlaceholderText('app.local')).toHaveValue('router.example');
    expect(screen.getByPlaceholderText('192.168.1.100')).toHaveValue('192.0.2.1 2001:db8::1');
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss error' }));
    fireEvent.click(screen.getByRole('button', { name: '+ Add Record' }));
    await screen.findByText('router.example');
    const expected = {
      method: 'POST',
      path: '/api/rewrites',
      body: { domain: 'router.example', ip_addresses: '192.0.2.1 2001:db8::1', enabled: true },
    };
    expect(server.writes).toEqual([expected, expected]);
    expect(server.read()).toEqual([{ id: 11, ...expected.body }]);
    expect(screen.getByPlaceholderText('app.local')).toHaveValue('');
    expect(screen.getByPlaceholderText('192.168.1.100')).toHaveValue('');
  });

  it.each(['toggle', 'delete'] as const)(
    'preserves the selected record after rejected %s and permits retry',
    async (operation) => {
      const server = backend();
      renderWithStatus();
      await screen.findByText('nas.example');
      const control = () =>
        operation === 'toggle'
          ? rewriteRow('nas.example').getByRole('checkbox')
          : rewriteRow('nas.example').getByRole('button', { name: 'Delete rewrite' });
      server.failNext('http');
      fireEvent.click(control());
      expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
      expect(server.read()).toEqual(initial);
      expect(rewriteRow('nas.example').getByRole('checkbox')).toBeChecked();
      fireEvent.click(screen.getByRole('button', { name: 'Dismiss error' }));
      fireEvent.click(control());
      await waitFor(() => {
        if (operation === 'toggle')
          expect(rewriteRow('nas.example').getByRole('checkbox')).not.toBeChecked();
        else expect(screen.queryByText('nas.example')).not.toBeInTheDocument();
      });
      const expected =
        operation === 'toggle'
          ? { method: 'PUT', path: '/api/rewrites/7', body: { enabled: false } }
          : { method: 'DELETE', path: '/api/rewrites/7', body: null };
      expect(server.writes).toEqual([expected, expected]);
      expect(server.read()).toEqual(
        operation === 'toggle' ? [{ ...initial[0], enabled: false }, initial[1]] : [initial[1]],
      );
    },
  );
  it('saves both edited fields as one atomic request', async () => {
    const server = backend();
    render(<AppStateProvider>{<Rewrites />}</AppStateProvider>);
    await screen.findByText('nas.example');
    fireEvent.click(rewriteRow('nas.example').getByRole('button', { name: 'Edit rewrite' }));
    fireEvent.change(screen.getByDisplayValue('nas.example'), {
      target: { value: 'storage.example' },
    });
    fireEvent.change(screen.getByDisplayValue('192.0.2.10 2001:db8::10'), {
      target: { value: '192.0.2.11 2001:db8::11' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    await screen.findByText('storage.example');
    expect(server.writes).toEqual([
      {
        method: 'PUT',
        path: '/api/rewrites/7',
        body: {
          domain: 'storage.example',
          ip_addresses: '192.0.2.11 2001:db8::11',
        },
      },
    ]);
    expect(server.read()).toEqual([
      {
        id: 7,
        domain: 'storage.example',
        ip_addresses: '192.0.2.11 2001:db8::11',
        enabled: true,
      },
      initial[1],
    ]);
    expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument();
  });
  it('renders exact records, disabled state, complete client statistics and case/IP search', async () => {
    backend();
    render(<AppStateProvider>{<Rewrites />}</AppStateProvider>);
    await screen.findByText('nas.example');
    const nas = rewriteRow('nas.example');
    expect(nas.getByText('192.0.2.10 2001:db8::10')).toBeInTheDocument();
    expect(nas.getByRole('checkbox')).toBeChecked();
    expect(nas.getByText('33')).toBeInTheDocument();
    expect(nas.getByText('192.0.2.101')).toBeInTheDocument();
    expect(nas.getByText('21')).toBeInTheDocument();
    expect(nas.getByText('192.0.2.102')).toBeInTheDocument();
    expect(nas.getByText('12')).toBeInTheDocument();
    expect(rewriteRow('printer.example').getByRole('checkbox')).not.toBeChecked();
    expect(rewriteRow('printer.example').getAllByText('0')).toHaveLength(2);
    const search = screen.getByPlaceholderText('Search domains...');
    fireEvent.change(search, { target: { value: 'NAS.EXAMPLE' } });
    expect(screen.queryByText('printer.example')).not.toBeInTheDocument();
    fireEvent.change(search, { target: { value: '192.0.2.20' } });
    expect(screen.queryByText('nas.example')).not.toBeInTheDocument();
    expect(screen.getByText('printer.example')).toBeInTheDocument();
    fireEvent.change(search, { target: { value: 'missing.example' } });
    expect(screen.getByText('No matching rewrites')).toBeInTheDocument();
    fireEvent.change(search, { target: { value: '' } });
    expect(screen.getAllByRole('checkbox')).toHaveLength(2);
  });

  it.each(['app.local', '192.168.1.100'])(
    'adds complete IPv4/IPv6 record with Enter in %s and clears only after success',
    async (enterField) => {
      const server = backend([]);
      render(<AppStateProvider>{<Rewrites />}</AppStateProvider>);
      await screen.findByText('No rewrites configured');
      fireEvent.click(screen.getByRole('button', { name: '+ Add Record' }));
      expect(server.writes).toEqual([]);
      fireEvent.change(screen.getByPlaceholderText('app.local'), {
        target: { value: 'router.example' },
      });
      fireEvent.click(screen.getByRole('button', { name: '+ Add Record' }));
      expect(server.writes).toEqual([]);
      fireEvent.change(screen.getByPlaceholderText('192.168.1.100'), {
        target: { value: '192.0.2.1 2001:db8::1' },
      });
      fireEvent.keyDown(screen.getByPlaceholderText(enterField), {
        key: 'Enter',
      });
      await screen.findByText('router.example');
      expect(server.writes).toEqual([
        {
          method: 'POST',
          path: '/api/rewrites',
          body: {
            domain: 'router.example',
            ip_addresses: '192.0.2.1 2001:db8::1',
            enabled: true,
          },
        },
      ]);
      expect(screen.getByPlaceholderText('app.local')).toHaveValue('');
      expect(screen.getByPlaceholderText('192.168.1.100')).toHaveValue('');
      expect(server.read()).toEqual([
        {
          id: 11,
          domain: 'router.example',
          ip_addresses: '192.0.2.1 2001:db8::1',
          enabled: true,
        },
      ]);
    },
  );

  it('cancels an edit without writes and toggles/deletes only the selected record', async () => {
    const server = backend();
    render(<AppStateProvider>{<Rewrites />}</AppStateProvider>);
    await screen.findByText('nas.example');
    fireEvent.click(rewriteRow('nas.example').getByRole('button', { name: 'Edit rewrite' }));
    fireEvent.change(screen.getByDisplayValue('nas.example'), {
      target: { value: 'cancelled.example' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(server.writes).toEqual([]);
    expect(screen.getByText('nas.example')).toBeInTheDocument();
    fireEvent.click(rewriteRow('printer.example').getByRole('checkbox'));
    await waitFor(() => {
      expect(rewriteRow('printer.example').getByRole('checkbox')).toBeChecked();
    });
    fireEvent.click(rewriteRow('nas.example').getByRole('button', { name: 'Delete rewrite' }));
    await waitFor(() => {
      expect(screen.queryByText('nas.example')).not.toBeInTheDocument();
    });
    expect(server.writes).toEqual([
      { method: 'PUT', path: '/api/rewrites/9', body: { enabled: true } },
      { method: 'DELETE', path: '/api/rewrites/7', body: null },
    ]);
    expect(server.read()).toEqual([
      {
        id: 9,
        domain: 'printer.example',
        ip_addresses: '192.0.2.20',
        enabled: true,
      },
    ]);
  });
});
