import { act, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import ApplicationStatus from '../components/ApplicationStatus';
import { AppStateProvider } from '../hooks/AppStateProvider';
import Config from './Config';

const fetchMock = vi.fn<typeof fetch>();
const success = (data: unknown) =>
  Promise.resolve(new Response(JSON.stringify({ data, error: null }), { status: 200 }));
let settings: Record<string, string>;
let settingsRead: (() => Promise<Response>) | undefined;
let upstreams: { id: number; upstream: string; enabled: boolean }[];
let servers: { id: number; server: string }[];
let failures: Set<string>;
let writes: { path: string; method: string; body: unknown }[];
function readBody(body: BodyInit | null | undefined): Record<string, unknown> {
  const parsed: unknown = typeof body === 'string' ? JSON.parse(body) : {};
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
    throw new Error('Invalid request body');
  return Object.fromEntries(Object.entries(parsed));
}
beforeEach(() => {
  settingsRead = undefined;
  settings = {
    timezone: 'UTC',
    cache_ttl: '120',
    denied_ttl: '300',
    strategy: 'weighted',
    bootstrap_ttl: '86400',
    logging_enabled: 'true',
    log_retention_days: '730',
  };
  upstreams = [
    { id: 1, upstream: 'https://resolver.example/dns-query', enabled: true },
    { id: 2, upstream: 'tls://resolver.example', enabled: false },
    { id: 3, upstream: '9.9.9.9:853', enabled: true },
  ];
  servers = [];
  failures = new Set();
  writes = [];
  fetchMock.mockImplementation((input, init) => {
    const path = typeof input === 'string' ? input : input instanceof URL ? input.href : input.url;
    const method = init?.method ?? 'GET';
    if (method === 'GET') {
      if (path === '/api/settings') return settingsRead ? settingsRead() : success(settings);
      if (path === '/api/upstreams') return success(upstreams);
      if (path === '/api/bootstrap') return success(servers);
    }
    const body = readBody(init?.body);
    writes.push({ path, method, body });
    if (failures.has(path))
      return Promise.resolve(
        new Response(
          JSON.stringify({ data: null, error: 'Storage unavailable', error_code: 'unavailable' }),
          { status: 503 },
        ),
      );
    if (path.startsWith('/api/settings/')) {
      const key = path.slice('/api/settings/'.length);
      if (typeof body.value !== 'string') throw new Error('Missing value');
      settings = { ...settings, [key]: body.value };
      return success({ key, updated: true });
    } else if (path === '/api/upstreams') {
      if (typeof body.upstream !== 'string') throw new Error('Missing upstream');
      const created = { id: 4, upstream: body.upstream, enabled: true };
      upstreams = [...upstreams, created];
      return success(created);
    } else if (path === '/api/upstreams/1/toggle')
      upstreams = upstreams.map((u) => (u.id === 1 ? { ...u, enabled: !u.enabled } : u));
    else if (path === '/api/upstreams/4') upstreams = upstreams.filter((u) => u.id !== 4);
    else if (path === '/api/bootstrap' && method === 'POST') {
      if (typeof body.server !== 'string') throw new Error('Missing server');
      servers = [...servers, { id: 1, server: body.server }];
    } else if (path === '/api/bootstrap' && method === 'PUT') {
      if (
        !Array.isArray(body.servers) ||
        !body.servers.every((v: unknown): v is string => typeof v === 'string')
      )
        throw new Error('Missing servers');
      servers = body.servers.map((server, index) => ({ id: index + 1, server }));
    } else if (path !== '/api/cache/clear') throw new Error(`Unexpected request ${method} ${path}`);
    return success({ success: true });
  });
  vi.stubGlobal('fetch', fetchMock);
});
afterEach(() => vi.unstubAllGlobals());
function mount() {
  return render(
    <AppStateProvider>
      <MemoryRouter>
        <ApplicationStatus />
        <Config />
      </MemoryRouter>
    </AppStateProvider>,
  );
}
function card(title: string) {
  const element = screen.getByText(title).closest('.config-card');
  if (!(element instanceof HTMLElement)) throw new Error('Missing card');
  return within(element);
}

it('edits only changed preferences, preserves failed values, and retries a successful save', async () => {
  mount();
  const ttl = await screen.findByDisplayValue('120');
  fireEvent.change(ttl, { target: { value: '600' } });
  failures.add('/api/settings/cache_ttl');
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  expect(await screen.findByText('Error: Storage unavailable')).toBeInTheDocument();
  expect(ttl).toHaveValue(600);
  expect(writes).toEqual([
    { path: '/api/settings/cache_ttl', method: 'PUT', body: { value: '600' } },
  ]);
  failures.clear();
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  expect(await screen.findByText('Submitted changes saved')).toBeInTheDocument();
  expect(settings.cache_ttl).toBe('600');
  expect(writes).toHaveLength(2);
  fireEvent.change(screen.getByDisplayValue('UTC'), { target: { value: 'America/Los_Angeles' } });
  fireEvent.change(screen.getByDisplayValue('300'), { target: { value: '90' } });
  fireEvent.change(screen.getByDisplayValue('Weighted Random (Performance)'), {
    target: { value: 'blended' },
  });
  fireEvent.change(screen.getByDisplayValue('86400'), { target: { value: '900' } });
  fireEvent.change(screen.getByDisplayValue('730'), { target: { value: '7' } });
  fireEvent.click(screen.getByRole('checkbox'));
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  await waitFor(() => {
    expect(settings).toEqual({
      timezone: 'America/Los_Angeles',
      cache_ttl: '600',
      denied_ttl: '90',
      strategy: 'blended',
      bootstrap_ttl: '900',
      logging_enabled: 'false',
      log_retention_days: '7',
    });
  });
});

it('adds, toggles and removes upstreams, retaining failed input for retry', async () => {
  mount();
  await screen.findByText('https://resolver.example/dns-query');
  const section = card('Upstream DNS Servers'),
    input = section.getByRole('textbox');
  fireEvent.click(section.getByRole('button', { name: 'Add' }));
  expect(writes).toHaveLength(0);
  fireEvent.change(input, { target: { value: '1.1.1.1:53' } });
  failures.add('/api/upstreams');
  fireEvent.keyDown(input, { key: 'Enter' });
  expect(await screen.findByRole('alert')).toHaveTextContent('Storage unavailable');
  expect(input).toHaveValue('1.1.1.1:53');
  failures.clear();
  fireEvent.click(section.getByRole('button', { name: 'Add' }));
  await section.findByText('1.1.1.1:53');
  expect(input).toHaveValue('');
  fireEvent.click(
    section.getAllByRole('button', { name: 'Disable upstream' })[0] ??
      section.getByRole('button', { name: 'missing' }),
  );
  await waitFor(() => {
    expect(upstreams[0]?.enabled).toBe(false);
  });
  const added = section.getByText('1.1.1.1:53').closest('.upstream-row');
  if (!(added instanceof HTMLElement)) throw new Error('Missing upstream');
  fireEvent.click(within(added).getByRole('button', { name: 'Remove upstream' }));
  await waitFor(() => {
    expect(section.queryByText('1.1.1.1:53')).not.toBeInTheDocument();
  });
});

it('adds and removes bootstrap servers in order, and reports cache-clear failures', async () => {
  mount();
  await screen.findByText(
    'DoH/DoT upstreams require bootstrap servers to avoid circular DNS resolution',
  );
  const section = card('Bootstrap DNS'),
    input = section.getByRole('textbox');
  fireEvent.change(input, { target: { value: ' 9.9.9.9 ' } });
  fireEvent.keyDown(input, { key: 'Enter' });
  await section.findByText('9.9.9.9:53');
  expect(writes[0]).toEqual({
    path: '/api/bootstrap',
    method: 'POST',
    body: { server: '9.9.9.9:53' },
  });
  fireEvent.click(section.getByRole('button', { name: 'Remove server' }));
  await section.findByText('No bootstrap servers — DoH/DoT hostnames will use system resolver');
  expect(writes[1]).toEqual({ path: '/api/bootstrap', method: 'PUT', body: { servers: [] } });
  failures.add('/api/cache/clear');
  fireEvent.click(screen.getByRole('button', { name: 'Clear Cache' }));
  expect(await screen.findByText('Error: Storage unavailable')).toBeInTheDocument();
  failures.clear();
  fireEvent.click(screen.getByRole('button', { name: 'Clear Cache' }));
  expect(await screen.findByText('Cache cleared')).toBeInTheDocument();
});

it('preserves a new draft when an older save refresh arrives before the next save', async () => {
  mount();
  const strategy = await screen.findByDisplayValue('Weighted Random (Performance)');
  let release: ((response: Response) => void) | undefined;
  let snapshot = '';
  settingsRead = () => {
    snapshot = JSON.stringify({ data: settings, error: null });
    return new Promise<Response>((resolve) => {
      release = resolve;
    });
  };
  fireEvent.change(strategy, { target: { value: 'blended' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  await waitFor(() => {
    expect(release).toBeDefined();
  });
  fireEvent.change(strategy, { target: { value: 'random' } });
  expect(strategy).toHaveValue('random');
  expect(strategy).toBeEnabled();
  settingsRead = undefined;
  await act(async () => {
    if (!release) throw new Error('Settings refresh was not requested');
    release(new Response(snapshot, { status: 200 }));
    await Promise.resolve();
  });
  expect(strategy).toHaveValue('random');
  await waitFor(() => {
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled();
  });
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  await waitFor(() => {
    expect(settings.strategy).toBe('random');
  });
  expect(writes.filter((write) => write.path === '/api/settings/strategy')).toEqual([
    { path: '/api/settings/strategy', method: 'PUT', body: { value: 'blended' } },
    { path: '/api/settings/strategy', method: 'PUT', body: { value: 'random' } },
  ]);
});

it('keeps edits entered during an in-flight save and describes only the submitted changes as saved', async () => {
  mount();
  const ttl = await screen.findByDisplayValue('120');
  const original = fetchMock.getMockImplementation();
  if (!original) throw new Error('Missing fixture handler');
  let release: () => void = () => undefined;
  const pending = new Promise<void>((resolve) => {
    release = resolve;
  });
  let delayed = false;
  fetchMock.mockImplementation(async (input, init) => {
    const response = await original(input, init);
    if (input === '/api/settings/cache_ttl' && !delayed) {
      delayed = true;
      await pending;
    }
    return response;
  });
  fireEvent.change(ttl, { target: { value: '600' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  await waitFor(() => {
    expect(delayed).toBe(true);
  });
  fireEvent.change(ttl, { target: { value: '900' } });
  await act(async () => {
    release();
    await pending;
  });
  expect(await screen.findByText('Submitted changes saved')).toBeInTheDocument();
  expect(ttl).toHaveValue(900);
  expect(settings.cache_ttl).toBe('600');
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  await waitFor(() => {
    expect(settings.cache_ttl).toBe('900');
  });
});

it('reverts a draft to the current server value without writing a stale edit', async () => {
  mount();
  const ttl = await screen.findByDisplayValue('120');
  fireEvent.change(ttl, { target: { value: '600' } });
  fireEvent.change(ttl, { target: { value: '120' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  expect(await screen.findByText('Submitted changes saved')).toBeInTheDocument();
  expect(ttl).toHaveValue(120);
  expect(settings.cache_ttl).toBe('120');
  expect(writes).toEqual([]);
});

it('retains all draft values after a partial save fails and retries the remaining values', async () => {
  mount();
  const ttl = await screen.findByDisplayValue('120');
  const denied = screen.getByDisplayValue('300');
  fireEvent.change(ttl, { target: { value: '600' } });
  fireEvent.change(denied, { target: { value: '90' } });
  failures.add('/api/settings/denied_ttl');
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  expect(await screen.findByText('Error: Storage unavailable')).toBeInTheDocument();
  expect(settings.cache_ttl).toBe('600');
  expect(settings.denied_ttl).toBe('300');
  expect(ttl).toHaveValue(600);
  expect(denied).toHaveValue(90);
  failures.clear();
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  expect(await screen.findByText('Submitted changes saved')).toBeInTheDocument();
  expect(settings.cache_ttl).toBe('600');
  expect(settings.denied_ttl).toBe('90');
  expect(writes).toEqual([
    { path: '/api/settings/cache_ttl', method: 'PUT', body: { value: '600' } },
    { path: '/api/settings/denied_ttl', method: 'PUT', body: { value: '90' } },
    { path: '/api/settings/cache_ttl', method: 'PUT', body: { value: '600' } },
    { path: '/api/settings/denied_ttl', method: 'PUT', body: { value: '90' } },
  ]);
});

it('keeps a reverted newer edit across an older refresh and a later in-flight save', async () => {
  settings = { ...settings, strategy: 'blended' };
  let releaseRead: ((response: Response) => void) | undefined;
  let oldSnapshot = '';
  settingsRead = () => {
    oldSnapshot = JSON.stringify({ data: settings, error: null });
    return new Promise<Response>((resolve) => {
      releaseRead = resolve;
    });
  };
  mount();
  const strategy = screen.getByDisplayValue('Weighted Random (Performance)');
  await waitFor(() => {
    expect(releaseRead).toBeDefined();
  });
  settingsRead = undefined;
  const original = fetchMock.getMockImplementation();
  if (!original) throw new Error('Missing fixture handler');
  let releaseWrite: () => void = () => undefined;
  const pendingWrite = new Promise<void>((resolve) => {
    releaseWrite = resolve;
  });
  let writePending = false;
  fetchMock.mockImplementation(async (input, init) => {
    const response = await original(input, init);
    if (input === '/api/settings/strategy' && readBody(init?.body).value === 'random') {
      writePending = true;
      await pendingWrite;
    }
    return response;
  });
  fireEvent.change(strategy, { target: { value: 'random' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  await waitFor(() => {
    expect(writePending).toBe(true);
  });
  fireEvent.change(strategy, { target: { value: 'blended' } });
  await act(async () => {
    if (!releaseRead) throw new Error('Missing held settings response');
    releaseRead(new Response(oldSnapshot, { status: 200 }));
    await Promise.resolve();
  });
  await act(async () => {
    releaseWrite();
    await pendingWrite;
  });
  await waitFor(() => {
    expect(screen.getByRole('button', { name: 'Save Changes' })).toBeEnabled();
  });
  expect(settings.strategy).toBe('random');
  expect(strategy).toHaveValue('blended');
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  await waitFor(() => {
    expect(settings.strategy).toBe('blended');
  });
});

it('retains submitted values when confirmation fails or returns a different value', async () => {
  mount();
  const ttl = await screen.findByDisplayValue('120');
  fireEvent.change(ttl, { target: { value: '600' } });
  settingsRead = () =>
    Promise.resolve(
      new Response(
        JSON.stringify({ data: null, error: 'Read unavailable', error_code: 'unavailable' }),
        { status: 503 },
      ),
    );
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  expect(await screen.findByText('Error: Read unavailable')).toBeInTheDocument();
  expect(ttl).toHaveValue(600);
  settingsRead = () => success({ ...settings, cache_ttl: '120' });
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  expect(
    await screen.findByText(
      'Error: Saved value for cache_ttl could not be verified; retry the retained draft',
    ),
  ).toBeInTheDocument();
  expect(ttl).toHaveValue(600);
  settingsRead = undefined;
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  expect(await screen.findByText('Submitted changes saved')).toBeInTheDocument();
  expect(settings.cache_ttl).toBe('600');
});

it('keeps the confirmed saved value visible while any follow-up refresh is held', async () => {
  mount();
  const ttl = await screen.findByDisplayValue('120');
  let reads = 0;
  settingsRead = () => {
    reads += 1;
    if (reads === 1) return success(settings);
    return new Promise<Response>(() => undefined);
  };
  fireEvent.change(ttl, { target: { value: '600' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  expect(await screen.findByText('Submitted changes saved')).toBeInTheDocument();
  expect(settings.cache_ttl).toBe('600');
  expect(ttl).toHaveValue(600);
  expect(reads).toBe(1);
});

it('does not replace confirmed settings with an earlier resource response', async () => {
  let release: ((response: Response) => void) | undefined;
  const snapshot = JSON.stringify({ data: settings, error: null });
  settingsRead = () =>
    new Promise<Response>((resolve) => {
      release = resolve;
    });
  mount();
  const field = screen.getByText('Cache TTL (Seconds)').closest('.form-group');
  if (!(field instanceof HTMLElement)) throw new Error('Missing cache TTL field');
  const ttl = within(field).getByRole('spinbutton');
  await waitFor(() => {
    expect(release).toBeDefined();
  });
  settingsRead = undefined;
  fireEvent.change(ttl, { target: { value: '600' } });
  fireEvent.click(screen.getByRole('button', { name: 'Save Changes' }));
  expect(await screen.findByText('Submitted changes saved')).toBeInTheDocument();
  expect(ttl).toHaveValue(600);
  await act(async () => {
    if (!release) throw new Error('Missing earlier response');
    release(new Response(snapshot, { status: 200 }));
    await Promise.resolve();
  });
  expect(ttl).toHaveValue(600);
  expect(settings.cache_ttl).toBe('600');
});
