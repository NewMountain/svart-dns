import Badge from '../components/Badge';
import ContextMenu from '../components/ContextMenu';
import LatencyValue from '../components/LatencyValue';
import TopBar from '../components/TopBar';
import { useLogsController } from './Logs.controller';

import { formatSource, resultBadge } from '../lib/queryResult';
import '../styles/pages/logs.css';

export default function Logs() {
  const model = useLogsController();
  return <LogsView model={model} />;
}

function LogsView({ model }: { model: NonNullable<ReturnType<typeof useLogsController>> }) {
  const {
    action,
    refreshInterval,
    setRefreshInterval,
    search,
    clientFilter,
    groupFilter,
    rangeFilter,
    resultFilter,
    typeFilter,
    limit,
    offset,
    setOffset,
    setSearch,
    setClientFilter,
    setGroupFilter,
    setRangeFilter,
    setResultFilter,
    setTypeFilter,
    setLimit,
    logs,
    total,
    loading,
    error,
    expandedId,
    evalDetail,
    evalLoading,
    contextMenu,
    setContextMenu,
    clients,
    groups,
    ranges,
    fetchLogs,
    toggleExpand,
    renderTierBlock,
    openContextMenu,
    allowDomain,
    blockDomain,
    COL_COUNT,
    hasFilters,
  } = model;
  return (
    <div className="logs-page">
      <TopBar title="Query Logs" />

      <div className="controls-wrapper">
        <div className="controls-row">
          <div className="search-wrapper">
            <svg
              className="search-icon"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2"
            >
              <circle cx="11" cy="11" r="8" />
              <line x1="21" y1="21" x2="16.65" y2="16.65" />
            </svg>
            <input
              type="text"
              className="search-input"
              aria-label="Search query logs"
              placeholder="Search domain, client IP, or rule..."
              value={search}
              onChange={(e) => {
                setSearch(e.target.value);
              }}
            />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <div className="mode-switch">
              <div className="mode-btn live-active">
                <div className="pulse-dot" /> Live
              </div>
            </div>
            <select
              className="filter-select"
              aria-label="Refresh interval"
              value={refreshInterval}
              onChange={(e) => {
                setRefreshInterval(Number(e.target.value));
              }}
            >
              <option value={5000}>5s</option>
              <option value={30000}>30s</option>
              <option value={60000}>1m</option>
              <option value={300000}>5m</option>
            </select>
          </div>
        </div>
        <div className="controls-row" style={{ flexWrap: 'wrap' }}>
          <select
            className="filter-select"
            aria-label="Client"
            value={clientFilter}
            onChange={(e) => {
              setClientFilter(e.target.value);
            }}
          >
            <option value="">All Clients</option>
            {(clients ?? []).map((c) => (
              <option key={c.ip_address} value={c.ip_address}>
                {c.alias ? `${c.alias} (${c.ip_address})` : c.ip_address}
              </option>
            ))}
          </select>
          <select
            className="filter-select"
            aria-label="Group"
            value={groupFilter}
            onChange={(e) => {
              setGroupFilter(e.target.value);
            }}
          >
            <option value="">All Groups</option>
            {(groups ?? []).map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
          <select
            className="filter-select"
            aria-label="Network"
            value={rangeFilter}
            onChange={(e) => {
              setRangeFilter(e.target.value);
            }}
          >
            <option value="">All Networks</option>
            {(ranges ?? []).map((r) => (
              <option key={r.id} value={r.id}>
                {r.name}
              </option>
            ))}
          </select>
          <div style={{ width: 1, height: 24, background: 'var(--border)', margin: '0 4px' }} />
          <select
            className="filter-select"
            aria-label="Result"
            value={resultFilter}
            onChange={(e) => {
              setResultFilter(e.target.value);
            }}
          >
            <option value="">All Results</option>
            <option value="blocked">Blocked</option>
            <option value="allowed">Allowed</option>
            <option value="rewrite">Rewrite</option>
          </select>
          <select
            className="filter-select"
            aria-label="Query type"
            value={typeFilter}
            onChange={(e) => {
              setTypeFilter(e.target.value);
            }}
          >
            <option value="">All Types</option>
            <option value="A">A</option>
            <option value="AAAA">AAAA</option>
            <option value="HTTPS">HTTPS</option>
          </select>
          <div style={{ marginLeft: 'auto' }}>
            <select
              className="filter-select"
              aria-label="Rows per page"
              style={{ minWidth: 80 }}
              value={limit}
              onChange={(e) => {
                setLimit(Number(e.target.value));
              }}
            >
              <option value="100">100 Rows</option>
              <option value="500">500 Rows</option>
              <option value="1000">1k Rows</option>
              <option value="5000">5k Rows</option>
            </select>
          </div>
        </div>
      </div>

      {error && (
        <div className="logs-error" role="alert">
          <span>
            {error} {logs.length > 0 && 'The last loaded queries are still shown.'}
          </span>
          <button className="btn btn-ghost" onClick={() => void fetchLogs()}>
            Retry
          </button>
        </div>
      )}
      <div className="logs-table-wrapper" role="region" aria-label="Query log table" tabIndex={0}>
        <table className="log-table">
          <colgroup>
            {[10, 16, 8, 8, 5, 15, 7, 7, 14, 6, 4].map((width, index) => (
              <col key={index} style={{ width: `${String(width)}%` }} />
            ))}
          </colgroup>
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
              <th>Policy</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {loading ? (
              <tr>
                <td colSpan={COL_COUNT}>
                  <div className="spinner" style={{ margin: '20px auto' }} />
                </td>
              </tr>
            ) : (
              logs.flatMap((entry) => {
                const rows = [
                  <tr key={entry.id} className={expandedId === entry.id ? 'expanded' : ''}>
                    <td>
                      {new Date(entry.timestamp).toLocaleTimeString([], {
                        hour: '2-digit',
                        minute: '2-digit',
                        second: '2-digit',
                      })}
                    </td>
                    <td>
                      {entry.client_ip}
                      {entry.client_alias && (
                        <span className="text-client-name">{entry.client_alias}</span>
                      )}
                    </td>
                    <td>
                      {entry.range_name && <span className="text-tag">{entry.range_name}</span>}
                    </td>
                    <td>
                      {entry.group_name && <span className="text-tag">{entry.group_name}</span>}
                    </td>
                    <td>{entry.query_type}</td>
                    <td className="domain-cell">{entry.query_name.replace(/\.$/, '')}</td>
                    <td>{resultBadge(entry)}</td>
                    <td className="latency-cell">
                      <LatencyValue microseconds={entry.latency_microseconds} />
                    </td>
                    <td className="source-cell">{formatSource(entry)}</td>
                    <td>
                      <button
                        className={`expand-btn${expandedId === entry.id ? ' open' : ''}`}
                        onClick={action(() => toggleExpand(entry.id))}
                        title="Show policy evaluation"
                        aria-label="Show policy evaluation"
                      >
                        <svg
                          width="14"
                          height="14"
                          viewBox="0 0 24 24"
                          fill="none"
                          stroke="currentColor"
                          strokeWidth="2"
                        >
                          <polyline points="6 9 12 15 18 9" />
                        </svg>
                      </button>
                    </td>
                    <td>
                      <button
                        className="cog-btn"
                        onClick={(e) => {
                          openContextMenu(e, entry);
                        }}
                        aria-label="More actions"
                      >
                        ⚙
                      </button>
                    </td>
                  </tr>,
                ];
                if (expandedId === entry.id) {
                  rows.push(
                    <tr key={`${String(entry.id)}-eval`} className="eval-row">
                      <td colSpan={COL_COUNT}>
                        {evalLoading ? (
                          <div className="eval-loading">
                            <div className="spinner" />
                          </div>
                        ) : evalDetail?.policy ? (
                          <div className="eval-panel">
                            <div className="eval-header">
                              Policy Evaluation:{' '}
                              <strong>{evalDetail.policy.result.toUpperCase() || 'ALLOW'}</strong>
                            </div>
                            {renderTierBlock(
                              'Network',
                              evalDetail.policy.range_evaluation,
                              evalDetail.policy.result_source?.tier === 'range',
                            )}
                            {renderTierBlock(
                              'Group',
                              evalDetail.policy.group_evaluation,
                              evalDetail.policy.result_source?.tier === 'group',
                            )}
                            {renderTierBlock(
                              'IP',
                              evalDetail.policy.ip_evaluation,
                              evalDetail.policy.result_source?.tier === 'ip',
                            )}
                            {evalDetail.policy.result_source?.tier === 'default' && (
                              <div className="eval-tier eval-winner">
                                <div className="eval-tier-header">
                                  <span className="eval-tier-label">Default</span>
                                  <Badge variant="allow">ALLOW</Badge>
                                  <span className="eval-winner-tag">WINNER</span>
                                </div>
                              </div>
                            )}
                          </div>
                        ) : (
                          <div className="eval-panel">
                            <div className="eval-no-match">No policy data available</div>
                          </div>
                        )}
                      </td>
                    </tr>,
                  );
                }
                return rows;
              })
            )}
          </tbody>
        </table>
        {!loading && !error && logs.length === 0 && (
          <div className="empty-state">
            <strong>{hasFilters ? 'No logs matching filters' : 'No DNS queries logged yet'}</strong>
            <span>
              {hasFilters
                ? 'Clear or broaden the filters to see more queries.'
                : 'Send a DNS query to this server. New queries appear automatically.'}
            </span>
          </div>
        )}
      </div>

      <div className="pagination">
        <div>
          {error && logs.length === 0 ? (
            'Query count unavailable'
          ) : (
            <>
              Showing{' '}
              <strong>
                {total === 0 ? 0 : offset + 1}-{Math.min(offset + limit, total)}
              </strong>{' '}
              of <strong>{total.toLocaleString()}</strong>
            </>
          )}
        </div>
        <div style={{ display: 'flex', gap: 8 }}>
          <button
            style={{
              background: 'none',
              border: '1px solid var(--border)',
              color: 'var(--text-muted)',
              padding: '4px 8px',
              borderRadius: 4,
              cursor: 'pointer',
            }}
            disabled={offset === 0}
            onClick={() => {
              setOffset(Math.max(0, offset - limit));
            }}
          >
            Previous
          </button>
          <button
            style={{
              background: 'var(--bg-hover)',
              border: '1px solid var(--border)',
              color: 'var(--text-main)',
              padding: '4px 8px',
              borderRadius: 4,
              cursor: 'pointer',
            }}
            disabled={offset + limit >= total}
            onClick={() => {
              setOffset(offset + limit);
            }}
          >
            Next
          </button>
        </div>
      </div>

      {contextMenu && (
        <ContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          header={contextMenu.entry.query_name}
          items={[
            {
              label: `Allow for ${contextMenu.entry.client_alias || contextMenu.entry.client_ip}`,
              onClick: action(() =>
                allowDomain(contextMenu.entry.query_name, contextMenu.entry.client_ip),
              ),
              success: true,
            },
            {
              label: `Block for ${contextMenu.entry.client_alias || contextMenu.entry.client_ip}`,
              onClick: action(() =>
                blockDomain(contextMenu.entry.query_name, contextMenu.entry.client_ip),
              ),
              danger: true,
            },
            {
              label: 'Copy Domain',
              onClick: action(() => navigator.clipboard.writeText(contextMenu.entry.query_name)),
            },
          ]}
          onClose={() => {
            setContextMenu(null);
          }}
        />
      )}
    </div>
  );
}
