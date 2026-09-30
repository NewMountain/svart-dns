import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import * as apiClient from '../api/client';
import { AppStateProvider } from '../hooks/AppStateProvider';
import {
  clientActivityTooltipFormatter,
  donutTooltipFormatter,
  escapeHtml,
} from '../lib/chartTooltips';
import Dashboard from './Dashboard';

vi.mock('react-apexcharts', () => ({
  default: ({ type }: { type: string }) => <div data-testid={`chart-${type}`} />,
}));

vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client');
  return {
    ...actual,
    get: vi.fn(),
  };
});

const getMock = vi.mocked(apiClient.get);

function dashboardResponse(path: string, _decode: unknown, params?: Record<string, string>) {
  if (path === '/api/stats/timeseries' && params?.buckets === '1') {
    return Promise.resolve({
      total_queries: 1234,
      blocked_queries: 246,
      allowed_queries: 988,
      avg_latency_microseconds: 12345,
      avg_latency_ms: 12,
      active_clients: 12,
      cache_hits: 654,
      servfail_count: 3,
      rewrite_count: 2,
      custom_blocks: 22,
      custom_allows: 11,
      rewrite_hits: 2,
    });
  }
  if (path === '/api/stats/timeseries') {
    return Promise.resolve([
      { timestamp: '2026-03-29T10:00:00', queries: 100, blocked: 10 },
      { timestamp: '2026-03-29T11:00:00', queries: 140, blocked: 14 },
    ]);
  }
  if (path === '/api/stats/latency') {
    return Promise.resolve([
      { timestamp: '2026-03-29T10:00:00', avg_latency_us: 11000, max_latency_us: 17000 },
      { timestamp: '2026-03-29T11:00:00', avg_latency_us: 12000, max_latency_us: 18000 },
    ]);
  }
  if (path === '/api/stats/top-clients') {
    return Promise.resolve([
      { client_ip: '10.42.1.42', alias: 'Chris Macbook', allowed: 500, blocked: 100 },
      { client_ip: '10.42.1.43', alias: 'Sam iPhone', allowed: 300, blocked: 50 },
    ]);
  }
  if (path === '/api/stats/block-sources') {
    return Promise.resolve([
      { list_name: 'Hagezi Pro', count: 120, percentage: 48.7 },
      { list_name: 'Custom', count: 60, percentage: 24.3 },
    ]);
  }
  if (path === '/api/stats/upstream-usage') {
    return Promise.resolve([
      { upstream: '9.9.9.9:53', count: 200, percentage: 60 },
      { upstream: 'tls://dns.quad9.net', count: 100, percentage: 30 },
    ]);
  }
  if (path === '/api/stats/top-domains' && params?.blocked === 'false') {
    return Promise.resolve({
      domains: [
        { domain: 'example.com.', count: 100 },
        { domain: 'github.com.', count: 60 },
      ],
      total: 250,
    });
  }
  if (path === '/api/stats/top-domains' && params?.blocked === 'true') {
    return Promise.resolve({
      domains: [
        { domain: 'ads.example.', count: 80 },
        { domain: 'tracker.example.', count: 40 },
      ],
      total: 140,
    });
  }
  if (path === '/api/stats/servfails') {
    return Promise.resolve([
      {
        client_ip: '10.42.1.42',
        alias: 'Chris Macbook',
        count: 3,
        top_domains: [{ domain: 'broken.example.', count: 3 }],
      },
    ]);
  }
  if (path === '/api/stats/system') {
    return Promise.resolve([
      {
        timestamp: '2026-03-29T10:00:00',
        cpu_percent: 12.1,
        rss_bytes: 536870912,
        heap_alloc: 134217728,
        db_size_bytes: 67108864,
      },
      {
        timestamp: '2026-03-29T10:00:30',
        cpu_percent: 18.4,
        rss_bytes: 603979776,
        heap_alloc: 150994944,
        db_size_bytes: 68157440,
      },
    ]);
  }
  if (path === '/api/query-logs') {
    return Promise.resolve({
      total: 1,
      limit: 20,
      offset: 0,
      logs: [
        {
          id: 1,
          timestamp: '2026-03-29T10:10:10',
          client_ip: '10.42.1.42',
          client_alias: 'Chris Macbook',
          query_name: 'example.com.',
          query_type: 'A',
          response_code: 'NOERROR',
          blocked: false,
          latency_microseconds: 1200,
          upstream: '9.9.9.9:53',
          result_reason: 'default_allow',
        },
      ],
    });
  }
  return Promise.reject(new Error(`unexpected api call: ${path} ${JSON.stringify(params)}`));
}

