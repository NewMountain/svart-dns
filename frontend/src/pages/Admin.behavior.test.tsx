import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import * as schema from '../api/generated';
import ApplicationStatus from '../components/ApplicationStatus';
import { AppStateProvider } from '../hooks/AppStateProvider';
import Admin from './Admin';

const success = (data: unknown) => Response.json({ data, error: null });
type Write = { path: string; method: string; body: unknown };
function server(role = 'admin') {
  let tokens: schema.APIToken[] = [
    {
      id: 1,
      name: 'Metrics',
      token_prefix: 'sv_old',
      role: 'readonly',
      created_at: '2026-01-01T00:00:00Z',
    },
  ];
  let users: schema.AdminUser[] = [
    { id: 1, username: 'owner', role: 'admin', created_at: '2026-01-01T00:00:00Z' },
    { id: 2, username: 'guest', role: 'readonly', created_at: '' },
  ];
  let peers: schema.PeerView[] = [];
  const settings: Record<string, string> = { node_name: 'fixture-node', sync_interval: '5s' };
  const writes: Write[] = [];
  let rejectNext = false;
  vi.stubGlobal(
    'fetch',
    vi.fn((path: string, options?: RequestInit) => {
      const method = options?.method ?? 'GET';
      if (method === 'GET') {
        switch (path) {
          case '/api/tokens':
            return Promise.resolve(success(tokens));
          case '/api/users':
            return Promise.resolve(success(users));
          case '/api/auth/check':
            return Promise.resolve(success({ authenticated: true, username: 'owner', role }));
          case '/api/peers':
            return Promise.resolve(
              success({
                node_id: 'fixture-node-id',
                node_name: settings.node_name,
                sync_interval: settings.sync_interval,
                has_secret: true,
                tls_configured: true,
                sync_tls_ready: true,
                replicate_identity: false,
                peers,
              }),
            );
          default:
            throw new Error(`Unexpected GET ${path}`);
        }
      }
      const body: unknown = typeof options?.body === 'string' ? JSON.parse(options.body) : null;
      writes.push({ path, method, body });
      if (rejectNext) {
        rejectNext = false;
        return Promise.resolve(
          Response.json(
            { data: null, error: 'Storage unavailable', error_code: 'unavailable' },
            { status: 503 },
          ),
        );
      }
      if (path === '/api/tokens' && method === 'POST') {
        const input = schema.parsePostApiTokensBody(body);
        const token = {
          id: 3,
          name: input.name,
          role: input.role ?? 'readonly',
          token: 'sv_created_fixture',
        };
        tokens.push({
          id: token.id,
          name: token.name,
          role: token.role,
          token_prefix: 'sv_created',
          created_at: '2026-01-02T00:00:00Z',
        });
        return Promise.resolve(success(token));
      }
      if (path.startsWith('/api/tokens/') && method === 'DELETE') {
        tokens = tokens.filter((t) => path !== `/api/tokens/${String(t.id)}`);
        return Promise.resolve(success({ success: true }));
      }
      if (path === '/api/users' && method === 'POST') {
        const input = schema.parsePostApiUsersBody(body);
        const user = {
          id: 3,
          username: input.username,
          role: input.role ?? 'admin',
          created_at: '2026-01-02T00:00:00Z',
        };
        users.push(user);
        return Promise.resolve(success(user));
      }
      if (path.startsWith('/api/users/')) {
        if (method === 'DELETE') users = users.filter((u) => path !== `/api/users/${String(u.id)}`);
        else {
          const input = schema.parsePutApiUsersIdBody(body);
          users = users.map((u) =>
            path === `/api/users/${String(u.id)}` ? { ...u, role: input.role ?? u.role } : u,
          );
        }
        return Promise.resolve(success({ success: true }));
      }
      if (path === '/api/peers/pair')
        return Promise.resolve(
          success({
            pairing_code: 'fixture-pair-code',
            self_url: 'https://dns.example',
            node_id: 'fixture-node-id',
            expires_in: '10m',
          }),
        );
      if (path === '/api/peers/confirm')
        return Promise.resolve(
          success({ peer_url: 'https://peer.example', remote_node: 'peer-fixture', success: true }),
        );
      if (path === '/api/peers') {
        const input = schema.parsePostApiPeersBody(body);
        peers.push({
          url: input.url,
          healthy: true,
          last_sync_at: '',
          consecutive_errors: 0,
          changes_24h: 0,
        });
        return Promise.resolve(success({ success: true }));
      }
      if (path.startsWith('/api/peers/')) {
        peers = peers.filter((p) => path !== `/api/peers/${encodeURIComponent(p.url)}`);
        return Promise.resolve(success({ success: true }));
      }
      if (path.startsWith('/api/settings/')) {
        const key = path.replace('/api/settings/', '');
        const input = schema.parsePutApiSettingsKeyBody(body);
        if (typeof input.value !== 'string') throw new Error('Missing setting value');
        if (key !== 'sync_secret') settings[key] = input.value;
        return Promise.resolve(
          success(key === 'sync_secret' ? { key, updated: true } : { key, value: input.value }),
        );
      }
      throw new Error(`Unexpected mutation ${method} ${path}`);
    }),
  );
  return {
    writes,
    failNext: () => {
      rejectNext = true;
    },
    read: () => structuredClone({ tokens, users, peers, settings }),
  };
}
function show() {
  return render(
    <AppStateProvider>
      <ApplicationStatus />
      <Admin />
    </AppStateProvider>,
  );
}
function row(text: string) {
  const element = screen.getByText(text).closest('tr');
  if (!element) throw new Error(`Missing row ${text}`);
  return within(element);
}
afterEach(() => {
  vi.unstubAllGlobals();
});

