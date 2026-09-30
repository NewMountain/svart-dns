import type { DashboardResourceStatus } from '../Dashboard.shared';
import { memo, useMemo } from 'react';
import type { SystemSample } from '../../api/generated';
import '../../styles/pages/dashboard.css';
import '../../styles/pages/logs.css';
import { ChartPanel, DashboardChart } from './charts';
import { buildChartOptions, formatTime } from './options';

export const SystemResourcesRow = memo(function SystemResourcesRow({
  system,
  status,
  animateCharts,
}: {
  system: SystemSample[];
  status: DashboardResourceStatus;
  animateCharts: boolean;
}) {
  const hasSystemData = system.length > 0;
  const latestSample = system[system.length - 1] ?? null;
  const timeLabels = useMemo(() => system.map((sample) => formatTime(sample.timestamp)), [system]);

  const cpuChartOptions = useMemo(
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
          colors: ['#a9dc76'],
          stroke: { curve: 'smooth', width: 2 },
          fill: {
            type: 'gradient',
            gradient: { shadeIntensity: 1, opacityFrom: 0.4, opacityTo: 0.05, stops: [0, 100] },
          },
          yaxis: {
            min: 0,
            max: 100,
            labels: { formatter: (value: number) => `${value.toFixed(0)}%` },
          },
          xaxis: {
            categories: timeLabels,
            labels: { show: false },
            axisBorder: { show: false },
            axisTicks: { show: false },
          },
        },
      ),
    [animateCharts, timeLabels],
  );

  const memChartOptions = useMemo(
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
          colors: ['#78dce8', '#ab9df2'],
          stroke: { curve: 'smooth', width: 2 },
          fill: {
            type: 'gradient',
            gradient: { shadeIntensity: 1, opacityFrom: 0.4, opacityTo: 0.05, stops: [0, 100] },
          },
          yaxis: {
            min: 0,
            labels: {
              formatter: (value: number) =>
                value >= 1024 * 1024 * 1024
                  ? `${(value / 1024 / 1024 / 1024).toFixed(1)} GB`
                  : `${(value / 1024 / 1024).toFixed(0)} MB`,
            },
          },
          xaxis: {
            categories: timeLabels,
            labels: { show: false },
            axisBorder: { show: false },
            axisTicks: { show: false },
          },
        },
      ),
    [animateCharts, timeLabels],
  );

  const diskChartOptions = useMemo(
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
          colors: ['#fc9867'],
          stroke: { curve: 'smooth', width: 2 },
          fill: {
            type: 'gradient',
            gradient: { shadeIntensity: 1, opacityFrom: 0.4, opacityTo: 0.05, stops: [0, 100] },
          },
          yaxis: {
            min: 0,
            labels: { formatter: (value: number) => `${(value / 1024 / 1024).toFixed(1)} MB` },
          },
          xaxis: {
            categories: timeLabels,
            labels: { show: false },
            axisBorder: { show: false },
            axisTicks: { show: false },
          },
        },
      ),
    [animateCharts, timeLabels],
  );

  return (
    <div className="row-3-col">
      <div className="card">
        <div className="card-header">
          <div className="card-title text-green">CPU Usage</div>
          {latestSample && (
            <div className="text-xs text-muted font-mono">
              {latestSample.cpu_percent.toFixed(1)}%
            </div>
          )}
        </div>
        <div className="chart-container">
          <ChartPanel {...status} hasData={hasSystemData} emptyMessage="Collecting data...">
            <DashboardChart
              options={cpuChartOptions}
              series={[
                { name: 'CPU %', data: system.map((sample) => +sample.cpu_percent.toFixed(1)) },
              ]}
              type="area"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <div className="card-title text-blue">Memory</div>
          {latestSample && (
            <div className="text-xs text-muted font-mono">
              {(latestSample.rss_bytes / 1024 / 1024).toFixed(0)} MB RSS
            </div>
          )}
        </div>
        <div className="chart-container">
          <ChartPanel {...status} hasData={hasSystemData} emptyMessage="Collecting data...">
            <DashboardChart
              options={memChartOptions}
              series={[
                { name: 'RSS', data: system.map((sample) => sample.rss_bytes) },
                { name: 'Heap', data: system.map((sample) => sample.heap_alloc) },
              ]}
              type="area"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <div className="card-title text-orange">Database Size</div>
          {latestSample && (
            <div className="text-xs text-muted font-mono">
              {(latestSample.db_size_bytes / 1024 / 1024).toFixed(1)} MB
            </div>
          )}
        </div>
        <div className="chart-container">
          <ChartPanel {...status} hasData={hasSystemData} emptyMessage="Collecting data...">
            <DashboardChart
              options={diskChartOptions}
              series={[{ name: 'DB Size', data: system.map((sample) => sample.db_size_bytes) }]}
              type="area"
              height="100%"
            />
          </ChartPanel>
        </div>
      </div>
    </div>
  );
});
