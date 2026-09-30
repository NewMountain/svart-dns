import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError, get, request } from './client';
import {
  parseGetApiBlocklistsData,
  parseGetApiQueryLogsData,
  parseInvestigationView,
  parsePutApiClientsIpAliasBody,
} from './generated';
import {
  deleteApiClientsIpBlockDomainDomain,
  postApiBlocklistsIdToggle,
  postApiGroups,
} from './operations';

const fetchMock = vi.fn<typeof fetch>();
beforeEach(() => {
  vi.stubGlobal('fetch', fetchMock);
  fetchMock.mockReset();
});
afterEach(() => vi.unstubAllGlobals());
const respond = (body: unknown, status = 200) =>
  fetchMock.mockResolvedValueOnce(
    new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json' } }),
  );
const list = {
  id: 9,
  alias: 'Example',
  url: 'https://example.com/list.txt',
  enabled: true,
  domain_count: 42,
  compatibility: {
    assessed: false,
    assessed_at: '',
    lines: 0,
    applied: 0,
    unsupported: 0,
    invalid: 0,
    diagnostic_count: 0,
  },
  last_updated: '2026-01-01T00:00:00Z',
  refresh_interval: 60,
};

describe('generated API transport', () => {
  it('mirrors optional null and unknown request fields accepted by the server', () => {
    expect(parsePutApiClientsIpAliasBody({ alias: null, future: true })).toEqual({
      alias: null,
      future: true,
    });
  });
  it('decodes complete records and preserves empty collections', async () => {
    respond({ data: [list], error: null });
    expect(await get('/api/blocklists', parseGetApiBlocklistsData)).toEqual([list]);
    respond({ data: [], error: null });
    expect(await get('/api/blocklists', parseGetApiBlocklistsData)).toEqual([]);
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/blocklists',
      expect.objectContaining({ credentials: 'same-origin', cache: 'no-store' }),
    );
  });
  it.each([
    { data: [{ ...list, domain_count: '42' }], error: null },
    { data: [{ id: 9, alias: 'Example' }], error: null },
    { data: [{ ...list, enabled: null }], error: null },
    { data: {}, error: null },
    { error: null },
    null,
    { data: [list], error: 'partial result', error_code: 'unavailable' },
    { data: null, error: 'missing code' },
  ])('rejects malformed envelopes or record fields: %j', async (body) => {
    respond(body);
    await expect(get('/api/blocklists', parseGetApiBlocklistsData)).rejects.toThrow(
      /Invalid API response/,
    );
  });
  it('accepts newer response fields without dropping them', async () => {
    const newer = { ...list, future: { nested: [1, true, null] } };
    respond({ data: [newer], error: null, envelope_extension: true });
    expect(await get('/api/blocklists', parseGetApiBlocklistsData)).toEqual([newer]);
  });
  it('rejects an HTTP failure even when it contains success-shaped data', async () => {
    respond({ data: [list], error: null }, 503);
    await expect(get('/api/blocklists', parseGetApiBlocklistsData)).rejects.toThrow(
      'Invalid API response',
    );
  });
  it('preserves the machine code and safe message without decoding partial data', async () => {
    respond(
      { data: null, error: 'Data unavailable; retry the request', error_code: 'unavailable' },
      503,
    );
    await expect(get('/api/blocklists', parseGetApiBlocklistsData)).rejects.toEqual(
      new ApiError(503, 'Data unavailable; retry the request', 'unavailable'),
    );
  });
  it('uses the generated mutation method, path encoding, and request shape', async () => {
    respond({ data: { id: 7, name: 'Guest devices' }, error: null }, 201);
    expect(await postApiGroups({ name: 'Guest devices' })).toEqual({
      id: 7,
      name: 'Guest devices',
    });
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/groups',
      expect.objectContaining({ method: 'POST', body: '{"name":"Guest devices"}' }),
    );
    respond({ data: { success: true }, error: null });
    await deleteApiClientsIpBlockDomainDomain('2001:db8::1', 'ad.example/path');
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/clients/2001%3Adb8%3A%3A1/block-domain/ad.example%2Fpath',
      expect.objectContaining({ method: 'DELETE' }),
    );
    respond({ data: { success: true }, error: null });
    await postApiBlocklistsIdToggle(9);
    expect(fetchMock).toHaveBeenLastCalledWith(
      '/api/blocklists/9/toggle',
      expect.objectContaining({ method: 'POST' }),
    );
  });
  it('requires the complete query log page rather than accepting a caller-selected subset', () => {
    expect(() => parseGetApiQueryLogsData({ logs: [] })).toThrow('Invalid API response');
    expect(parseGetApiQueryLogsData({ logs: [], total: 0, limit: 20, offset: 0 })).toEqual({
      logs: [],
      total: 0,
      limit: 20,
      offset: 0,
    });
  });
  it('preserves nested SQL JSON values and rejects non-row results', () => {
    const result = {
      columns: ['nested', 'nullable'],
      rows: [[{ labels: ['example', 7], enabled: true }, null]],
      row_count: 1,
      duration_ms: 3,
    };
    expect(parseInvestigationView(result)).toEqual(result);
    expect(() => parseInvestigationView({ ...result, rows: { misleading: 'partial' } })).toThrow(
      'Invalid API response',
    );
  });
});

it.each<HeadersInit>([
  { Authorization: 'Bearer fixture', 'Content-Type': 'application/custom' },
  [
    ['Authorization', 'Bearer fixture'],
    ['Content-Type', 'application/custom'],
  ],
  new Headers({ Authorization: 'Bearer fixture', 'Content-Type': 'application/custom' }),
])('preserves caller headers for every Fetch-supported input shape', async (headers) => {
  respond({ data: [], error: null });
  await request('/api/blocklists', parseGetApiBlocklistsData, { headers });
  const sent = new Headers(fetchMock.mock.calls[0]?.[1]?.headers);
  expect([...sent.entries()]).toEqual([
    ['authorization', 'Bearer fixture'],
    ['content-type', 'application/custom'],
  ]);
});
