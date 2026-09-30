import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import type { EntityResult, PolicyResult } from '../api/generated';
import ApplicationStatus from '../components/ApplicationStatus';
import { AppStateProvider } from '../hooks/AppStateProvider';
import { call, reply, unavailable, type Call } from '../test/http';
import Analysis from './Analysis';
const fetchMock = vi.fn<typeof fetch>();
let requests: Call[];
let failure: boolean;
let empty: boolean;
const source: EntityResult = {
  name: 'Home',
  tier: 'range',
  result: 'block',
  published_list: { list_id: 1, list_name: 'Ads', rule: 'ads.example', action: 'block' },
};
const group: EntityResult = {
  name: 'Friends',
  tier: 'group',
  result: 'allow',
  custom_rule: { rule: 'good.example', action: 'allow' },
};
const client: EntityResult = {
  name: 'Laptop',
  tier: 'ip',
  result: 'allow',
  custom_rule: { rule: 'good.example', action: 'allow' },
};
const policies: PolicyResult[] = [
  {
    domain: 'ads.example',
    client_ip: '10.0.0.5',
    record_type: 1,
    result: 'block',
    result_source: source,
    range_evaluation: { result: 'block', result_source: source, entities: [source] },
    group_evaluation: { result: 'allow', result_source: group, entities: [group] },
    ip_evaluation: { result: 'allow', result_source: client, entities: [client] },
  },
  {
    domain: 'good.example',
    client_ip: '10.0.0.5',
    record_type: 1,
    result: 'allow',
    result_source: client,
    range_evaluation: { result: '', entities: [{ name: 'Home', tier: 'range', result: '' }] },
    group_evaluation: { result: '', entities: [{ name: 'Friends', tier: 'group', result: '' }] },
    ip_evaluation: { result: 'allow', result_source: client, entities: [client] },
  },
  { domain: 'implicit.example', client_ip: '10.0.0.5', record_type: 1, result: 'allow' },
];
beforeEach(() => {
  requests = [];
  failure = false;
  empty = false;
  fetchMock.mockImplementation((input, init) => {
    const req = call(input, init);
    requests.push(req);
    if (req.path === '/api/clients')
      return reply([
        {
          ip_address: '10.0.0.5',
          alias: 'Laptop',
          is_member: false,
          last_seen: '',
          query_count: 4,
        },
      ]);
    if (failure) return unavailable();
    if (req.path === '/api/analysis/domains')
      return reply({
        source: req.query.get('source'),
        count: empty ? 0 : 2,
        domains: empty ? [] : ['ads.example', 'good.example'],
      });
    if (req.path === '/api/analysis/matrix')
      return reply({
        record_type: 1,
        domains: ['ads.example', 'good.example'],
        lists: [
          { id: 1, alias: 'Ads', domain_count: 2 },
          { id: 2, alias: 'Privacy', domain_count: 1 },
        ],
        matrix: [
          [true, false],
          [false, false],
        ],
        matched_rules: [
          ['ads.example', null],
          [null, null],
        ],
      });
    if (req.path === '/api/analysis/simulate')
      return reply({ client_ip: '10.0.0.5', results: policies });
    throw Error('Unexpected ' + req.path);
  });
  vi.stubGlobal('fetch', fetchMock);
});
afterEach(() => vi.unstubAllGlobals());
function mount() {
  render(
    <AppStateProvider>
      <MemoryRouter>
        <ApplicationStatus />
        <Analysis />
      </MemoryRouter>
    </AppStateProvider>,
  );
}
function domainInput() {
  const field = document.querySelector('textarea');
  if (!(field instanceof HTMLTextAreaElement)) throw Error('Missing domains');
  return field;
}
async function chooseClient() {
  const input = screen.getByPlaceholderText('Search clients...');
  fireEvent.focus(input);
  fireEvent.change(input, { target: { value: 'Laptop' } });
  await screen.findByText('Laptop (10.0.0.5)');
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(input).toHaveValue('10.0.0.5');
}
it('loads each domain source, preserves input on empty data and uses selected client/window', async () => {
  mount();
  await chooseClient();
  for (const name of ['Top 1k sites', 'Top Blocked', 'Recent Queries', 'Top 250 Local']) {
    fireEvent.click(screen.getByRole('button', { name }));
    await waitFor(() => {
      expect(domainInput()).toHaveValue('ads.example\ngood.example');
      expect(screen.getByRole('button', { name })).toBeEnabled();
    });
  }
  empty = true;
  fireEvent.change(domainInput(), { target: { value: 'keep.example' } });
  fireEvent.click(screen.getByRole('button', { name: 'Recent Queries' }));
  await waitFor(() => {
    expect(screen.getByRole('button', { name: 'Recent Queries' })).toBeEnabled();
  });
  expect(domainInput()).toHaveValue('keep.example');
  empty = false;
  fireEvent.change(screen.getByRole('combobox', { name: 'Traffic window' }), {
    target: { value: '24h' },
  });
  fireEvent.click(screen.getByRole('button', { name: 'Load Domains' }));
  await waitFor(() => {
    expect(domainInput()).toHaveValue('ads.example\ngood.example');
  });
  const request = [...requests].reverse().find((r) => r.path === '/api/analysis/domains');
  expect(Object.fromEntries(request?.query ?? [])).toEqual({
    source: 'client-history',
    client_ip: '10.0.0.5',
    window: '24h',
    limit: '250',
  });
  failure = true;
  fireEvent.click(screen.getByRole('button', { name: 'Your Top 250' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
  expect(domainInput()).toHaveValue('ads.example\ngood.example');
});
it('runs the matrix with trimmed nonempty domains and retries a failed run without losing input', async () => {
  mount();
  fireEvent.change(domainInput(), { target: { value: '  ads.example \n\ngood.example  ' } });
  failure = true;
  fireEvent.click(screen.getByRole('button', { name: 'Run Matrix' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
  expect(domainInput()).toHaveValue('  ads.example \n\ngood.example  ');
  expect(screen.getByRole('button', { name: 'Run Matrix' })).toBeEnabled();
  failure = false;
  fireEvent.click(screen.getByRole('button', { name: 'Run Matrix' }));
  await screen.findByText('Privacy');
  expect(requests.filter((r) => r.path === '/api/analysis/matrix').map((r) => r.body)).toEqual([
    { domains: ['ads.example', 'good.example'], blocklist_ids: [], type: 'A' },
    { domains: ['ads.example', 'good.example'], blocklist_ids: [], type: 'A' },
  ]);
  expect(screen.getAllByText('ads.example').length).toBeGreaterThan(0);
  fireEvent.change(domainInput(), { target: { value: ' \n ' } });
  const before = requests.length;
  fireEvent.click(screen.getByRole('button', { name: 'Run Matrix' }));
  expect(requests).toHaveLength(before);
});
it('simulates all policy tiers and displays the winning source plus implicit allow', async () => {
  mount();
  fireEvent.click(screen.getByRole('button', { name: 'Policy Simulator' }));
  expect(screen.getByRole('button', { name: 'Simulate Policy' })).toBeDisabled();
  await chooseClient();
  fireEvent.change(domainInput(), {
    target: { value: 'ads.example\ngood.example\nimplicit.example' },
  });
  failure = true;
  fireEvent.click(screen.getByRole('button', { name: 'Simulate Policy' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
  failure = false;
  fireEvent.click(screen.getByRole('button', { name: 'Simulate Policy' }));
  expect(await screen.findByText('Implicit Allow')).toBeInTheDocument();
  expect(screen.getByText('Tier 1: Network')).toBeInTheDocument();
  expect(screen.getByText('Tier 3: Client')).toBeInTheDocument();
  expect(screen.getByText('Rule: ads.example (Ads)')).toBeInTheDocument();
  expect([...requests].reverse().find((r) => r.path === '/api/analysis/simulate')?.body).toEqual({
    client_ip: '10.0.0.5',
    domains: ['ads.example', 'good.example', 'implicit.example'],
    type: 'A',
  });
  fireEvent.click(screen.getByRole('button', { name: 'Blocklist Matrix' }));
  expect(screen.getByRole('button', { name: 'Run Matrix' })).toBeInTheDocument();
});

it('sends the selected DNS record type with matrix and simulator requests', async () => {
  mount();
  fireEvent.change(screen.getByLabelText('Record type'), { target: { value: 'TXT' } });
  fireEvent.click(screen.getByRole('button', { name: 'Run Matrix' }));
  await waitFor(() => {
    expect(requests.find((r) => r.path === '/api/analysis/matrix')?.body).toMatchObject({
      type: 'TXT',
    });
  });
  await chooseClient();
  fireEvent.click(screen.getByRole('button', { name: 'Policy Simulator' }));
  fireEvent.click(screen.getByRole('button', { name: 'Simulate Policy' }));
  await waitFor(() => {
    expect(requests.find((r) => r.path === '/api/analysis/simulate')?.body).toMatchObject({
      type: 'TXT',
    });
  });
});
