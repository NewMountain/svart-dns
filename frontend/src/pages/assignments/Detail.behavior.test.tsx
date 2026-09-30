import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import ApplicationStatus from '../../components/ApplicationStatus';
import type { Group } from '../../api/generated';
import { AppStateProvider } from '../../hooks/AppStateProvider';
import { call, reply, unavailable, type Call } from '../../test/http';
import { PolicyDetailView } from './BundleDetail';
import { ClientDetailView } from './ClientDetail';
import { GroupDetailView } from './GroupDetail';
import { RangeDetailView } from './NetworkDetail';
const fetchMock = vi.fn<typeof fetch>(),
  deleted = vi.fn();
let requests: Call[], failed: boolean, blocked: string[], allowed: string[], assigned: boolean;
let membershipGroups: Group[];
function detail() {
  return {
    id: 1,
    ip: '10.0.0.5',
    name: 'Home',
    alias: 'Laptop',
    description: 'Local devices',
    cidr: '10.0.0.0/24',
    created_at: '',
    first_seen: '',
    last_seen: '',
    total_queries: 4,
    avg_latency_microseconds: 1000,
    blocklist_stats: { unique_total: 0, lists: [] },
    blocklists: [
      {
        id: 9,
        alias: 'Ads',
        url: 'https://lists.example/ads',
        domain_count: 5,
        is_assigned: assigned,
        source: 'direct',
      },
    ],
    allowlists: [],
    custom_blocked: blocked,
    custom_allowed: allowed,
    groups: membershipGroups,
    members: [],
    recent_logs: [],
    assigned_to: { ranges: [], groups: [], clients: [] },
  };
}
beforeEach(() => {
  requests = [];
  membershipGroups = [];
  failed = false;
  blocked = ['existing.example'];
  allowed = ['safe.example'];
  assigned = false;
  deleted.mockReset();
  fetchMock.mockImplementation((input, init) => {
    const req = call(input, init);
    requests.push(req);
    if (req.method === 'GET') {
      if (req.path === '/api/query-logs')
        return reply({ logs: [], total: 0, offset: 0, limit: 20 });
      return reply(detail());
    }
    if (failed) return unavailable();
    if (req.path.includes('/block-domain')) {
      if (req.method === 'POST' && typeof req.body.domain === 'string')
        blocked = [...blocked, req.body.domain];
      if (req.method === 'DELETE')
        blocked = blocked.filter(
          (d) => d !== decodeURIComponent(req.path.split('/').slice(-1)[0] ?? ''),
        );
    } else if (req.path.includes('/allow-domain')) {
      if (req.method === 'POST' && typeof req.body.domain === 'string')
        allowed = [...allowed, req.body.domain];
      if (req.method === 'DELETE')
        allowed = allowed.filter(
          (d) => d !== decodeURIComponent(req.path.split('/').slice(-1)[0] ?? ''),
        );
    } else if (req.path.includes('/blocklists/')) assigned = req.method === 'POST';
    return reply({ success: true });
  });
  vi.stubGlobal('fetch', fetchMock);
});
afterEach(() => vi.unstubAllGlobals());
function mount(kind: string) {
  const props = { id: '1', policies: [], onDelete: deleted };
  render(
    <AppStateProvider>
      <ApplicationStatus />
      {kind === 'range' ? (
        <RangeDetailView {...props} />
      ) : kind === 'group' ? (
        <GroupDetailView {...props} clients={[]} />
      ) : kind === 'client' ? (
        <ClientDetailView ip="10.0.0.5" policies={[]} />
      ) : (
        <PolicyDetailView id="1" onDelete={deleted} />
      )}
    </AppStateProvider>,
  );
}
it.each(['range', 'group', 'client', 'bundle'])(
  '%s custom rules preserve rejected input, read back success and keep rejected deletions visible',
  async (kind) => {
    mount(kind);
    await screen.findByText('existing.example');
    const input = screen.getByPlaceholderText('domain.com');
    fireEvent.change(input, { target: { value: 'new.example' } });
    failed = true;
    fireEvent.keyDown(input, { key: 'Enter' });
    expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
    expect(input).toHaveValue('new.example');
    expect(screen.queryByText('new.example')).not.toBeInTheDocument();
    failed = false;
    const rules = input.closest('.widget');
    if (!(rules instanceof HTMLElement)) throw new Error('Missing custom rules widget');
    fireEvent.click(within(rules).getByRole('button', { name: 'Add' }));
    await screen.findByText('new.example');
    expect(input).toHaveValue('');
    failed = true;
    fireEvent.click(
      screen.getByRole('button', { name: 'Remove existing.example custom block rule' }),
    );
    await waitFor(() => {
      expect(requests.filter((r) => r.method === 'DELETE')).toHaveLength(1);
    });
    expect(screen.getByText('existing.example')).toBeInTheDocument();
    failed = false;
    fireEvent.click(
      screen.getByRole('button', { name: 'Remove existing.example custom block rule' }),
    );
    await waitFor(() => {
      expect(screen.queryByText('existing.example')).not.toBeInTheDocument();
    });
    fireEvent.change(screen.getByDisplayValue('Block'), { target: { value: 'allow' } });
    fireEvent.change(input, { target: { value: 'new-safe.example' } });
    fireEvent.keyDown(input, { key: 'Enter' });
    await screen.findByText('new-safe.example');
    fireEvent.click(screen.getByRole('button', { name: 'Remove safe.example custom allow rule' }));
    await waitFor(() => {
      expect(screen.queryByText('safe.example')).not.toBeInTheDocument();
    });
    const base =
      kind === 'client'
        ? '/api/clients/10.0.0.5'
        : `/api/${kind === 'range' ? 'ranges' : kind === 'group' ? 'groups' : 'policies'}/1`;
    expect(
      requests.filter((r) => r.method === 'POST').map((r) => ({ path: r.path, body: r.body })),
    ).toEqual([
      { path: base + '/block-domain', body: { domain: 'new.example' } },
      { path: base + '/block-domain', body: { domain: 'new.example' } },
      { path: base + '/allow-domain', body: { domain: 'new-safe.example' } },
    ]);
  },
);
it.each(['range', 'group', 'client', 'bundle'])(
  '%s assigned list reflects success only and supports removal',
  async (kind) => {
    mount(kind);
    await screen.findByText('existing.example');
    const toggle = screen.getByRole('checkbox');
    failed = true;
    fireEvent.click(toggle);
    expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
    expect(toggle).not.toBeChecked();
    failed = false;
    fireEvent.click(toggle);
    await waitFor(() => {
      expect(toggle).toBeChecked();
    });
    fireEvent.click(toggle);
    await waitFor(() => {
      expect(toggle).not.toBeChecked();
    });
  },
);
it.each(['range', 'group', 'bundle'])(
  '%s delete stays open on failure and calls navigation only after successful deletion',
  async (kind) => {
    mount(kind);
    await screen.findByText('existing.example');
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));
    expect(deleted).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
    failed = true;
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
    expect(deleted).not.toHaveBeenCalled();
    expect(screen.getByRole('button', { name: 'Confirm' })).toBeInTheDocument();
    failed = false;
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => {
      expect(deleted).toHaveBeenCalledTimes(1);
    });
  },
);

it('shows no group chips when client detail lists groups the client has not joined', async () => {
  membershipGroups = [
    { id: 1, name: "Chris' Devices", is_member: false, member_count: 4, blocklist_count: 1 },
    { id: 2, name: 'Dev', is_member: false, member_count: 2, blocklist_count: 1 },
  ];
  mount('client');
  await screen.findByText('Laptop');
  expect(screen.queryByText("Chris' Devices")).not.toBeInTheDocument();
  expect(screen.queryByText('Dev')).not.toBeInTheDocument();
  expect(screen.getByText('Not in any groups')).toBeInTheDocument();
});
it('shows only actual memberships in the client Groups widget', async () => {
  membershipGroups = [
    { id: 1, name: "Chris' Devices", is_member: true, member_count: 4, blocklist_count: 1 },
    { id: 2, name: 'Dev', is_member: false, member_count: 2, blocklist_count: 1 },
  ];
  mount('client');
  expect(await screen.findByText("Chris' Devices")).toBeInTheDocument();
  expect(screen.queryByText('Dev')).not.toBeInTheDocument();
  expect(screen.queryByText('Not in any groups')).not.toBeInTheDocument();
});
