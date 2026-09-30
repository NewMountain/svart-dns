import { memo } from 'react';
import LatencyValue from '../../components/LatencyValue';
import {
  formatSource as logFormatSource,
  resultBadge as logResultBadge,
} from '../../lib/queryResult';
import '../../styles/pages/dashboard.css';
import '../../styles/pages/logs.css';
import { useLiveQueryLogController } from './LiveQueryLogCard.controller';

export const LiveQueryLogCard = memo(function LiveQueryLogCard({ enabled }: { enabled: boolean }) {
  return <LiveQueryLogView model={useLiveQueryLogController(enabled)} />;
});
function LiveQueryLogView({ model }: { model: ReturnType<typeof useLiveQueryLogController> }) {
  const { rows, enabled, loading, error, hasResult } = model;
  return (
    <div className="card">
      <div className="card-header">
        <div className="card-title text-green">Live Query Log</div>
      </div>
      {error && (
        <p role="status" className="text-muted">
          {hasResult ? 'Refresh failed. Showing last available data.' : 'Data unavailable.'}
        </p>
      )}
      {enabled && !loading ? (
        <div className="logs-table-wrapper" style={{ maxHeight: 680 }}>
          <table className="log-table">
            <thead>
              <tr>
                <th>Time</th>
                <th>Client</th>
                <th>Networks</th>
                <th>Groups</th>
                <th>Type</th>
                <th>Query</th>
                <th>Result</th>
                <th>Lat.</th>
                <th>Source</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((log) => (
                <tr key={log.id}>
                  <td>
                    {new Date(log.timestamp).toLocaleTimeString([], {
                      hour: '2-digit',
                      minute: '2-digit',
                      second: '2-digit',
                    })}
                  </td>
                  <td>
                    {log.client_ip}
                    {log.client_alias && (
                      <span className="text-client-name">{log.client_alias}</span>
                    )}
                  </td>
                  <td>{log.range_name && <span className="text-tag">{log.range_name}</span>}</td>
                  <td>{log.group_name && <span className="text-tag">{log.group_name}</span>}</td>
                  <td>{log.query_type}</td>
                  <td className="domain-cell">{log.query_name.replace(/\.$/, '')}</td>
                  <td>{logResultBadge(log)}</td>
                  <td className="latency-cell">
                    <LatencyValue microseconds={log.latency_microseconds} />
                  </td>
                  <td className="source-cell">{logFormatSource(log)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : (
        <div className="chart-loading" style={{ minHeight: 320 }}>
          <div className="loading-pulse" />
        </div>
      )}
    </div>
  );
}
