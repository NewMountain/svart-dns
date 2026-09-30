import { type ReactNode } from 'react';
import type {
  BlocklistUniqueStats as BlocklistStats,
  PolicyDetailView as PolicyDetail,
  PolicyReference as PolicyRef,
  PolicyView as PolicySummary,
  RecentLogView as RecentLog,
  TierEvaluation as TierEval,
} from '../../api/generated';
import Badge from '../../components/Badge';
import ContextMenu from '../../components/ContextMenu';
import Toggle from '../../components/Toggle';
import { formatSource, resultBadge } from '../../lib/queryResult';
import '../../styles/pages/logs.css';
import '../../styles/pages/tiers.css';
import { useListToggleSectionController } from './ListToggleSection.controller';
import { ListItem } from './navigation';
import { useRecentActivityWidgetController } from './RecentActivityWidget.controller';

export function PencilIcon() {
  return (
    <svg
      className="edit-icon"
      width="14"
      height="14"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M17 3a2.85 2.85 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z" />
      <path d="m15 5 4 4" />
    </svg>
  );
}

export function DeleteButton({
  confirming,
  onConfirm,
  onArm,
  onCancel,
}: {
  confirming: boolean;
  onConfirm: () => void;
  onArm: () => void;
  onCancel: () => void;
}) {
  if (confirming) {
    return (
      <div style={{ display: 'flex', gap: 6 }}>
        <button
          className="btn btn-danger"
          style={{ padding: '6px 12px', fontSize: '0.8rem' }}
          onClick={onConfirm}
        >
          Confirm
        </button>
        <button
          className="btn"
          style={{ padding: '6px 12px', fontSize: '0.8rem' }}
          onClick={onCancel}
        >
          Cancel
        </button>
      </div>
    );
  }
  return (
    <button
      className="btn btn-danger"
      style={{ padding: '6px 12px', fontSize: '0.8rem' }}
      onClick={onArm}
    >
      Delete
    </button>
  );
}

export function PolicySelector({
  current,
  policies,
  onAssign,
  onRemove,
}: {
  current?: PolicyRef;
  policies: PolicySummary[] | null;
  onAssign: (policyId: number) => void;
  onRemove: () => void;
}) {
  function handleChange(e: React.ChangeEvent<HTMLSelectElement>) {
    const val = e.target.value;
    if (val === '') {
      if (current) onRemove();
    } else {
      onAssign(Number(val));
    }
  }

  return (
    <div className="widget policy-selector-widget">
      <div className="widget-title">Bundle</div>
      <select
        className="form-input policy-selector"
        value={current?.id ?? ''}
        onChange={handleChange}
      >
        <option value="">No bundle</option>
        {(policies ?? []).map((p) => (
          <option key={p.id} value={p.id}>
            {p.name}
          </option>
        ))}
      </select>
    </div>
  );
}

