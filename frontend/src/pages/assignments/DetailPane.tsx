import type { Client, PolicyView as PolicySummary } from '../../api/generated';
import '../../styles/pages/logs.css';
import '../../styles/pages/tiers.css';
import { PolicyDetailView } from './BundleDetail';
import { ClientDetailView } from './ClientDetail';
import { GroupDetailView } from './GroupDetail';
import { TierTab } from './navigation';
import { RangeDetailView } from './NetworkDetail';

export function DetailPane({
  tab,
  selectedId,
  clients,
  policies,
  onDelete,
}: {
  tab: TierTab;
  selectedId: string | null;
  clients: Client[] | null;
  policies: PolicySummary[] | null;
  onDelete: () => void;
}) {
  if (!selectedId) {
    const label =
      tab === 'policies'
        ? 'bundle'
        : tab === 'ranges'
          ? 'network'
          : tab === 'groups'
            ? 'group'
            : 'client';
    return (
      <div className="detail-pane">
        <div className="tiers-empty">
          <svg
            width="48"
            height="48"
            viewBox="0 0 24 24"
            fill="none"
            stroke="var(--text-muted)"
            strokeWidth="1"
            opacity="0.3"
          >
            <path d="M12 2L2 7l10 5 10-5-10-5z" />
            <path d="M2 17l10 5 10-5" />
            <path d="M2 12l10 5 10-5" />
          </svg>
          <p className="text-muted">Select a {label} to view details</p>
        </div>
      </div>
    );
  }

  if (tab === 'policies') return <PolicyDetailView id={selectedId} onDelete={onDelete} />;
  if (tab === 'ranges')
    return <RangeDetailView id={selectedId} policies={policies} onDelete={onDelete} />;
  if (tab === 'groups')
    return (
      <GroupDetailView id={selectedId} clients={clients} policies={policies} onDelete={onDelete} />
    );
  return <ClientDetailView ip={selectedId} policies={policies} />;
}
