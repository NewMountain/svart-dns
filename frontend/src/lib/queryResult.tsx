// Shared rendering for a DNS query log's result badge and decision source,
// used by the Logs table, the Dashboard live query log, and the Assignments
// recent-activity widget. Previously duplicated three times (LogEntry,
// QueryLog, RecentLog) with the same branching logic — kept here once so a
// future policy-reason change only needs one edit.
import Badge from '../components/Badge';

export interface QueryResultLike {
  blocked: boolean;
  upstream?: string;
  result_reason?: string;
  result_tier?: string;
  block_tier?: string;
  result_entity?: string;
  result_list_name?: string;
  block_list_name?: string;
}

export function resultBadge(log: QueryResultLike) {
  const reason = log.result_reason;
  if (reason === 'custom_block') return <Badge variant="block">C.BLOCK</Badge>;
  if (reason === 'published_block') return <Badge variant="block">BLOCK</Badge>;
  if (reason === 'custom_allow') return <Badge variant="allow">C.ALLOW</Badge>;
  if (reason === 'published_allow') return <Badge variant="allow">P.ALLOW</Badge>;
  if (reason === 'rewrite') return <Badge variant="rewrite">RWT</Badge>;
  if (log.blocked) return <Badge variant="block">BLOCK</Badge>;
  if (log.upstream === 'rewrite') return <Badge variant="rewrite">RWT</Badge>;
  return <Badge variant="allow">ALLOW</Badge>;
}

export function formatSource(log: QueryResultLike) {
  const listName = log.result_list_name || log.block_list_name;
  const tier = log.result_tier || log.block_tier;
  const entity = log.result_entity;
  const reason = log.result_reason;

  if (listName && tier && tier !== 'default') {
    const tierLabel =
      tier === 'group'
        ? `Group ${entity || ''}`.trim()
        : tier === 'range'
          ? `Range ${entity || ''}`.trim()
          : tier === 'ip'
            ? `IP ${entity || ''}`.trim()
            : tier;
    return (
      <>
        <span className="source-list">{listName}</span>
        <span className="source-sep"> · </span>
        <span className="source-tier">{tierLabel}</span>
      </>
    );
  }

  if (
    reason &&
    (reason.includes('block') || reason.includes('allow')) &&
    tier &&
    tier !== 'default'
  ) {
    const action = reason.includes('block') ? 'Custom block' : 'Custom allow';
    const tierLabel =
      tier === 'group'
        ? `Group ${entity || ''}`.trim()
        : tier === 'range'
          ? `Range ${entity || ''}`.trim()
          : tier === 'ip'
            ? `IP ${entity || ''}`.trim()
            : tier;
    return (
      <>
        <span className="source-list">{action}</span>
        <span className="source-sep"> · </span>
        <span className="source-tier">{tierLabel}</span>
      </>
    );
  }

  if (listName) return <span className="source-list">{listName}</span>;
  if (log.upstream === 'rewrite') return <>rewrite</>;
  if (log.upstream === 'cache') return <>cache</>;
  if (log.upstream) return <>{log.upstream}</>;
  return <>-</>;
}
