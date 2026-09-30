import type { DashboardResourceStatus } from '../Dashboard.shared';
import { memo, useMemo } from 'react';
import type {
  DashboardLatencyPoint as LatencyPoint,
  DashboardTimeseriesPoint as TimeseriesPoint,
} from '../../api/generated';
import '../../styles/pages/dashboard.css';
import '../../styles/pages/logs.css';
import { ChartPanel, DashboardChart } from './charts';
import { buildChartOptions, formatTime } from './options';

export const TrafficLatencyRow = memo(function TrafficLatencyRow({
  timeseries,
  latency,
  avgLatencyMicroseconds,
  timeseriesStatus,
  latencyStatus,
  animateCharts,
}: {
  timeseries: TimeseriesPoint[];
  latency: LatencyPoint[];
  avgLatencyMicroseconds: number | null;
  timeseriesStatus: DashboardResourceStatus;
  latencyStatus: DashboardResourceStatus;
  animateCharts: boolean;
}) {
  const trafficOptions = useMemo(
    () =>
      buildChartOptions(
        animateCharts,
        {
          type: 'area',
          height: '100%',
          parentHeightOffset: 0,
          zoom: { enabled: false },
        },
        {
          colors: ['#78dce8', '#ff6188'],
          stroke: { curve: 'smooth', width: 2 },
          fill: {
            type: 'gradient',
            gradient: { shadeIntensity: 1, opacityFrom: 0.4, opacityTo: 0.05, stops: [0, 100] },
          },
          xaxis: {
            categories: timeseries.map((point) => formatTime(point.timestamp)),
            axisBorder: { show: false },
            axisTicks: { show: false },
          },
        },
      ),
    [animateCharts, timeseries],
  );

  const trafficSeries = useMemo(
    () => [
      { name: 'Queries', data: timeseries.map((point) => point.queries) },
      { name: 'Blocked', data: timeseries.map((point) => point.blocked) },
    ],
    [timeseries],
  );

  const latencyOptions = useMemo(
    () =>
      buildChartOptions(
        animateCharts,
        {
          type: 'area',
          height: '100%',
          parentHeightOffset: 0,
          zoom: { enabled: false },
        },
        {
          colors: ['#ab9df2', '#fc9867'],
          stroke: { curve: 'smooth', width: 2 },
          fill: {
            type: 'gradient',
            gradient: { shadeIntensity: 1, opacityFrom: 0.4, opacityTo: 0.05, stops: [0, 100] },
          },
          yaxis: { title: { text: 'Milliseconds (ms)', style: { color: '#71717a' } } },
          xaxis: {
            categories: latency.map((point) => formatTime(point.timestamp)),
            axisBorder: { show: false },
            axisTicks: { show: false },
          },
        },
      ),
    [animateCharts, latency],
  );

  const latencySeries = useMemo(
    () => [
      {
        name: 'Overall Avg',
        data: latency.map((point) => +(point.avg_latency_us / 1000).toFixed(2)),
      },
      {
        name: 'Upstream Avg',
        data: latency.map((point) => +(point.max_latency_us / 1000).toFixed(2)),
      },
    ],
    [latency],
  );

  return (
    <div className="row-50-50">
      <div className="card">
        <div className="card-header">
          <div className="card-title">Traffic Volume & Block Rate</div>
          <div className="text-xs text-muted font-mono">30s REFRESH</div>
        </div>
        <div className="chart-container">
          <ChartPanel {...timeseriesStatus} hasData={timeseries.length > 0} emptyMessage="No data">
            <DashboardChart
              options={trafficOptions}
              series={trafficSeries}
              type="area"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <div className="card-title text-purple">Latency Performance</div>
          <div className="text-xs text-muted font-mono">
            {avgLatencyMicroseconds === null
              ? 'AVG —'
              : `AVG ${(avgLatencyMicroseconds / 1000).toFixed(1)}ms`}
          </div>
        </div>
        <div className="chart-container">
          <ChartPanel {...latencyStatus} hasData={latency.length > 0} emptyMessage="No data">
            <DashboardChart
              options={latencyOptions}
              series={latencySeries}
              type="area"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>
    </div>
  );
});