export function CustomRuleEditor({
  newRule,
  setNewRule,
  ruleType,
  setRuleType,
  onAdd,
  customBlocked,
  customAllowed,
  onRemove,
  error,
  extra,
  showEmptyState = true,
}: {
  newRule: string;
  setNewRule: (v: string) => void;
  ruleType: 'block' | 'allow';
  setRuleType: (v: 'block' | 'allow') => void;
  onAdd: () => void;
  customBlocked: string[];
  customAllowed: string[];
  onRemove: (domain: string, type: 'block' | 'allow') => void;
  error?: string | null;
  extra?: ReactNode;
  showEmptyState?: boolean;
}) {
  return (
    <div className="widget">
      <div className="widget-title">Custom Rules</div>
      <div className="custom-rule-inputs" style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        <input
          className="form-input custom-rule-domain"
          value={newRule}
          onChange={(e) => {
            setNewRule(e.target.value);
          }}
          placeholder="domain.com"
          onKeyDown={(e) => {
            if (e.key === 'Enter') onAdd();
          }}
        />
        <select
          className="form-input"
          value={ruleType}
          onChange={(e) => {
            setRuleType(e.target.value === 'allow' ? 'allow' : 'block');
          }}
          style={{ width: 100 }}
        >
          <option value="block">Block</option>
          <option value="allow">Allow</option>
        </select>
        <button className="btn btn-primary" style={{ padding: '8px 16px' }} onClick={onAdd}>
          Add
        </button>
      </div>
      {error && (
        <div className="rule-error" role="alert">
          {error}
        </div>
      )}
      {extra}
      {customBlocked.map((d) => (
        <div className="rule-row" key={`b-${d}`}>
          <span style={{ fontFamily: 'JetBrains Mono, monospace', fontSize: '0.85rem' }}>{d}</span>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span className="rule-type-block">BLOCK</span>
            <button
              className="btn-icon"
              aria-label={`Remove ${d} custom block rule`}
              onClick={() => {
                onRemove(d, 'block');
              }}
              style={{ color: 'var(--red)', padding: 4 }}
            >
              x
            </button>
          </div>
        </div>
      ))}
      {customAllowed.map((d) => (
        <div className="rule-row" key={`a-${d}`}>
          <span style={{ fontFamily: 'JetBrains Mono, monospace', fontSize: '0.85rem' }}>{d}</span>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span className="rule-type-allow">ALLOW</span>
            <button
              className="btn-icon"
              aria-label={`Remove ${d} custom allow rule`}
              onClick={() => {
                onRemove(d, 'allow');
              }}
              style={{ color: 'var(--red)', padding: 4 }}
            >
              x
            </button>
          </div>
        </div>
      ))}
      {showEmptyState && customBlocked.length === 0 && customAllowed.length === 0 && (
        <span className="text-sm text-muted">No custom rules</span>
      )}
    </div>
  );
}

export function StatsWidget({
  totalQueries,
  avgLatency,
  firstSeen,
  lastSeen,
}: {
  totalQueries: number;
  avgLatency: number;
  firstSeen: string;
  lastSeen: string;
}) {
  return (
    <div className="widget">
      <div className="widget-title">Stats</div>
      <div style={{ display: 'flex', gap: 32, fontSize: '0.85rem', flexWrap: 'wrap' }}>
        <div>
          <span className="text-muted">Queries:</span>{' '}
          <span className="font-mono">{totalQueries.toLocaleString()}</span>
        </div>
        <div>
          <span className="text-muted">Avg Latency:</span>{' '}
          <span className="font-mono">{(avgLatency / 1000).toFixed(1)}ms</span>
        </div>
        <div>
          <span className="text-muted">First Seen:</span>{' '}
          <span className="font-mono">{firstSeen || 'Never'}</span>
        </div>
        <div>
          <span className="text-muted">Last Seen:</span>{' '}
          <span className="font-mono">{lastSeen || 'Never'}</span>
        </div>
      </div>
    </div>
  );
}

function renderTierBlock(label: string, tier: TierEval | undefined, isWinner: boolean) {
  if (!tier || (tier.entities ?? []).length === 0) {
    return (
      <div className={`eval-tier${isWinner ? ' eval-winner' : ''}`}>
        <div className="eval-tier-header">
          <span className="eval-tier-label">{label}</span>
          <span className="eval-no-match">no match</span>
        </div>
      </div>
    );
  }
  return (
    <div className={`eval-tier${isWinner ? ' eval-winner' : ''}`}>
      <div className="eval-tier-header">
        <span className="eval-tier-label">{label}</span>
        {tier.result && (
          <Badge variant={tier.result === 'block' ? 'block' : 'allow'}>
            {tier.result.toUpperCase()}
          </Badge>
        )}
        {isWinner && <span className="eval-winner-tag">WINNER</span>}
      </div>
      {(tier.entities ?? []).map((ent, i) => (
        <div className="eval-entity" key={i}>
          <span className="eval-entity-name">{ent.name || '(unnamed)'}</span>
          {ent.result ? (
            <span className={`eval-entity-result eval-${ent.result}`}>{ent.result}</span>
          ) : (
            <span className="eval-entity-result eval-none">no decision</span>
          )}
          {ent.published_list && (
            <span className="eval-match">
              {ent.published_list.list_name} matched <code>{ent.published_list.rule}</code>
            </span>
          )}
          {ent.custom_rule && (
            <span className="eval-match">
              custom {ent.custom_rule.action}: <code>{ent.custom_rule.rule}</code>
            </span>
          )}
        </div>
      ))}
    </div>
  );
}

