import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AppStateProvider } from '../hooks/AppStateProvider';
import Dashboard from './Dashboard';

vi.mock('react-apexcharts', () => ({
  default: () => <div data-testid="available-chart" />,
}));

afterEach(() => vi.unstubAllGlobals());

function mountDashboard() {
  vi.stubGlobal('IntersectionObserver', undefined);
  return render(
    <AppStateProvider>
      <MemoryRouter>
        <Dashboard />
      </MemoryRouter>
    </AppStateProvider>,
  );
}

function card(title: string) {
  const element = screen.getByText(title).closest('.card');
  if (!(element instanceof HTMLElement)) throw new Error(`Missing card ${title}`);
  return within(element);
}

const zeroSummary = {
  total_queries: 0,
  blocked_queries: 0,
  allowed_queries: 0,
  avg_latency_microseconds: 0,
  avg_latency_ms: 0,
  active_clients: 0,
  cache_hits: 0,
  servfail_count: 0,
  rewrite_count: 0,
  custom_blocks: 0,
  custom_allows: 0,
  rewrite_hits: 0,
};

function successfulData(url: URL) {
  if (url.pathname === '/api/stats/timeseries' && url.searchParams.get('buckets') === '1')
    return zeroSummary;
  if (url.pathname === '/api/stats/top-domains') return { domains: [], total: 0 };
  if (url.pathname === '/api/query-logs') return { logs: [], total: 0, limit: 20, offset: 0 };
  return [];
}

function respond(data: unknown, status = 200) {
  return new Response(
    JSON.stringify({ data, error: status === 200 ? null : 'Database unavailable' }),
    {
      status,
      headers: { 'Content-Type': 'application/json' },
    },
  );
}

describe('dashboard availability', () => {
  it('shows unavailable panels and unknown totals after cold request failures', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(() => Promise.resolve(respond(null, 503))),
    );
    const { container } = mountDashboard();
    await waitFor(() => {
      expect(screen.getAllByText('Data unavailable.').length).toBeGreaterThan(10);
    });
    expect([...container.querySelectorAll('.stat-value')].map((node) => node.textContent)).toEqual([
      '—',
      '—',
      '—',
      '—',
    ]);
    expect(screen.queryByText('0 SERVFAIL')).not.toBeInTheDocument();
    expect(screen.queryByText('0% CACHE HIT')).not.toBeInTheDocument();
    expect(screen.queryByText('AVG 0.0ms')).not.toBeInTheDocument();
    expect(screen.queryByText('No failures')).not.toBeInTheDocument();
    expect(screen.queryByText('No custom overrides')).not.toBeInTheDocument();
    expect(card('Live Query Log').getByText('Data unavailable.')).toBeInTheDocument();
  });

  it('keeps successfully measured zero distinct from unavailable', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: string) =>
        Promise.resolve(respond(successfulData(new URL(input, 'http://localhost')))),
      ),
    );
    const { container } = mountDashboard();
    await waitFor(() => expect(screen.getByText('0 SERVFAIL')).toBeInTheDocument());
    expect([...container.querySelectorAll('.stat-value')].map((node) => node.textContent)).toEqual([
      '0',
      '0',
      '0.0ms',
      '0',
    ]);
    expect(screen.getByText('0% CACHE HIT')).toBeInTheDocument();
    expect(screen.getByText('No failures')).toBeInTheDocument();
    expect(screen.queryByText('Data unavailable.')).not.toBeInTheDocument();
  });

  it.each([[], null])(
    'retains a successful empty collection %j after refresh failure',
    async (emptyCollection) => {
      let failed = false;
      vi.stubGlobal(
        'fetch',
        vi.fn((input: string) =>
          Promise.resolve(
            failed
              ? respond(null, 503)
              : respond(
                  Array.isArray(successfulData(new URL(input, 'http://localhost')))
                    ? emptyCollection
                    : successfulData(new URL(input, 'http://localhost')),
                ),
          ),
        ),
      );
      mountDashboard();
      expect(await screen.findByText('No failures')).toBeInTheDocument();
      failed = true;
      fireEvent.click(screen.getByRole('button', { name: '1h' }));
      await waitFor(() =>
        expect(
          card('Resolution Failures').getByText('Refresh failed. Showing last available data.'),
        ).toBeInTheDocument(),
      );
      expect(card('Resolution Failures').getByText('No failures')).toBeInTheDocument();
      expect(card('Upstream Usage').getByText('No data')).toBeInTheDocument();
      expect(screen.queryByText('Data unavailable.')).not.toBeInTheDocument();
    },
  );

  it('keeps successful panels visible when a neighboring request fails', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((input: string) => {
        const url = new URL(input, 'http://localhost');
        if (url.pathname === '/api/stats/block-sources') return Promise.resolve(respond(null, 503));
        const data =
          url.pathname === '/api/stats/upstream-usage'
            ? [{ upstream: '192.0.2.53:53', count: 7, percentage: 100 }]
            : successfulData(url);
        return Promise.resolve(respond(data));
      }),
    );
    mountDashboard();
    await waitFor(() =>
      expect(card('Block Sources').getByText('Data unavailable.')).toBeInTheDocument(),
    );
    expect(await card('Upstream Usage').findByTestId('available-chart')).toBeInTheDocument();
    expect(card('Blocked vs Allowed').getByText('No data')).toBeInTheDocument();
  });
  it('marks retained results stale during refresh failure and clears the warning on recovery', async () => {
    let failed = false;
    let total = 42;
    vi.stubGlobal(
      'fetch',
      vi.fn((input: string) => {
        if (failed) return Promise.resolve(respond(null, 503));
        const url = new URL(input, 'http://localhost');
        let data: unknown = successfulData(url);
        if (url.pathname === '/api/stats/timeseries' && url.searchParams.get('buckets') === '1')
          data = { ...zeroSummary, total_queries: total, allowed_queries: total };
        if (url.pathname === '/api/stats/upstream-usage')
          data = [{ upstream: '192.0.2.53:53', count: total, percentage: 100 }];
        return Promise.resolve(respond(data));
      }),
    );
    mountDashboard();
    expect(await screen.findByText('42')).toBeInTheDocument();
    failed = true;
    fireEvent.click(screen.getByRole('button', { name: '1h' }));
    await waitFor(() =>
      expect(
        card('Upstream Usage').getByText('Refresh failed. Showing last available data.'),
      ).toBeInTheDocument(),
    );
    expect(card('Upstream Usage').getByTestId('available-chart')).toBeInTheDocument();
    expect(screen.getByText('42')).toBeInTheDocument();
    failed = false;
    total = 84;
    fireEvent.click(screen.getByRole('button', { name: '5m' }));
    expect(await screen.findByText('84')).toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.queryByText('Refresh failed. Showing last available data.'),
      ).not.toBeInTheDocument(),
    );
  });
});
