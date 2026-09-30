import type { EntityResult, PolicyResult, TierEvaluation } from '../api/generated';
import ClientSearch from '../components/ClientSearch';
import TopBar from '../components/TopBar';
import { useAnalysisController } from './Analysis.controller';

import '../styles/pages/analysis.css';

export default function Analysis() {
  const model = useAnalysisController();
  return <AnalysisView model={model} />;
}

function recordTypeLabel(type: number): string {
  const names: Record<number, string> = {
    1: 'A',
    28: 'AAAA',
    5: 'CNAME',
    65: 'HTTPS',
    64: 'SVCB',
    16: 'TXT',
    15: 'MX',
    12: 'PTR',
    33: 'SRV',
    2: 'NS',
    6: 'SOA',
    255: 'ANY',
  };
  return names[type] ?? `TYPE${String(type)}`;
}

interface SimRowProps {
  pr: PolicyResult;
  rangeEnts: EntityResult[];
  groupEnts: EntityResult[];
  ipEnts: EntityResult[];
  expectedRangeCount: number;
  expectedGroupCount: number;
  expectedIpCount: number;
  renderEntityBadge: (result: string) => React.JSX.Element;
  renderTierBadge: (result: string) => React.JSX.Element;
  renderEntityDetail: (entity: EntityResult) => string;
  renderTierSource: (tier: TierEvaluation) => string;
  renderFinalDecision: (pr: PolicyResult) => React.JSX.Element;
}

function SimRow({
  pr,
  rangeEnts,
  groupEnts,
  ipEnts,
  expectedRangeCount,
  expectedGroupCount,
  expectedIpCount,
  renderEntityBadge,
  renderTierBadge,
  renderEntityDetail,
  renderTierSource,
  renderFinalDecision,
}: SimRowProps) {
  return (
    <>
      <div className="sim-cell domain">
        {pr.domain}
        <span className="cell-detail">{recordTypeLabel(pr.record_type)}</span>
      </div>

      {/* Range entities */}
      {expectedRangeCount > 0 && (
        <>
          {Array.from({ length: expectedRangeCount }).map((_, i) => {
            const ent = rangeEnts[i];
            if (!ent) {
              return (
                <div key={`re-${String(i)}`} className="sim-cell inactive">
                  <span className="badge-none">-</span>
                </div>
              );
            }
            return (
              <div key={`re-${String(i)}`} className="sim-cell">
                {renderEntityBadge(ent.result)}
                <span className="cell-detail">{renderEntityDetail(ent)}</span>
              </div>
            );
          })}
          <div className="sim-cell res">
            {renderTierBadge(pr.range_evaluation?.result ?? '')}
            {pr.range_evaluation?.result_source && (
              <span className="cell-detail cell-winner">
                {renderTierSource(pr.range_evaluation)}
              </span>
            )}
          </div>
        </>
      )}

      {/* Group entities */}
      {expectedGroupCount > 0 && (
        <>
          {Array.from({ length: expectedGroupCount }).map((_, i) => {
            const ent = groupEnts[i];
            if (!ent) {
              return (
                <div key={`ge-${String(i)}`} className="sim-cell inactive">
                  <span className="badge-none">-</span>
                </div>
              );
            }
            return (
              <div key={`ge-${String(i)}`} className="sim-cell">
                {renderEntityBadge(ent.result)}
                <span className="cell-detail">{renderEntityDetail(ent)}</span>
              </div>
            );
          })}
          <div className="sim-cell res">
            {renderTierBadge(pr.group_evaluation?.result ?? '')}
            {pr.group_evaluation?.result_source && (
              <span className="cell-detail cell-winner">
                {renderTierSource(pr.group_evaluation)}
              </span>
            )}
          </div>
        </>
      )}

      {/* IP entities */}
      {expectedIpCount > 0 && (
        <>
          {Array.from({ length: expectedIpCount }).map((_, i) => {
            const ent = ipEnts[i];
            if (!ent) {
              return (
                <div key={`ie-${String(i)}`} className="sim-cell inactive">
                  <span className="badge-none">-</span>
                </div>
              );
            }
            return (
              <div key={`ie-${String(i)}`} className="sim-cell">
                {renderEntityBadge(ent.result)}
                <span className="cell-detail">{renderEntityDetail(ent)}</span>
              </div>
            );
          })}
          <div className="sim-cell res">
            {renderTierBadge(pr.ip_evaluation?.result ?? '')}
            {pr.ip_evaluation?.result_source && (
              <span className="cell-detail cell-winner">{renderTierSource(pr.ip_evaluation)}</span>
            )}
          </div>
        </>
      )}

      {/* Final */}
      {renderFinalDecision(pr)}
    </>
  );
}

