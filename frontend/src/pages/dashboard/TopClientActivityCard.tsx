import type { DashboardResourceStatus } from '../Dashboard.shared';
import { memo, useMemo } from 'react';
import { useNavigate } from 'react-router-dom';
import type { DashboardTopClient as TopClient } from '../../api/generated';
import { clientActivityTooltipFormatter } from '../../lib/chartTooltips';
import '../../styles/pages/dashboard.css';
import '../../styles/pages/logs.css';
import { ChartPanel, DashboardChart } from './charts';
import { buildChartOptions } from './options';

export const TopClientActivityCard = memo(function TopClientActivityCard({
  topClients,
  status,
  animateCharts,
}: {
  topClients: TopClient[];
  status: DashboardResourceStatus;
  animateCharts: boolean;
}) {
  const navigate = useNavigate();

  const clientBarOptions = useMemo(
    () =>
      buildChartOptions(
        animateCharts,
        {
          type: 'bar',
          height: '100%',
          stacked: true,
          events: {
            dataPointSelection(
              _event: unknown,
              _chart: unknown,
              options: { dataPointIndex: number },
            ) {
              const client = topClients[options.dataPointIndex];
              if (client) {
                void navigate(
                  `/assignments?tab=clients&selected=${encodeURIComponent(client.client_ip)}`,
                );
              }
            },
          },
        },
        {
          colors: ['#a9dc76', '#ff6188'],
          plotOptions: { bar: { horizontal: false, borderRadius: 0, columnWidth: '40%' } },
          xaxis: {
            categories: topClients.map((client) => client.alias || client.client_ip),
            axisBorder: { show: false },
            axisTicks: { show: false },
          },
          legend: { position: 'top', horizontalAlign: 'right', labels: { colors: '#f4f4f5' } },
          tooltip: {
            theme: 'dark',
            style: { fontFamily: 'JetBrains Mono' },
            shared: true,
            intersect: false,
            custom: clientActivityTooltipFormatter,
          },
        },
      ),
    [animateCharts, navigate, topClients],
  );

  const clientSeries = useMemo(
    () => [
      { name: 'Allowed', data: topClients.map((client) => client.allowed) },
      { name: 'Blocked', data: topClients.map((client) => client.blocked) },
    ],
    [topClients],
  );

  return (
    <div className="card" style={{ marginBottom: 'var(--space-lg)', height: 400 }}>
      <div className="card-header">
        <div className="card-title">Top Client Activity</div>
        {topClients.length > 0 && (
          <div className="text-sm text-muted">Showing top {topClients.length}</div>
        )}
      </div>
      <div className="chart-container">
        <ChartPanel {...status} hasData={topClients.length > 0} emptyMessage="No data">
          <DashboardChart
            options={clientBarOptions}
            series={clientSeries}
            type="bar"
            height="100%"
          />
        </ChartPanel>
      </div>
    </div>
  );
});
