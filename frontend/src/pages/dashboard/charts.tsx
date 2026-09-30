import type { ApexOptions } from 'apexcharts';
import { Suspense, lazy, type ReactNode } from 'react';
import '../../styles/pages/dashboard.css';
import '../../styles/pages/logs.css';

export interface ChartPanelProps {
  loading: boolean;
  error?: string | null;
  hasData: boolean;
  hasResult: boolean;
  emptyMessage: string;
  children: ReactNode;
}

export const Chart = lazy(() => import('react-apexcharts'));

export function ChartPanel({
  loading,
  error,
  hasData,
  hasResult,
  emptyMessage,
  children,
}: ChartPanelProps) {
  if (loading) {
    return (
      <div className="chart-loading">
        <div className="loading-pulse" />
      </div>
    );
  }
  if (error) {
    return (
      <div className="chart-stale-panel">
        <p role="status" className="text-muted chart-stale-warning">
          {hasResult ? 'Refresh failed. Showing last available data.' : 'Data unavailable.'}
        </p>
        {hasResult && (
          <div className="chart-stale-content">
            {hasData ? (
              children
            ) : (
              <div className="empty-state">
                <p>{emptyMessage}</p>
              </div>
            )}
          </div>
        )}
      </div>
    );
  }
  if (!hasData) {
    return (
      <div className="empty-state">
        <p>{emptyMessage}</p>
      </div>
    );
  }
  return <>{children}</>;
}

export function DashboardChart({
  options,
  series,
  type,
  height = '100%',
}: {
  options: ApexOptions;
  series: NonNullable<ApexOptions['series']>;
  type: 'area' | 'donut' | 'bar';
  height?: string | number;
}) {
  return (
    <Suspense
      fallback={
        <div className="chart-loading">
          <div className="loading-pulse" />
        </div>
      }
    >
      <Chart options={options} series={series} type={type} height={height} />
    </Suspense>
  );
}