function AnalysisView({ model }: { model: NonNullable<ReturnType<typeof useAnalysisController>> }) {
  const {
    action,
    tab,
    domains,
    setDomains,
    clientIp,
    setClientIp,
    recordType,
    setRecordType,
    setTab,
    matrixResult,
    simResult,
    loading,
    loadingSource,
    sourceError,
    trafficWindow,
    setTrafficWindow,
    hoveredCol,
    onColEnter,
    onColLeave,
    loadTrafficDomains,
    loadDomains,
    runMatrix,
    runSimulator,
    simColumns,
    renderEntityBadge,
    renderTierBadge,
    renderEntityDetail,
    renderTierSource,
    renderFinalDecision,
  } = model;
  return (
    <div style={{ display: 'flex', flexDirection: 'column', flex: 1, overflow: 'hidden' }}>
      <TopBar title="Analysis Tools" />

      <div className="sub-nav">
        <button
          className={`tab-item${tab === 'matrix' ? ' active' : ''}`}
          onClick={() => {
            setTab('matrix');
          }}
        >
          Blocklist Matrix
        </button>
        <button
          className={`tab-item${tab === 'sim' ? ' active' : ''}`}
          onClick={() => {
            setTab('sim');
          }}
        >
          Policy Simulator
        </button>
      </div>

      <div className="analysis-container" style={{ flex: 1 }}>
        <div className="input-pane">
          {/* Well 1: Client */}
          <div className="input-card">
            <div className="analysis-label">Client</div>
            <ClientSearch value={clientIp} onChange={setClientIp} />
          </div>

          <div className="input-card">
            <label className="analysis-label" htmlFor="analysis-record-type">
              Record type
            </label>
            <input
              id="analysis-record-type"
              className="analysis-input-field"
              list="analysis-record-types"
              value={recordType}
              onChange={(event) => {
                setRecordType(event.target.value.toUpperCase());
              }}
            />
            <datalist id="analysis-record-types">
              {[
                'A',
                'AAAA',
                'CNAME',
                'HTTPS',
                'SVCB',
                'TXT',
                'MX',
                'PTR',
                'SRV',
                'NS',
                'SOA',
                'ANY',
              ].map((type) => (
                <option key={type} value={type} />
              ))}
            </datalist>
          </div>

          {/* Well 2: Standard Simulations */}
          <div className="input-card">
            <div className="analysis-label">Standard Simulations</div>
            <div className="quick-load-grid">
              <button
                className="btn-small"
                disabled={loadingSource !== null}
                title="Load the operator-configured local ranking"
                onClick={action(() => loadDomains('top-sites', 1000))}
              >
                {loadingSource === 'top-sites' ? 'Loading...' : 'Top 1k sites'}
              </button>
              <button
                className="btn-small"
                disabled={loadingSource !== null}
                onClick={action(() => loadDomains('top-blocked', 250))}
              >
                {loadingSource === 'top-blocked' ? 'Loading...' : 'Top Blocked'}
              </button>
              <button
                className="btn-small"
                disabled={loadingSource !== null}
                onClick={action(() => loadDomains('top-queried', 250))}
              >
                {loadingSource === 'top-queried' ? 'Loading...' : 'Recent Queries'}
              </button>
              <button
                className="btn-small"
                disabled={loadingSource !== null}
                onClick={action(() => loadDomains('top-allowed', 250))}
              >
                {loadingSource === 'top-allowed' ? 'Loading...' : 'Top 250 Local'}
              </button>
              <button
                className="btn-small"
                style={{ gridColumn: 'span 2' }}
                disabled={loadingSource !== null || !clientIp}
                onClick={action(loadTrafficDomains)}
              >
                {loadingSource === 'client-history' ? 'Loading...' : 'Your Top 250'}
              </button>
            </div>
          </div>

          {sourceError && (
            <p role="alert" className="error-message">
              {sourceError}
            </p>
          )}

          {/* Well 3: Client Traffic */}
          <div className={`input-card${!clientIp ? ' client-traffic-disabled' : ''}`}>
            <div className="analysis-label">Client Traffic</div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              <div style={{ fontSize: '0.8rem', color: 'var(--text-muted)' }}>All traffic in:</div>
              <select
                className="analysis-input-field"
                aria-label="Traffic window"
                value={trafficWindow}
                onChange={(e) => {
                  setTrafficWindow(e.target.value);
                }}
                disabled={!clientIp}
              >
                <option value="1h">Last hour</option>
                <option value="24h">Last 24 hours</option>
                <option value="7d">Last 7 days</option>
                <option value="30d">Last 30 days</option>
              </select>
              <button
                className="btn-small"
                disabled={loadingSource !== null || !clientIp}
                onClick={action(loadTrafficDomains)}
              >
                {loadingSource === 'client-history' ? 'Loading...' : 'Load Domains'}
              </button>
            </div>
          </div>

          {/* Textarea + Run button (tab-specific) */}
          <div style={{ flex: 1, display: 'flex', flexDirection: 'column' }}>
            <div className="analysis-label">
              {tab === 'matrix' ? 'Domains to Analyze' : 'Domains to Test'}
            </div>
            <textarea
              className="analysis-text-area"
              aria-label="Domains"
              value={domains}
              onChange={(e) => {
                setDomains(e.target.value);
              }}
            />
          </div>

          {tab === 'matrix' ? (
            <button className="analysis-run-btn" onClick={action(runMatrix)} disabled={loading}>
              {loading ? (
                <div className="spinner" />
              ) : (
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <polygon points="5 3 19 12 5 21 5 3" />
                </svg>
              )}
              Run Matrix
            </button>
          ) : (
            <button
              className="analysis-run-btn"
              onClick={action(runSimulator)}
              disabled={loading || !clientIp}
            >
              {loading ? (
                <div className="spinner" />
              ) : (
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
                </svg>
              )}
              Simulate Policy
            </button>
          )}
        </div>

        <div className="results-pane">
          {/* Matrix results */}
          {tab === 'matrix' &&
            (matrixResult ? (
              <div
                className="matrix-container"
                role="region"
                aria-label="Blocklist matrix results"
                tabIndex={0}
              >
                <table className="matrix-table" onMouseLeave={onColLeave}>
                  <caption>Record type: {recordTypeLabel(matrixResult.record_type)}</caption>
                  <thead>
                    <tr>
                      <th style={{ backgroundColor: 'var(--bg-card)', zIndex: 11 }} />
                      {(matrixResult.lists ?? []).map((list, li) => (
                        <th
                          key={list.id}
                          className={`th-angle${hoveredCol === li ? ' col-highlight' : ''}`}
                        >
                          <div>
                            <span>{list.alias}</span>
                          </div>
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {(matrixResult.domains ?? []).map((domain, di) => (
                      <tr key={domain}>
                        <th>{domain}</th>
                        {((matrixResult.matrix ?? [])[di] ?? []).map((blocked, li) => (
                          <td
                            key={li}
                            className={hoveredCol === li ? 'col-highlight' : undefined}
                            title={(matrixResult.matched_rules ?? [])[di]?.[li] ?? undefined}
                            onMouseEnter={() => {
                              onColEnter(li);
                            }}
                          >
                            {blocked ? (
                              <div className="icon-block">{'\u00D7'}</div>
                            ) : (
                              <span className="icon-check">{'\u2713'}</span>
                            )}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="empty-state">
                <svg
                  width="48"
                  height="48"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1"
                  style={{ opacity: 0.3 }}
                >
                  <rect x="3" y="3" width="7" height="7" />
                  <rect x="14" y="3" width="7" height="7" />
                  <rect x="3" y="14" width="7" height="7" />
                  <rect x="14" y="14" width="7" height="7" />
                </svg>
                <div>Enter domains and click Run Matrix to see blocklist coverage</div>
              </div>
            ))}

          {/* Simulator results */}
          {tab === 'sim' &&
            (simResult && simColumns ? (
              <div className="sim-terminal">
                <div
                  className="sim-scroll"
                  role="region"
                  aria-label="Policy simulator results"
                  tabIndex={0}
                >
                  <div
                    className="sim-grid"
                    style={{ gridTemplateColumns: simColumns.gridTemplate }}
                  >
                    {/* Mega headers */}
                    <div className="sim-mega-header header-domain" />
                    {simColumns.rangeCols > 0 && (
                      <div
                        className="sim-mega-header header-range"
                        style={{ gridColumn: `span ${String(simColumns.rangeCols)}` }}
                      >
                        1. RANGES
                      </div>
                    )}
                    {simColumns.groupCols > 0 && (
                      <div
                        className="sim-mega-header header-group"
                        style={{ gridColumn: `span ${String(simColumns.groupCols)}` }}
                      >
                        2. GROUPS
                      </div>
                    )}
                    {simColumns.ipCols > 0 && (
                      <div
                        className="sim-mega-header header-client"
                        style={{ gridColumn: `span ${String(simColumns.ipCols)}` }}
                      >
                        3. CLIENT
                      </div>
                    )}
                    <div className="sim-mega-header header-final">FINAL</div>

                    {/* Sub headers */}
                    <div className="sim-sub-header" style={{ textAlign: 'left', paddingLeft: 16 }}>
                      DOMAIN
                    </div>
                    {simColumns.rangeEntities.map((e, i) => (
                      <div key={`rh-${String(i)}`} className="sim-sub-header">
                        {e.name}
                      </div>
                    ))}
                    {simColumns.rangeEntities.length > 0 && (
                      <div className="sim-sub-header res">TIER</div>
                    )}
                    {simColumns.groupEntities.map((e, i) => (
                      <div key={`gh-${String(i)}`} className="sim-sub-header">
                        {e.name}
                      </div>
                    ))}
                    {simColumns.groupEntities.length > 0 && (
                      <div className="sim-sub-header res">TIER</div>
                    )}
                    {simColumns.ipEntities.map((e, i) => (
                      <div key={`ih-${String(i)}`} className="sim-sub-header">
                        {e.name}
                      </div>
                    ))}
                    {simColumns.ipEntities.length > 0 && (
                      <div className="sim-sub-header res">TIER</div>
                    )}
                    <div className="sim-sub-header final">DECISION</div>

                    {/* Data rows */}
                    {(simResult.results ?? []).map((pr, rowIndex) => {
                      if (pr === null)
                        return (
                          <div key={rowIndex} role="alert">
                            Policy result unavailable
                          </div>
                        );
                      const rangeEnts = pr.range_evaluation?.entities ?? [];
                      const groupEnts = pr.group_evaluation?.entities ?? [];
                      const ipEnts = pr.ip_evaluation?.entities ?? [];

                      return (
                        <SimRow
                          key={pr.domain}
                          pr={pr}
                          rangeEnts={rangeEnts}
                          groupEnts={groupEnts}
                          ipEnts={ipEnts}
                          expectedRangeCount={simColumns.rangeEntities.length}
                          expectedGroupCount={simColumns.groupEntities.length}
                          expectedIpCount={simColumns.ipEntities.length}
                          renderEntityBadge={renderEntityBadge}
                          renderTierBadge={renderTierBadge}
                          renderEntityDetail={renderEntityDetail}
                          renderTierSource={renderTierSource}
                          renderFinalDecision={renderFinalDecision}
                        />
                      );
                    })}
                  </div>
                </div>
              </div>
            ) : (
              <div className="empty-state">
                <svg
                  width="48"
                  height="48"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1"
                  style={{ opacity: 0.3 }}
                >
                  <polyline points="22 12 18 12 15 21 9 3 6 12 2 12" />
                </svg>
                <div>Enter a client IP and domains, then click Simulate Policy</div>
              </div>
            ))}
        </div>
      </div>
    </div>
  );
}
