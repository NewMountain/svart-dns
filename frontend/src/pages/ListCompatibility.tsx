import { useState } from 'react';
import type { ListCompatibilityPage, ListCompatibilitySummary } from '../api/generated';
import { getApiBlocklistsIdCompatibility } from '../api/operations';

export default function ListCompatibility({
  id,
  summary,
}: {
  id: number;
  summary: ListCompatibilitySummary;
}) {
  const [page, setPage] = useState<ListCompatibilityPage | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [requestedOffset, setRequestedOffset] = useState(0);
  async function load(offset: number) {
    setRequestedOffset(offset);
    setLoading(true);
    setError(null);
    try {
      setPage(await getApiBlocklistsIdCompatibility(id, { limit: '50', offset: String(offset) }));
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Compatibility report unavailable.');
    } finally {
      setLoading(false);
    }
  }
  const currentSummary = page?.summary ?? summary;
  if (!currentSummary.assessed)
    return <div className="list-url">Compatibility not assessed; refresh this list.</div>;
  return (
    <details
      className="list-compatibility"
      onToggle={(event) => {
        if (event.currentTarget.open && !page && !loading) void load(0);
      }}
    >
      <summary>
        {currentSummary.applied.toLocaleString()} applied ·{' '}
        {currentSummary.unsupported.toLocaleString()} unsupported ·{' '}
        {currentSummary.invalid.toLocaleString()} invalid
      </summary>
      <p className="list-url">
        Assessed {new Date(currentSummary.assessed_at).toLocaleString()}. Applied counts effective
        rules; blocklist exceptions are excluded.
      </p>
      {loading && <p role="status">Loading diagnostics…</p>}
      {error && (
        <div role="alert">
          {error}{' '}
          <button
            className="pagination-btn"
            disabled={loading}
            onClick={() => void load(requestedOffset)}
          >
            Retry diagnostics
          </button>
        </div>
      )}
      {page && (
        <>
          {page.total === 0 && <p>No skipped rules in this generation.</p>}
          {page.total > 0 && (page.diagnostics ?? []).length === 0 && (
            <p>
              This generation has changed.{' '}
              <button className="pagination-btn" disabled={loading} onClick={() => void load(0)}>
                First diagnostics page
              </button>
            </p>
          )}
          <ol start={page.offset + 1}>
            {(page.diagnostics ?? []).map((diagnostic, i) => (
              <li key={`${String(page.offset + i)}-${String(diagnostic.line)}`}>
                <div>
                  Line {diagnostic.line}: {diagnostic.reason}
                </div>
                <code>{diagnostic.rule}</code>
              </li>
            ))}
          </ol>
          {page.total > page.limit && (
            <div className="compatibility-pagination">
              <button
                className="pagination-btn"
                aria-label="Previous diagnostics"
                disabled={loading || page.offset === 0}
                onClick={() => void load(Math.max(0, page.offset - page.limit))}
              >
                Previous
              </button>
              <span>
                {page.offset + 1}–{Math.min(page.offset + page.limit, page.total)} of {page.total}
              </span>
              <button
                className="pagination-btn"
                aria-label="Next diagnostics"
                disabled={loading || page.offset + page.limit >= page.total}
                onClick={() => void load(page.offset + page.limit)}
              >
                Next
              </button>
            </div>
          )}
        </>
      )}
    </details>
  );
}
