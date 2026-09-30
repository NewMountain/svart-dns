import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AppStateProvider } from '../hooks/AppStateProvider';
import { call, reply } from '../test/http';
import { ClientDetailView } from './Tiers';
const fetchMock = vi.fn<typeof fetch>();
const clientDetail = {
  ip: '10.42.60.128',
  alias: "Chris' MacBook",
  total_queries: 10,
  avg_latency_microseconds: 1000,
  first_seen: '2026-08-22T08:00:00Z',
  last_seen: '2026-08-22T09:00:00Z',
  groups: [],
  blocklists: [],
  custom_blocked: ['costco'],
  custom_allowed: ['data.orders.costco.com'],
  blocklist_stats: { unique_total: 0, lists: [] },
};

describe('ClientDetailView custom rules', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.stubGlobal('fetch', fetchMock);
    fetchMock.mockImplementation((input, init) => {
      const request = call(input, init);
      if (request.path.startsWith('/api/query-logs'))
        return reply({ logs: [], total: 0, limit: 20, offset: 0 });
      if (request.method === 'GET') return reply(clientDetail);
      return Promise.resolve(
        new Response(
          JSON.stringify({
            data: null,
            error: 'domain required in path',
            error_code: 'invalid_request',
          }),
          { status: 400 },
        ),
      );
    });
  });

  afterEach(() => vi.unstubAllGlobals());
  it('preserves a rule and reports the API error when deletion fails', async () => {
    render(
      <AppStateProvider>{<ClientDetailView ip="10.42.60.128" policies={[]} />}</AppStateProvider>,
    );

    await screen.findByText('costco');
    fireEvent.click(screen.getByRole('button', { name: 'Remove costco custom block rule' }));

    await waitFor(() => {
      expect(screen.getByText('costco')).toBeInTheDocument();
      expect(
        screen.getByText('Unable to delete custom rule: domain required in path'),
      ).toBeInTheDocument();
    });
    expect(fetchMock).toHaveBeenCalledWith(
      '/api/clients/10.42.60.128/block-domain/costco',
      expect.objectContaining({ method: 'DELETE' }),
    );
  });
});