describe('Admin through generated HTTP operations', () => {
  it('retains a failed token draft, reveals exactly once, copies, and confirms revocation', async () => {
    const api = server();
    const copy = vi.fn(() => Promise.resolve());
    vi.stubGlobal('navigator', { clipboard: { writeText: copy } });
    show();
    await screen.findByText('Metrics');
    expect(screen.getByText('sv_old...')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'New Token' }));
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }));
    expect(api.writes).toEqual([]);
    fireEvent.change(screen.getByPlaceholderText('e.g. Grafana Dashboard'), {
      target: { value: 'Automation' },
    });
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'admin' } });
    api.failNext();
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
    expect(screen.getByPlaceholderText('e.g. Grafana Dashboard')).toHaveValue('Automation');
    expect(api.read().tokens).toHaveLength(1);
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss error' }));
    fireEvent.click(screen.getByRole('button', { name: 'Generate' }));
    await screen.findByText('sv_created_fixture');
    fireEvent.click(screen.getByRole('button', { name: 'Copy' }));
    expect(copy).toHaveBeenCalledWith('sv_created_fixture');
    fireEvent.click(screen.getByRole('button', { name: 'I have copied it' }));
    expect(screen.queryByText('sv_created_fixture')).not.toBeInTheDocument();
    await screen.findByText('Automation');
    fireEvent.click(row('Automation').getByRole('button', { name: 'Revoke token' }));
    fireEvent.click(row('Automation').getByRole('button', { name: 'No' }));
    expect(api.writes).toHaveLength(2);
    fireEvent.click(row('Automation').getByRole('button', { name: 'Revoke token' }));
    api.failNext();
    fireEvent.click(row('Automation').getByRole('button', { name: 'Yes' }));
    await screen.findByRole('alert');
    expect(row('Automation').getByRole('button', { name: 'Yes' })).toBeInTheDocument();
    expect(api.read().tokens).toHaveLength(2);
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss error' }));
    fireEvent.click(row('Automation').getByRole('button', { name: 'Yes' }));
    await waitFor(() => {
      expect(screen.queryByText('Automation')).not.toBeInTheDocument();
    });
    expect(api.read().tokens.map((t) => t.name)).toEqual(['Metrics']);
    expect(api.writes).toEqual([
      { method: 'POST', path: '/api/tokens', body: { name: 'Automation', role: 'admin' } },
      { method: 'POST', path: '/api/tokens', body: { name: 'Automation', role: 'admin' } },
      { method: 'DELETE', path: '/api/tokens/3', body: null },
      { method: 'DELETE', path: '/api/tokens/3', body: null },
    ]);
  });

  it('preserves failed user edits and applies creation, role/password update and confirmed deletion', async () => {
    const api = server();
    show();
    await screen.findByText('guest');
    expect(row('owner').queryByRole('button', { name: 'Delete user' })).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'New User' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    expect(api.writes).toEqual([]);
    fireEvent.change(screen.getByPlaceholderText('e.g. sam'), { target: { value: 'operator' } });
    fireEvent.change(screen.getByPlaceholderText('Enter password'), {
      target: { value: 'disposable-example-password' },
    });
    fireEvent.change(screen.getByRole('combobox'), { target: { value: 'readonly' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));
    await screen.findByText('operator');
    fireEvent.click(row('operator').getByRole('button', { name: 'Edit user' }));
    fireEvent.change(row('operator').getByRole('combobox'), { target: { value: 'admin' } });
    fireEvent.change(screen.getByPlaceholderText('New password (optional)'), {
      target: { value: 'replacement-example-password' },
    });
    api.failNext();
    fireEvent.click(row('operator').getByRole('button', { name: 'Save' }));
    await screen.findByRole('alert');
    expect(screen.getByPlaceholderText('New password (optional)')).toHaveValue(
      'replacement-example-password',
    );
    expect(row('operator').getByRole('combobox')).toHaveValue('admin');
    expect(api.read().users.find((u) => u.username === 'operator')?.role).toBe('readonly');
    fireEvent.click(screen.getByRole('button', { name: 'Dismiss error' }));
    fireEvent.click(row('operator').getByRole('button', { name: 'Save' }));
    await waitFor(() => {
      expect(row('operator').queryByRole('combobox')).not.toBeInTheDocument();
    });
    expect(row('operator').getByText('Admin')).toBeInTheDocument();
    fireEvent.click(row('operator').getByRole('button', { name: 'Delete user' }));
    fireEvent.click(row('operator').getByRole('button', { name: 'No' }));
    fireEvent.click(row('operator').getByRole('button', { name: 'Delete user' }));
    fireEvent.click(row('operator').getByRole('button', { name: 'Yes' }));
    await waitFor(() => {
      expect(screen.queryByText('operator')).not.toBeInTheDocument();
    });
    expect(api.read().users.map((u) => u.username)).toEqual(['owner', 'guest']);
    expect(api.writes).toEqual([
      {
        method: 'POST',
        path: '/api/users',
        body: { username: 'operator', password: 'disposable-example-password', role: 'readonly' },
      },
      {
        method: 'PUT',
        path: '/api/users/3',
        body: { role: 'admin', password: 'replacement-example-password' },
      },
      {
        method: 'PUT',
        path: '/api/users/3',
        body: { role: 'admin', password: 'replacement-example-password' },
      },
      { method: 'DELETE', path: '/api/users/3', body: null },
    ]);
  });

  it('keeps failed pairing inputs, retries, and adds/removes the exact encoded peer URL', async () => {
    const api = server();
    show();
    await screen.findByText('Shared Secret');
    fireEvent.click(screen.getByRole('button', { name: 'Pair' }));
    expect(await screen.findByDisplayValue('fixture-pair-code')).toBeInTheDocument();
    expect(screen.getByDisplayValue('https://dns.example')).toHaveAttribute('readonly');
    fireEvent.click(screen.getByRole('button', { name: 'Done' }));
    fireEvent.click(screen.getByRole('button', { name: 'Confirm Pair' }));
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(api.writes).toHaveLength(1);
    fireEvent.change(screen.getByPlaceholderText('https://192.168.1.3:3000'), {
      target: { value: 'https://peer.example' },
    });
    fireEvent.change(screen.getByPlaceholderText('Paste pairing code'), {
      target: { value: 'fixture-remote-code' },
    });
    api.failNext();
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await screen.findByText('Pairing failed: Storage unavailable');
    expect(screen.getByPlaceholderText('Paste pairing code')).toHaveValue('fixture-remote-code');
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(screen.queryByPlaceholderText('Paste pairing code')).not.toBeInTheDocument();
    });
    fireEvent.click(screen.getByRole('button', { name: 'Manual' }));
    fireEvent.change(screen.getByPlaceholderText('https://192.0.2.10:443'), {
      target: { value: 'https://peer.example:8443' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Add' }));
    await screen.findByText('peer.example:8443');
    fireEvent.click(row('peer.example:8443').getByRole('button', { name: 'Remove peer' }));
    fireEvent.click(row('peer.example:8443').getByRole('button', { name: 'No' }));
    fireEvent.click(row('peer.example:8443').getByRole('button', { name: 'Remove peer' }));
    fireEvent.click(row('peer.example:8443').getByRole('button', { name: 'Yes' }));
    await waitFor(() => {
      expect(screen.queryByText('peer.example:8443')).not.toBeInTheDocument();
    });
    expect(api.read().peers).toEqual([]);
    expect(api.writes).toEqual([
      { method: 'POST', path: '/api/peers/pair', body: null },
      {
        method: 'POST',
        path: '/api/peers/confirm',
        body: { peer_url: 'https://peer.example', pairing_code: 'fixture-remote-code' },
      },
      {
        method: 'POST',
        path: '/api/peers/confirm',
        body: { peer_url: 'https://peer.example', pairing_code: 'fixture-remote-code' },
      },
      { method: 'POST', path: '/api/peers', body: { url: 'https://peer.example:8443' } },
      { method: 'DELETE', path: '/api/peers/https%3A%2F%2Fpeer.example%3A8443', body: null },
    ]);
  });
});

