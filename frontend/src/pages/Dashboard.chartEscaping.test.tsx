import { AppStateProvider } from '../hooks/AppStateProvider';
/// <reference types="vite/client" />
import { render, waitFor } from '@testing-library/react';
import type { ApexOptions } from 'apexcharts';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import indexHtml from '../../index.html?raw';
import * as apiClient from '../api/client';
import Dashboard from './Dashboard';

// ApexCharts writes the legend text, the tooltip title (x category), the
// x-axis tooltip and the tooltip series name with innerHTML. Any of those can
// carry a DNS query name, a client alias, a list alias or an upstream URL,
// all of which a LAN device or a lower-privileged user controls.
const HOSTILE = '<img src=x onerror=alert(1)>.evil.example';
const ESCAPED = '&lt;img src=x onerror=alert(1)&gt;.evil.example';

const captured: { type: string; options: ApexOptions }[] = [];

vi.mock('react-apexcharts', () => ({
  default: ({ type, options }: { type: string; options: ApexOptions }) => {
    captured.push({ type, options });
    return <div data-testid={`chart-${type}`} />;
  },
}));

vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client');
  return { ...actual, get: vi.fn() };
});

const getMock = vi.mocked(apiClient.get);

function hostileResponse(path: string, _decode: unknown, params?: Record<string, string>) {
  switch (path) {
    case '/api/stats/timeseries':
      if (params?.buckets === '1') {
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
      return Promise.resolve([{ timestamp: '2026-03-29T10:00:00', queries: 100, blocked: 10 }]);
    case '/api/stats/latency':
      return Promise.resolve([
        { timestamp: '2026-03-29T10:00:00', avg_latency_us: 11000, max_latency_us: 17000 },
      ]);
    case '/api/stats/top-clients':
      return Promise.resolve([
        { client_ip: '10.42.1.42', alias: HOSTILE, allowed: 500, blocked: 100 },
      ]);
    case '/api/stats/block-sources':
      return Promise.resolve([{ list_name: HOSTILE, count: 120, percentage: 100 }]);
    case '/api/stats/upstream-usage':
      return Promise.resolve([{ upstream: HOSTILE, count: 200, percentage: 100 }]);
    case '/api/stats/top-domains':
      return Promise.resolve({ domains: [{ domain: HOSTILE, count: 100 }], total: 100 });
    case '/api/stats/servfails':
      return Promise.resolve([
        {
          client_ip: '10.42.1.42',
          alias: HOSTILE,
          count: 3,
          top_domains: [{ domain: HOSTILE, count: 3 }],
        },
      ]);
    case '/api/stats/system':
      return Promise.resolve([
        {
          timestamp: '2026-03-29T10:00:00',
          cpu_percent: 12.1,
          rss_bytes: 536870912,
          heap_alloc: 134217728,
          db_size_bytes: 67108864,
        },
      ]);
    case '/api/query-logs':
      return Promise.resolve({ logs: [], total: 0, limit: 20, offset: 0 });
    default:
      return Promise.reject(new Error(`unexpected api call: ${path}`));
  }
}

function htmlSinkFormatters(options: ApexOptions): Record<string, (() => unknown) | undefined> {
  const tooltip = options.tooltip;
  const yTooltip = Array.isArray(tooltip?.y) ? tooltip.y[0] : tooltip?.y;
  const opts = { seriesIndex: 0, dataPointIndex: 0, w: { globals: {} } };
  return {
    'legend.formatter': options.legend?.formatter
      ? () => options.legend?.formatter?.(HOSTILE, opts) ?? ''
      : undefined,
    'tooltip.x.formatter': tooltip?.x?.formatter
      ? () => {
          const result: unknown = tooltip.x?.formatter
            ? Reflect.apply(tooltip.x.formatter.bind(tooltip.x), tooltip.x, [HOSTILE, opts])
            : undefined;
          return result;
        }
      : undefined,
    'tooltip.y.title.formatter': yTooltip?.title?.formatter
      ? () => yTooltip.title?.formatter?.(HOSTILE, opts) ?? ''
      : undefined,
  };
}

describe('Dashboard chart text escaping (A6)', () => {
  beforeEach(() => {
    captured.length = 0;
    getMock.mockReset();
    getMock.mockImplementation(hostileResponse);
  });

  it('escapes every innerHTML-rendered chart string on every chart', async () => {
    render(
      <AppStateProvider>
        {
          <MemoryRouter initialEntries={['/?window=24h']}>
            <Dashboard />
          </MemoryRouter>
        }
      </AppStateProvider>,
    );
    await waitFor(() => {
      expect(captured.length).toBeGreaterThanOrEqual(10);
    });

    for (const { type, options } of captured) {
      for (const [name, formatter] of Object.entries(htmlSinkFormatters(options))) {
        expect(formatter, `${type} chart lacks ${name}`).toBeTypeOf('function');
        if (!formatter) throw new Error(`${type} chart lacks ${name}`);
        expect(formatter(), `${type} ${name}`).toBe(ESCAPED);
      }
    }
  });
});

describe('index.html meta CSP', () => {
  const csp = /http-equiv="Content-Security-Policy"\s+content="([^"]+)"/.exec(indexHtml)?.[1] ?? '';

  it('forbids form submissions to other origins', () => {
    expect(csp).toContain("form-action 'self'");
  });

  it('does not rely on frame-ancestors, which browsers ignore in a meta tag', () => {
    // Clickjacking protection is the server's Content-Security-Policy and
    // X-Frame-Options headers; a meta frame-ancestors only logs a console error.
    expect(csp).not.toContain('frame-ancestors');
  });
});
