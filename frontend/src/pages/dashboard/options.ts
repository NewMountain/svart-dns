import type { ApexOptions } from 'apexcharts';
import type { DashboardTopDomains as TopDomainsResponse } from '../../api/generated';
import { withEscapedChartText, type DonutSlice } from '../../lib/chartTooltips';
import '../../styles/pages/dashboard.css';
import '../../styles/pages/logs.css';

export const donutColors = [
  '#a9dc76',
  '#78dce8',
  '#ab9df2',
  '#fc9867',
  '#ffd866',
  '#ff6188',
  '#f4f4f5',
  '#71717a',
  '#e2e2e5',
  '#b4dfc4',
  '#c4b5fd',
  '#fca5a5',
  '#86efac',
  '#7dd3fc',
  '#d8b4fe',
];

export const commonOptions: ApexOptions = {
  theme: { mode: 'dark' },
  grid: { borderColor: '#27272a', strokeDashArray: 4 },
  dataLabels: { enabled: false },
  tooltip: { theme: 'dark', style: { fontFamily: 'JetBrains Mono' } },
};

export function buildDonutSlices(resp: TopDomainsResponse | null): DonutSlice[] {
  if (!resp || (resp.domains ?? []).length === 0) {
    return [];
  }

  const { domains, total } = resp;
  const slices: DonutSlice[] = [];

  for (const [index, domain] of (domains ?? []).slice(0, 10).entries()) {
    slices.push({
      label: domain.domain,
      value: domain.count,
      color: donutColors[index % donutColors.length] ?? '#a9dc76',
    });
  }

  const tierTwo = (domains ?? []).slice(10, 15);
  if (tierTwo.length > 0) {
    slices.push({
      label: `Next ${String(tierTwo.length)}`,
      value: tierTwo.reduce((sum, domain) => sum + domain.count, 0),
      color: '#3f3f46',
      details: tierTwo.map((domain) => ({ domain: domain.domain, count: domain.count })),
    });
  }

  const tierThree = (domains ?? []).slice(15, 25);
  if (tierThree.length > 0) {
    slices.push({
      label: `Next ${String(tierThree.length)}`,
      value: tierThree.reduce((sum, domain) => sum + domain.count, 0),
      color: '#27272a',
      details: tierThree.map((domain) => ({ domain: domain.domain, count: domain.count })),
    });
  }

  const topSum = (domains ?? []).reduce((sum, domain) => sum + domain.count, 0);
  const otherCount = total - topSum;
  if (otherCount > 0) {
    slices.push({ label: 'Other', value: otherCount, color: '#1f1f22' });
  }

  return slices;
}

export function buildChartOptions(
  animateCharts: boolean,
  chart: ApexOptions['chart'],
  extra: Omit<ApexOptions, 'chart'> = {},
): ApexOptions {
  const chartOptions = chart ?? {};
  const toolbar = { show: false, ...(chartOptions.toolbar ?? {}) };
  // Chart strings include DNS query names, client aliases, list aliases and
  // upstream URLs; every chart goes through withEscapedChartText.
  return withEscapedChartText({
    ...commonOptions,
    ...extra,
    chart: {
      background: 'transparent',
      animations: {
        enabled: animateCharts,
        speed: 600,
        dynamicAnimation: { enabled: animateCharts, speed: 300 },
      },
      fontFamily: 'Inter',
      toolbar,
      ...chartOptions,
    },
  });
}

export function formatTime(timestamp: string): string {
  return new Date(timestamp).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });
}