function renderDashboard(initialEntry = '/?window=24h') {
  return render(
    <AppStateProvider>
      {
        <MemoryRouter initialEntries={[initialEntry]}>
          <Dashboard />
        </MemoryRouter>
      }
    </AppStateProvider>,
  );
}

describe('Dashboard', () => {
  beforeEach(() => {
    getMock.mockReset();
    getMock.mockImplementation(dashboardResponse);
  });

  it('renders the key dashboard data and live query log', async () => {
    renderDashboard();

    expect(await screen.findByText('1,234')).toBeInTheDocument();
    expect(screen.getByText('Traffic Volume & Block Rate')).toBeInTheDocument();
    expect(screen.getByText('Live Query Log')).toBeInTheDocument();
    expect(await screen.findByText('example.com')).toBeInTheDocument();
    expect(screen.getByText('Chris Macbook')).toBeInTheDocument();
    expect(await screen.findAllByTestId(/chart-/)).not.toHaveLength(0);
  });

  it('refetches dashboard stats with the selected time window', async () => {
    renderDashboard();
    await screen.findByText('1,234');

    getMock.mockClear();

    fireEvent.click(screen.getByRole('button', { name: '5m' }));

    await waitFor(() => {
      expect(getMock).toHaveBeenCalledWith('/api/stats/timeseries', expect.any(Function), {
        window: '5m',
        buckets: '1',
      });
    });
    expect(getMock).toHaveBeenCalledWith('/api/stats/latency', expect.any(Function), {
      window: '5m',
      buckets: '24',
    });
    expect(getMock).toHaveBeenCalledWith('/api/stats/top-domains', expect.any(Function), {
      window: '5m',
      blocked: 'true',
    });
    expect(getMock).not.toHaveBeenCalledWith('/api/stats/dashboard', expect.anything());
  });
});

describe('tooltip HTML escaping (XSS hardening)', () => {
  const XSS_PAYLOAD = '<img src=x onerror=alert(1)>';

  it('escapes HTML metacharacters', () => {
    expect(escapeHtml(XSS_PAYLOAD)).toBe('&lt;img src=x onerror=alert(1)&gt;');
    expect(escapeHtml(`a & b "quote" 'apos'`)).toBe('a &amp; b &quot;quote&quot; &#39;apos&#39;');
  });

  it('escapes a malicious domain in the donut tooltip top-level label', () => {
    const formatter = donutTooltipFormatter([{ label: XSS_PAYLOAD, value: 42, color: '#a9dc76' }]);
    const html = formatter({ series: [42], seriesIndex: 0, w: {} });
    expect(html).not.toContain('<img');
    expect(html).toContain('&lt;img src=x onerror=alert(1)&gt;');
  });

  it('escapes a malicious domain nested in donut tooltip detail rows', () => {
    const formatter = donutTooltipFormatter([
      {
        label: 'Next 5',
        value: 100,
        color: '#3f3f46',
        details: [{ domain: XSS_PAYLOAD, count: 7 }],
      },
    ]);
    const html = formatter({ series: [100], seriesIndex: 0, w: {} });
    expect(html).not.toContain('<img');
    expect(html).toContain('&lt;img src=x onerror=alert(1)&gt;');
  });

  it('escapes a malicious client alias in the Top Client Activity tooltip', () => {
    const html = clientActivityTooltipFormatter({
      series: [[10], [5]],
      dataPointIndex: 0,
      w: { globals: { labels: [XSS_PAYLOAD] } },
    });
    expect(html).not.toContain('<img');
    expect(html).toContain('&lt;img src=x onerror=alert(1)&gt;');
  });
});