it('preserves failed settings edits, commits exact values, and never reveals the configured secret', async () => {
  const api = server();
  show();
  const secret = await screen.findByPlaceholderText('Configured — enter a new secret to rotate');
  expect(secret).toHaveValue('');
  expect(secret).toHaveAttribute('type', 'password');
  fireEvent.keyDown(secret, { key: 'Enter' });
  fireEvent.change(secret, { target: { value: 'too-short' } });
  fireEvent.keyDown(secret, { key: 'Enter' });
  expect(api.writes).toEqual([]);
  fireEvent.change(secret, { target: { value: 'fixture-secret-0123456789abcdef0123456789' } });
  api.failNext();
  fireEvent.keyDown(secret, { key: 'Enter' });
  expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
  expect(secret).toHaveValue('fixture-secret-0123456789abcdef0123456789');
  fireEvent.click(screen.getByRole('button', { name: 'Dismiss error' }));
  fireEvent.keyDown(secret, { key: 'Enter' });
  await waitFor(() => {
    expect(secret).toHaveValue('');
  });
  const name = screen.getByPlaceholderText('e.g. svart-basement');
  fireEvent.focus(name);
  fireEvent.change(name, { target: { value: ' fixture-renamed ' } });
  api.failNext();
  fireEvent.keyDown(name, { key: 'Enter' });
  await screen.findByRole('alert');
  expect(name).toHaveValue(' fixture-renamed ');
  expect(api.read().settings.node_name).toBe('fixture-node');
  fireEvent.click(screen.getByRole('button', { name: 'Dismiss error' }));
  fireEvent.keyDown(name, { key: 'Enter' });
  await waitFor(() => {
    expect(name).toHaveValue('fixture-renamed');
  });
  const interval = screen.getByPlaceholderText('2s');
  fireEvent.focus(interval);
  fireEvent.change(interval, { target: { value: '15s' } });
  fireEvent.keyDown(interval, { key: 'Enter' });
  await waitFor(() => {
    expect(api.read().settings.sync_interval).toBe('15s');
  });
  expect(api.read().settings).toEqual({ node_name: 'fixture-renamed', sync_interval: '15s' });
  expect(api.writes).toEqual([
    {
      method: 'PUT',
      path: '/api/settings/sync_secret',
      body: { value: 'fixture-secret-0123456789abcdef0123456789' },
    },
    {
      method: 'PUT',
      path: '/api/settings/sync_secret',
      body: { value: 'fixture-secret-0123456789abcdef0123456789' },
    },
    { method: 'PUT', path: '/api/settings/node_name', body: { value: 'fixture-renamed' } },
    { method: 'PUT', path: '/api/settings/node_name', body: { value: 'fixture-renamed' } },
    { method: 'PUT', path: '/api/settings/sync_interval', body: { value: '15s' } },
  ]);
});

it('uses authenticated readonly identity without displaying shared-secret controls', async () => {
  const api = server('readonly');
  show();
  await screen.findByText('(you)');
  expect(screen.queryByText('Shared Secret')).not.toBeInTheDocument();
  expect(
    screen.queryByPlaceholderText('Configured — enter a new secret to rotate'),
  ).not.toBeInTheDocument();
  expect(screen.getByPlaceholderText('2s')).toHaveValue('5s');
  expect(api.writes).toEqual([]);
});