export function RecentActivityWidget({ logs }: { logs: RecentLog[] }) {
  const model = useRecentActivityWidgetController({ logs });
  return <RecentActivityWidgetView model={model} />;
}

export function PolicyRules({ policyData }: { policyData: PolicyDetail | null }) {
  if (!policyData) return null;
  const blocked = policyData.custom_blocked;
  const allowed = policyData.custom_allowed;
  if ((blocked ?? []).length === 0 && (allowed ?? []).length === 0) return null;

  return (
    <>
      {(blocked ?? []).map((d) => (
        <div className="rule-row from-policy" key={`pb-${d}`}>
          <span style={{ fontFamily: 'JetBrains Mono, monospace', fontSize: '0.85rem' }}>{d}</span>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span className="rule-type-block">BLOCK</span>
            <span className="policy-source-badge">Bundle</span>
          </div>
        </div>
      ))}
      {(allowed ?? []).map((d) => (
        <div className="rule-row from-policy" key={`pa-${d}`}>
          <span style={{ fontFamily: 'JetBrains Mono, monospace', fontSize: '0.85rem' }}>{d}</span>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span className="rule-type-allow">ALLOW</span>
            <span className="policy-source-badge">Bundle</span>
          </div>
        </div>
      ))}
    </>
  );
}

export function ListToggleSection({
  title,
  items,
  stats,
  onToggle,
  lockedIds,
  policyName,
}: {
  title: string;
  items: ListItem[];
  stats?: BlocklistStats;
  onToggle: (id: number, assigned: boolean) => void;
  lockedIds?: Set<number>;
  policyName?: string;
}) {
  const model = useListToggleSectionController({
    title,
    items,
    stats,
    onToggle,
    lockedIds,
    policyName,
  });
  return <ListToggleSectionView model={model} />;
}

export function OctetVisualizer({ cidr }: { cidr: string }) {
  const parts = cidr.split('/');
  const ip = parts[0] ?? '';
  const prefix = parseInt(parts[1] ?? '32', 10);
  const octets = ip.split('.').map(Number);

  const octetInfo = octets.map((val, i) => {
    const bitsInOctet = Math.min(Math.max(prefix - i * 8, 0), 8);
    const fixed = bitsInOctet === 8;
    const partial = bitsInOctet > 0 && bitsInOctet < 8;
    const mask = (0xff << (8 - bitsInOctet)) & 0xff;
    const low = val & mask;
    const high = low | (~mask & 0xff);

    return {
      value: val,
      fixed,
      display: fixed ? String(val) : partial ? `${String(low)}-${String(high)}` : '0-255',
    };
  });

  return (
    <>
      <div className="octet-container">
        {octetInfo.map((o, i) => (
          <span key={i} style={{ display: 'contents' }}>
            <div className={`octet-box ${o.fixed ? 'locked' : 'range'}`}>{o.display}</div>
            {i < 3 && <span className="octet-dot">.</span>}
          </span>
        ))}
      </div>
      <div className="octet-scope">
        Covers <strong>{cidr}</strong> — {Math.pow(2, 32 - prefix)} addresses
      </div>
    </>
  );
}

