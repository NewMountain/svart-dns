import type { DashboardResourceStatus } from '../Dashboard.shared';
import { memo, useMemo } from 'react';
import type {
  DashboardSummary,
  DashboardServfailClient as ServfailClient,
} from '../../api/generated';
import '../../styles/pages/dashboard.css';
import '../../styles/pages/logs.css';
import { ChartPanel, DashboardChart } from './charts';
import { buildChartOptions } from './options';

export const OverridesFailuresRow = memo(function OverridesFailuresRow({
  summary,
  servfails,
  summaryStatus,
  servfailsStatus,
  animateCharts,
}: {
  summary: DashboardSummary | null;
  servfails: ServfailClient[];
  summaryStatus: DashboardResourceStatus;
  servfailsStatus: DashboardResourceStatus;
  animateCharts: boolean;
}) {
  const customAllows = summary?.custom_allows ?? 0;
  const customBlocks = summary?.custom_blocks ?? 0;
  const rewriteHits = summary?.rewrite_hits ?? 0;
  const servfailCount = summary?.servfail_count ?? 0;

  const overridesBarOptions = useMemo(
    () =>
      buildChartOptions(
        animateCharts,
        { type: 'bar' },
        {
          colors: ['#a9dc76', '#ff6188', '#78dce8'],
          plotOptions: {
            bar: { horizontal: true, barHeight: '50%', borderRadius: 4, distributed: true },
          },
          xaxis: {
            categories: ['Custom Allows', 'Custom Blocks', 'Rewrites'],
            labels: { style: { colors: '#71717a', fontFamily: 'JetBrains Mono' } },
          },
          yaxis: { labels: { style: { colors: '#f4f4f5' } } },
          legend: { show: false },
        },
      ),
    [animateCharts],
  );

  const servfailBarOptions = useMemo(
    () =>
      buildChartOptions(
        animateCharts,
        { type: 'bar' },
        {
          colors: ['#ff6188'],
          plotOptions: { bar: { horizontal: true, barHeight: '50%', borderRadius: 4 } },
          xaxis: {
            categories: servfails.map((client) => client.alias || client.client_ip),
            labels: { style: { colors: '#71717a', fontFamily: 'JetBrains Mono' } },
          },
          yaxis: { labels: { style: { colors: '#f4f4f5' }, maxWidth: 120 } },
          legend: { show: false },
        },
      ),
    [animateCharts, servfails],
  );

  return (
    <div className="row-50-50">
      <div className="card">
        <div className="card-header">
          <div className="card-title text-yellow">Custom Overrides</div>
        </div>
        <div className="chart-container">
          <ChartPanel
            {...summaryStatus}
            hasData={customAllows + customBlocks + rewriteHits > 0}
            emptyMessage="No custom overrides"
          >
            <DashboardChart
              options={overridesBarOptions}
              series={[{ name: 'Queries', data: [customAllows, customBlocks, rewriteHits] }]}
              type="bar"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <div className="card-title text-red">Resolution Failures</div>
          <div
            className="text-xs font-mono"
            style={{
              color:
                summary === null
                  ? 'var(--text-muted)'
                  : servfailCount > 0
                    ? 'var(--red)'
                    : 'var(--green)',
            }}
          >
            {summary === null ? '— SERVFAIL' : `${servfailCount.toString()} SERVFAIL`}
          </div>
        </div>
        <div className="chart-container">
          <ChartPanel
            {...servfailsStatus}
            hasData={servfails.length > 0}
            emptyMessage="No failures"
          >
            <DashboardChart
              options={servfailBarOptions}
              series={[{ name: 'Failures', data: servfails.map((client) => client.count) }]}
              type="bar"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>
    </div>
  );
});
