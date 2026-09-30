/** Render a label from explicit model values; the controller owns clock sampling. */
export function relativeTime(nowMs: number | null, dateStr?: string): string {
  if (!dateStr) return 'Never';
  if (nowMs === null) return '-';
  const diffMin = Math.floor((nowMs - new Date(dateStr).getTime()) / 60000);
  if (diffMin < 1) return 'Just now';
  if (diffMin < 60) return `${String(diffMin)}m ago`;
  const diffHrs = Math.floor(diffMin / 60);
  if (diffHrs < 24) return `${String(diffHrs)}h ago`;
  return `${String(Math.floor(diffHrs / 24))}d ago`;
}