function RecentActivityWidgetView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useRecentActivityWidgetController>>;
}) {
  const {
    logs,
    action,
    expandedId,
    evalDetail,
    evalLoading,
    contextMenu,
    setContextMenu,
    toggleExpand,
    openContextMenu,
    allowDomain,
    blockDomain,
    COL_COUNT,
  } = model;
  return (
    <div className="widget">
      <div className="widget-title">Recent Activity</div>
      <div className="logs-table-wrapper" style={{ height: 680 }}>
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
              <th>Policy</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {logs.length === 0 ? (
              <tr>
                <td colSpan={COL_COUNT} className="empty-state">
                  No recent activity
                </td>
              </tr>
            ) : (
              logs.flatMap((log) => {
                const rows = [
                  <tr key={log.id} className={expandedId === log.id ? 'expanded' : ''}>
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
                    <td>{resultBadge(log)}</td>
                    <td className="latency-cell">
                      {log.latency_microseconds > 1000
                        ? `${(log.latency_microseconds / 1000).toFixed(0)}ms`
                        : `${String(log.latency_microseconds)}µs`}
                    </td>
                    <td className="source-cell">{formatSource(log)}</td>
                    <td>
                      <button
                        className={`expand-btn${expandedId === log.id ? ' open' : ''}`}
                        onClick={action(() => toggleExpand(log.id))}
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
                          openContextMenu(e, log);
                        }}
                        aria-label="More actions"
                      >
                        &#9881;
                      </button>
                    </td>
                  </tr>,
                ];
                if (expandedId === log.id) {
                  rows.push(
                    <tr key={`${String(log.id)}-eval`} className="eval-row">
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
      </div>

      {contextMenu && (
        <ContextMenu
          x={contextMenu.x}
          y={contextMenu.y}
          header={contextMenu.log.query_name}
          items={[
            {
              label: `Allow for ${contextMenu.log.client_alias || contextMenu.log.client_ip}`,
              onClick: action(() =>
                allowDomain(contextMenu.log.query_name, contextMenu.log.client_ip),
              ),
              success: true,
            },
            {
              label: `Block for ${contextMenu.log.client_alias || contextMenu.log.client_ip}`,
              onClick: action(() =>
                blockDomain(contextMenu.log.query_name, contextMenu.log.client_ip),
              ),
              danger: true,
            },
            {
              label: 'Copy Domain',
              onClick: action(() => navigator.clipboard.writeText(contextMenu.log.query_name)),
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

function ListToggleSectionView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useListToggleSectionController>>;
}) {
  const {
    title,
    items,
    stats,
    onToggle,
    lockedIds,
    policyName,
    collapsed,
    setCollapsed,
    assigned,
    rawTotal,
    newMap,
  } = model;
  return (
    <div className="widget">
      <div
        className="widget-title accordion-title"
        onClick={() => {
          setCollapsed(!collapsed);
        }}
        style={{ cursor: 'pointer', userSelect: 'none' }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <svg
            className={`accordion-chevron${collapsed ? '' : ' open'}`}
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
          >
            <polyline points="6 9 12 15 18 9" />
          </svg>
          {title}
        </div>
      </div>
      {!collapsed && (
        <>
          <div className="blocklist-toggle-list">
            {items.map((item) => {
              const locked = lockedIds?.has(item.id) ?? false;
              const effectiveAssigned = item.is_assigned || locked;
              return (
                <div
                  className={`blocklist-toggle-row${effectiveAssigned ? ' assigned' : ''}${locked ? ' locked' : ''}`}
                  key={item.id}
                >
                  <Toggle
                    checked={effectiveAssigned}
                    onChange={() => {
                      onToggle(item.id, item.is_assigned);
                    }}
                    size="sm"
                    disabled={locked}
                  />
                  <span className="blocklist-toggle-name">
                    {item.alias}
                    {locked && policyName && (
                      <span className="policy-source-badge">Bundle: {policyName}</span>
                    )}
                    {effectiveAssigned && item.id in newMap && (
                      <span className="blocklist-unique-badge">
                        +{(newMap[item.id] ?? 0).toLocaleString()} new
                      </span>
                    )}
                  </span>
                  <span className="blocklist-toggle-count">
                    {item.domain_count.toLocaleString()}
                  </span>
                </div>
              );
            })}
            {items.length === 0 && (
              <span className="text-sm text-muted">No {title.toLowerCase()} available</span>
            )}
          </div>
          {assigned.length > 0 && stats && stats.unique_total > 0 && (
            <div className="blocklist-total-row">
              <span className="blocklist-total-label">Total unique domains</span>
              <span className="blocklist-total-count">{stats.unique_total.toLocaleString()}</span>
              {rawTotal > stats.unique_total && (
                <span className="blocklist-overlap-note">
                  {((1 - stats.unique_total / rawTotal) * 100).toFixed(0)}% overlap
                </span>
              )}
            </div>
          )}
        </>
      )}
    </div>
  );
}
