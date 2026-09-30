import { autocompletion, completionKeymap } from '@codemirror/autocomplete';
import { defaultKeymap, history, historyKeymap } from '@codemirror/commands';
import { sql, SQLite } from '@codemirror/lang-sql';
import { defaultHighlightStyle, HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { Compartment, EditorState } from '@codemirror/state';
import {
  EditorView,
  highlightActiveLine,
  highlightActiveLineGutter,
  keymap,
  lineNumbers,
} from '@codemirror/view';
import { tags } from '@lezer/highlight';
import { useCallback, useEffect, useRef } from 'react';
import { getApiInvestigateSchema, postApiInvestigate } from '../api/operations';
import { useAction } from '../hooks/useAction';
import { useUiField } from '../hooks/useUiField';
import TEMPLATES from './investigation-templates.json';

const defaultTemplate = TEMPLATES[3];
if (!defaultTemplate) throw new Error('The default investigation template is missing');
const initialSql = `-- Write a SELECT query against query_logs. Ctrl+Enter to run.\n-- The query_logs view includes live + archived data.\n\n${defaultTemplate.sql}`;

const svartTheme = EditorView.theme({
  '&': {
    backgroundColor: '#1f1f22',
    color: '#e4e4e7',
    fontFamily: "'JetBrains Mono', monospace",
    fontSize: '13px',
  },
  '.cm-gutters': { backgroundColor: '#18181b', borderRight: '1px solid #27272a', color: '#71717a' },
  '.cm-activeLineGutter': { backgroundColor: '#27272a' },
  '.cm-activeLine': { backgroundColor: '#27272a22' },
  '.cm-cursor': { borderLeftColor: '#e4e4e7' },
  '.cm-selectionBackground': { backgroundColor: '#3f3f4655' },
  '&.cm-focused .cm-selectionBackground': { backgroundColor: '#3f3f4688' },
  '.cm-content': { caretColor: '#e4e4e7', padding: '8px 0' },
  '.cm-line': { padding: '0 8px' },
});
const svartHighlight = syntaxHighlighting(
  HighlightStyle.define([
    { tag: tags.keyword, color: '#ff6188' },
    { tag: tags.string, color: '#ffd866' },
    { tag: tags.number, color: '#ab9df2' },
    { tag: tags.comment, color: '#71717a' },
    { tag: tags.typeName, color: '#78dce8' },
    { tag: tags.operator, color: '#ff6188' },
    { tag: tags.punctuation, color: '#71717a' },
    { tag: tags.variableName, color: '#e4e4e7' },
    { tag: tags.function(tags.variableName), color: '#a9dc76' },
  ]),
);
export function useInvestigationController() {
  const action = useAction();
  const [tables, setTables] = useUiField('Investigation.tables', []);
  const [openTables, setOpenTables] = useUiField('Investigation.openTables', new Set());
  const editorRef = useRef<HTMLDivElement>(null);
  const viewRef = useRef<EditorView | null>(null);
  const [loading, setLoading] = useUiField('Investigation.loading', false);
  const [result, setResult] = useUiField('Investigation.result', null);
  const [error, setError] = useUiField('Investigation.error', null);
  const [timeout, setTimeout] = useUiField('Investigation.timeout', 30);
  const [sqlText, setSqlText] = useUiField('Investigation.sql', initialSql);
  const runQueryKeymapCompartment = useRef(new Compartment());
  const [editorHeight, setEditorHeight] = useUiField('Investigation.editorHeight', 240);
  const draggingRef = useRef(false);
  const startYRef = useRef(0);
  const startHeightRef = useRef(0);
  useEffect(() => {
    getApiInvestigateSchema()
      .then((data) => {
        setTables(data.tables ?? []);
        // Auto-expand first table
        const first = data.tables?.[0];
        if (first) setOpenTables(new Set([first.name]));
      })
      .catch((error: unknown) => {
        setError(error instanceof Error ? error.message : 'Could not load database schema');
      });
  }, [setError, setOpenTables, setTables]);
  const runQuery = useCallback(async () => {
    const query = sqlText.trim();
    if (!query) return;

    setLoading(true);
    setError(null);
    try {
      const data = await postApiInvestigate({ sql: query, timeout });
      setResult(data);
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : 'Query failed');
      setResult(null);
    }
    setLoading(false);
  }, [setError, setLoading, setResult, sqlText, timeout]);
  useEffect(() => {
    if (!editorRef.current) return;

    const state = EditorState.create({
      doc: initialSql,
      extensions: [
        EditorView.updateListener.of((update) => {
          if (update.docChanged) setSqlText(update.state.doc.toString());
        }),
        lineNumbers(),
        highlightActiveLine(),
        highlightActiveLineGutter(),
        history(),
        sql({ dialect: SQLite }),
        autocompletion(),
        runQueryKeymapCompartment.current.of([]),
        keymap.of([...defaultKeymap, ...historyKeymap, ...completionKeymap]),
        svartTheme,
        svartHighlight,
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        EditorView.lineWrapping,
      ],
    });

    const view = new EditorView({
      state,
      parent: editorRef.current,
    });

    viewRef.current = view;

    return () => {
      view.destroy();
      viewRef.current = null;
    };
  }, [setSqlText]);
  useEffect(() => {
    const view = viewRef.current;
    if (view && view.state.doc.toString() !== sqlText) {
      view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: sqlText } });
    }
  }, [sqlText]);
  useEffect(() => {
    const view = viewRef.current;
    if (!view) return;

    view.dispatch({
      effects: runQueryKeymapCompartment.current.reconfigure(
        keymap.of([
          {
            key: 'Ctrl-Enter',
            run: () => {
              void runQuery();
              return true;
            },
          },
          {
            key: 'Mod-Enter',
            run: () => {
              void runQuery();
              return true;
            },
          },
        ]),
      ),
    });
  }, [runQuery]);
  function insertAtCursor(text: string) {
    const view = viewRef.current;
    if (!view) return;
    const { from } = view.state.selection.main;
    view.dispatch({
      changes: { from, insert: text },
      selection: { anchor: from + text.length },
    });
    view.focus();
  }
  function loadTemplate(index: number) {
    const view = viewRef.current;
    if (!view || index < 0) return;
    const tmpl = TEMPLATES[index];
    if (!tmpl) return;
    setSqlText(tmpl.sql);
    view.focus();
  }
  function toggleTable(name: string) {
    setOpenTables((prev) => {
      const next = new Set(prev);
      if (next.has(name)) next.delete(name);
      else next.add(name);
      return next;
    });
  }
  function onResizeStart(e: React.MouseEvent) {
    e.preventDefault();
    draggingRef.current = true;
    startYRef.current = e.clientY;
    startHeightRef.current = editorHeight;

    function onMove(ev: MouseEvent) {
      if (!draggingRef.current) return;
      const delta = ev.clientY - startYRef.current;
      setEditorHeight(Math.max(100, Math.min(600, startHeightRef.current + delta)));
    }
    function onUp() {
      draggingRef.current = false;
      document.removeEventListener('mousemove', onMove);
      document.removeEventListener('mouseup', onUp);
    }
    document.addEventListener('mousemove', onMove);
    document.addEventListener('mouseup', onUp);
  }
  return {
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
  };
}
