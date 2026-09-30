import type { DashboardResourceStatus } from '../Dashboard.shared';
import { memo, useMemo } from 'react';
import type { DashboardTopDomains as TopDomainsResponse } from '../../api/generated';
import { donutTooltipFormatter } from '../../lib/chartTooltips';
import '../../styles/pages/dashboard.css';
import '../../styles/pages/logs.css';
import { ChartPanel, DashboardChart } from './charts';
import { buildChartOptions, buildDonutSlices } from './options';

export const TopDomainsRow = memo(function TopDomainsRow({
  topPermitted,
  topBlocked,
  permittedStatus,
  blockedStatus,
  animateCharts,
}: {
  topPermitted: TopDomainsResponse | null;
  topBlocked: TopDomainsResponse | null;
  permittedStatus: DashboardResourceStatus;
  blockedStatus: DashboardResourceStatus;
  animateCharts: boolean;
}) {
  const permittedSlices = useMemo(() => buildDonutSlices(topPermitted), [topPermitted]);
  const blockedSlices = useMemo(() => buildDonutSlices(topBlocked), [topBlocked]);

  const permittedDonutOptions = useMemo(
    () =>
      buildChartOptions(
        animateCharts,
        { type: 'donut', height: '100%' },
        {
          labels: permittedSlices.map((slice) => slice.label),
          colors: permittedSlices.map((slice) => slice.color),
          legend: {
            position: 'bottom',
            labels: { colors: '#f4f4f5' },
            fontSize: '11px',
            fontFamily: 'JetBrains Mono',
          },
          stroke: { show: false },
          plotOptions: { pie: { donut: { size: '35%' } } },
          tooltip: { custom: donutTooltipFormatter(permittedSlices) },
        },
      ),
    [animateCharts, permittedSlices],
  );

  const blockedDonutOptions = useMemo(
    () =>
      buildChartOptions(
        animateCharts,
        { type: 'donut', height: '100%' },
        {
          labels: blockedSlices.map((slice) => slice.label),
          colors: blockedSlices.map((slice) => slice.color),
          legend: {
            position: 'bottom',
            labels: { colors: '#f4f4f5' },
            fontSize: '11px',
            fontFamily: 'JetBrains Mono',
          },
          stroke: { show: false },
          plotOptions: { pie: { donut: { size: '35%' } } },
          tooltip: { custom: donutTooltipFormatter(blockedSlices) },
        },
      ),
    [animateCharts, blockedSlices],
  );

  return (
    <div className="row-50-50">
      <div className="card">
        <div className="card-header">
          <div className="card-title text-blue">Top Permitted Domains</div>
        </div>
        <div className="chart-container">
          <ChartPanel
            {...permittedStatus}
            hasData={permittedSlices.length > 0}
            emptyMessage="No data"
          >
            <DashboardChart
              options={permittedDonutOptions}
              series={permittedSlices.map((slice) => slice.value)}
              type="donut"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <div className="card-title text-red">Top Blocked Domains</div>
        </div>
        <div className="chart-container">
          <ChartPanel {...blockedStatus} hasData={blockedSlices.length > 0} emptyMessage="No data">
            <DashboardChart
              options={blockedDonutOptions}
              series={blockedSlices.map((slice) => slice.value)}
              type="donut"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>
    </div>
  );
});
