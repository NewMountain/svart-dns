import type { DashboardResourceStatus } from '../Dashboard.shared';
import { memo } from 'react';
import type { DashboardSummary } from '../../api/generated';
import StatCard from '../../components/StatCard';
import '../../styles/pages/dashboard.css';
import '../../styles/pages/logs.css';

export const SummaryCards = memo(function SummaryCards({
  summary,
  status,
}: {
  summary: DashboardSummary | null;
  status: DashboardResourceStatus;
}) {
  const totalQueries = summary?.total_queries ?? 0;
  const blockedQueries = summary?.blocked_queries ?? 0;
  const avgLatency = summary?.avg_latency_microseconds ?? 0;
  const activeClients = summary?.active_clients ?? 0;

  return (
    <>
      {status.error && (
        <p role="status" className="text-muted">
          {summary === null ? 'Data unavailable.' : 'Refresh failed. Showing last available data.'}
        </p>
      )}
      <div className="stats-grid">
        <StatCard
          label="Total Queries"
          value={status.loading || summary === null ? '—' : totalQueries.toLocaleString()}
          valueColor="blue"
        />
        <StatCard
          label="Blocked"
          value={status.loading || summary === null ? '—' : blockedQueries.toLocaleString()}
          valueColor="red"
          accentTop="red"
        />
        <StatCard
          label="Avg Latency"
          value={status.loading || summary === null ? '—' : `${(avgLatency / 1000).toFixed(1)}ms`}
          valueColor="purple"
        />
        <StatCard
          label="Active Clients"
          value={status.loading || summary === null ? '—' : activeClients}
        />
      </div>
    </>
  );
});
