import TopBar from '../components/TopBar';
import { useInvestigationController } from './Investigation.controller';

import '../styles/pages/investigation.css';
import TEMPLATES from './investigation-templates.json';

// ── CodeMirror theme matching svart design system ──

// ── Starter query templates ──

// Shared with the API contract tests so every shipped template is exercised.
// ── Types ──

// ── Component ──

export default function Investigation() {
  const model = useInvestigationController();
  return <InvestigationView model={model} />;
}

function InvestigationView({
  model,
}: {
  model: NonNullable<ReturnType<typeof useInvestigationController>>;
}) {
  const {
    action,
    tables,
    openTables,
    editorRef,
    loading,
    result,
    error,
    timeout,
    setTimeout,
    editorHeight,
    runQuery,
    insertAtCursor,
    loadTemplate,
    toggleTable,
    onResizeStart,
  } = model;
  return (
    <div className="investigation-page">
      <TopBar title="Investigation" />

      <div className="investigation-layout">
        {/* ── Schema explorer ── */}
        <div className="schema-panel">
          <div className="schema-header">Schema</div>
          <div className="schema-tree">
            {tables.length === 0 ? (
              <div style={{ padding: '16px', color: 'var(--text-muted)', fontSize: '0.8rem' }}>
                Loading schema...
              </div>
            ) : (
              tables.map((table) => (
                <div key={table.name} className="schema-table">
                  <div
                    className={`schema-table-header${openTables.has(table.name) ? ' open' : ''}`}
                    onClick={() => {
                      toggleTable(table.name);
                    }}
                  >
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                      <polyline points="9 18 15 12 9 6" />
                    </svg>
                    <svg
                      className="schema-table-icon"
                      viewBox="0 0 24 24"
                      fill="none"
                      stroke="currentColor"
                      strokeWidth="1.5"
                    >
                      <rect x="3" y="3" width="18" height="18" rx="2" />
                      <line x1="3" y1="9" x2="21" y2="9" />
                      <line x1="9" y1="3" x2="9" y2="21" />
                    </svg>
                    {table.name}
                    <span
                      style={{
                        marginLeft: 'auto',
                        fontSize: '0.65rem',
                        color: 'var(--text-muted)',
                      }}
                    >
                      {table.type}
                    </span>
                  </div>
                  <div className={`schema-columns${openTables.has(table.name) ? ' open' : ''}`}>
                    {(table.columns ?? []).map((col) => (
                      <div
                        key={col.name}
                        className="schema-column"
                        onClick={() => {
                          insertAtCursor(col.name);
                        }}
                        title={`Click to insert "${col.name}"`}
                      >
                        <span>{col.name}</span>
                        <span className="schema-column-type">{col.type}</span>
                      </div>
                    ))}
                  </div>
                </div>
              ))
            )}
          </div>
        </div>

        {/* ── Right side: editor + results ── */}
        <div className="investigation-right">
          {/* Editor toolbar */}
          <div className="editor-panel" style={{ height: editorHeight, minHeight: 100 }}>
            <div className="editor-toolbar">
              <select
                className="template-select"
                defaultValue=""
                onChange={(e) => {
                  const idx = Number(e.target.value);
                  if (!isNaN(idx)) loadTemplate(idx);
                  e.target.value = '';
                }}
              >
                <option value="" disabled>
                  Starter queries...
                </option>
                {TEMPLATES.map((t, i) => (
                  <option key={i} value={i}>
                    {t.label}
                  </option>
                ))}
              </select>

              <select
                className="timeout-select"
                value={timeout}
                onChange={(e) => {
                  setTimeout(Number(e.target.value));
                }}
              >
                <option value={10}>10s</option>
                <option value={30}>30s</option>
                <option value={60}>60s</option>
              </select>

              <div style={{ flex: 1 }} />

              <button
                className="run-btn"
                onClick={() => {
                  action(runQuery)();
                }}
                disabled={loading}
              >
                {loading ? (
                  <div className="spinner" />
                ) : (
                  <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor" stroke="none">
                    <polygon points="5 3 19 12 5 21 5 3" />
                  </svg>
                )}
                Run <span className="kbd">Ctrl+Enter</span>
              </button>
            </div>

            <div className="editor-container" ref={editorRef} />
          </div>

          {/* Resize handle */}
          <div className="resize-handle" onMouseDown={onResizeStart} />

          {/* Results */}
          <div className="results-panel">
            {/* Toolbar */}
            {result && (
              <div className="results-toolbar">
                <div className="results-stat">
                  Rows: <strong>{result.row_count.toLocaleString()}</strong>
                </div>
                <div className="results-stat">
                  Duration: <strong>{result.duration_ms}ms</strong>
                </div>
                <div className="results-stat">
                  Columns: <strong>{(result.columns ?? []).length}</strong>
                </div>
              </div>
            )}

            {/* Error */}
            {error && (
              <div className="query-error">
                <svg
                  width="16"
                  height="16"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                >
                  <circle cx="12" cy="12" r="10" />
                  <line x1="15" y1="9" x2="9" y2="15" />
                  <line x1="9" y1="9" x2="15" y2="15" />
                </svg>
                <span>{error}</span>
              </div>
            )}

            {/* Results table */}
            {result && result.row_count > 0 ? (
              <div className="results-scroll">
                <table className="results-table">
                  <thead>
                    <tr>
                      {(result.columns ?? []).map((col) => (
                        <th key={col}>{col}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {(result.rows ?? []).map((row, ri) => (
                      <tr key={ri}>
                        {(row ?? []).map((cell, ci) => (
                          <td key={ci} className={cell === null ? 'null-cell' : undefined}>
                            {cell === null
                              ? 'NULL'
                              : typeof cell === 'object'
                                ? JSON.stringify(cell)
                                : String(cell)}
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : result && result.row_count === 0 ? (
              <div className="results-empty">
                <svg
                  width="40"
                  height="40"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1"
                >
                  <circle cx="12" cy="12" r="10" />
                  <line x1="8" y1="12" x2="16" y2="12" />
                </svg>
                <div>Query returned no rows</div>
              </div>
            ) : !error ? (
              <div className="results-empty">
                <svg
                  width="48"
                  height="48"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="1"
                >
                  <polyline points="16 18 22 12 16 6" />
                  <polyline points="8 6 2 12 8 18" />
                  <line x1="14" y1="4" x2="10" y2="20" />
                </svg>
                <div>
                  Write a SELECT query against query_logs and press Ctrl+Enter to investigate
                </div>
              </div>
            ) : null}
          </div>
        </div>
      </div>
    </div>
  );
}
