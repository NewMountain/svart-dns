import type { DashboardResourceStatus } from '../Dashboard.shared';
import { memo, useMemo } from 'react';
import type {
  DashboardBlockSource as BlockSource,
  DashboardSummary,
  DashboardUpstreamUsage as UpstreamUsage,
} from '../../api/generated';
import '../../styles/pages/dashboard.css';
import '../../styles/pages/logs.css';
import { ChartPanel, DashboardChart } from './charts';
import { buildChartOptions } from './options';

export const DistributionRow = memo(function DistributionRow({
  summary,
  blockSources,
  upstreamUsage,
  summaryStatus,
  blockSourcesStatus,
  upstreamStatus,
  animateCharts,
}: {
  summary: DashboardSummary | null;
  blockSources: BlockSource[];
  upstreamUsage: UpstreamUsage[];
  summaryStatus: DashboardResourceStatus;
  blockSourcesStatus: DashboardResourceStatus;
  upstreamStatus: DashboardResourceStatus;
  animateCharts: boolean;
}) {
  const totalQueries = summary?.total_queries ?? 0;
  const blockedQueries = summary?.blocked_queries ?? 0;
  const allowedQueries = summary?.allowed_queries ?? 0;
  const cacheHits = summary?.cache_hits ?? 0;
  const blockedPct = totalQueries > 0 ? ((blockedQueries / totalQueries) * 100).toFixed(1) : '0';
  const cacheHitPct = totalQueries > 0 ? ((cacheHits / totalQueries) * 100).toFixed(0) : '0';

  const blockedAllowedOptions = useMemo(
    () =>
      buildChartOptions(
        animateCharts,
        { type: 'donut', height: '100%' },
        {
          labels: ['Allowed', 'Blocked'],
          colors: ['#a9dc76', '#ff6188'],
          legend: { position: 'bottom', labels: { colors: '#f4f4f5' } },
          stroke: { show: false },
          plotOptions: {
            pie: {
              donut: {
                size: '50%',
                labels: {
                  show: true,
                  name: { show: true, color: '#71717a', fontSize: '12px' },
                  value: {
                    show: true,
                    color: '#f4f4f5',
                    fontSize: '20px',
                    fontFamily: 'JetBrains Mono',
                    fontWeight: 700,
                    formatter: () => `${blockedPct}%`,
                  },
                  total: {
                    show: true,
                    label: 'Blocked',
                    color: '#71717a',
                    fontSize: '12px',
                    fontFamily: 'Inter',
                    formatter: () => `${blockedPct}%`,
                  },
                },
              },
            },
          },
        },
      ),
    [animateCharts, blockedPct],
  );

  const blockDonutOptions = useMemo(
    () =>
      buildChartOptions(
        animateCharts,
        { type: 'donut', height: '100%' },
        {
          labels: blockSources.map((source) => source.list_name || 'Custom'),
          colors: ['#ff6188', '#fc9867', '#ab9df2'],
          legend: { position: 'bottom', labels: { colors: '#f4f4f5' } },
          stroke: { show: false },
          plotOptions: { pie: { donut: { size: '35%' } } },
        },
      ),
    [animateCharts, blockSources],
  );

  const upstreamOptions = useMemo(
    () =>
      buildChartOptions(
        animateCharts,
        { type: 'donut', height: '100%' },
        {
          labels: upstreamUsage.map((usage) => usage.upstream || 'Unknown'),
          colors: ['#fc9867', '#ffd866', '#a9dc76'],
          legend: { position: 'bottom', labels: { colors: '#f4f4f5' } },
          stroke: { show: false },
          plotOptions: { pie: { donut: { size: '35%' } } },
        },
      ),
    [animateCharts, upstreamUsage],
  );

  return (
    <div className="row-3-col">
      <div className="card">
        <div className="card-header">
          <div className="card-title">Blocked vs Allowed</div>
        </div>
        <div className="chart-container">
          <ChartPanel {...summaryStatus} hasData={totalQueries > 0} emptyMessage="No data">
            <DashboardChart
              options={blockedAllowedOptions}
              series={[allowedQueries, blockedQueries]}
              type="donut"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <div className="card-title text-red">Block Sources</div>
        </div>
        <div className="chart-container">
          <ChartPanel
            {...blockSourcesStatus}
            hasData={blockSources.length > 0}
            emptyMessage="No data"
          >
            <DashboardChart
              options={blockDonutOptions}
              series={blockSources.map((source) => source.count)}
              type="donut"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <div className="card-title text-orange">Upstream Usage</div>
          <div className="text-xs text-muted font-mono">
            {summary === null ? '— CACHE HIT' : `${cacheHitPct}% CACHE HIT`}
          </div>
        </div>
        <div className="chart-container">
          <ChartPanel {...upstreamStatus} hasData={upstreamUsage.length > 0} emptyMessage="No data">
            <DashboardChart
              options={upstreamOptions}
              series={upstreamUsage.map((usage) => usage.count)}
              type="donut"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>
    </div>
  );
});
