// Shared helpers for building ApexCharts `tooltip.custom` markup.
//
// ApexCharts renders the string returned by `tooltip.custom` via innerHTML.
// Values such as DNS query names and client aliases are attacker-controlled
// (any LAN device can cause an arbitrary domain string to be logged), so
// every dynamic value interpolated into one of these templates MUST be
// escaped first — otherwise hovering a chart executes stored XSS in the
// admin's session.

import type { ApexOptions } from 'apexcharts';

type ApexTooltip = NonNullable<ApexOptions['tooltip']>;

export interface DonutSlice {
  label: string;
  value: number;
  color: string;
  details?: { domain: string; count: number }[];
}

/** Escapes HTML metacharacters so a string is safe to interpolate into a raw markup template literal. */
export function escapeHtml(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

type ApexTooltipY = Exclude<ApexTooltip['y'], unknown[] | undefined>;

function escapedYTooltip(y: ApexTooltipY | undefined): ApexTooltipY {
  const title = y?.title;
  return {
    ...y,
    title: {
      ...y?.title,
      formatter: (seriesName: string, opts?: unknown) =>
        escapeHtml(title?.formatter ? title.formatter(seriesName, opts) : seriesName),
    },
  };
}

/**
 * Makes every chart string ApexCharts writes with innerHTML safe: legend
 * entries, the tooltip title and x-axis tooltip (the x category), and the
 * tooltip series name. Axis labels and data labels are SVG text nodes
 * (textContent) and need no escaping; escaping them would print entities.
 * Existing formatters are kept and their output is escaped.
 */
export function withEscapedChartText(options: ApexOptions): ApexOptions {
  const legend = options.legend;
  const tooltip = options.tooltip ?? {};
  const x = tooltip.x;
  return {
    ...options,
    legend: {
      ...options.legend,
      formatter: (name: string, opts?: unknown) =>
        escapeHtml(legend?.formatter ? legend.formatter(name, opts) : name),
    },
    tooltip: {
      ...tooltip,
      x: {
        ...tooltip.x,
        formatter: (value: number, opts?: unknown) =>
          escapeHtml(x?.formatter ? x.formatter(value, opts) : String(value)),
      },
      y: Array.isArray(tooltip.y) ? tooltip.y.map(escapedYTooltip) : escapedYTooltip(tooltip.y),
    },
  };
}

export function donutTooltipFormatter(slices: DonutSlice[]) {
  return function ({ seriesIndex }: { series: number[]; seriesIndex: number; w: unknown }) {
    const slice = slices[seriesIndex];
    if (!slice) {
      return '';
    }

    const header =
      '<div style="background:#09090b;border:1px solid #27272a;border-radius:4px;padding:8px 12px;font-family:JetBrains Mono,monospace;font-size:12px;color:#f4f4f5;max-height:320px;overflow-y:auto">';
    if (slice.details && slice.details.length > 0) {
      const rows = slice.details
        .map(
          (domain) =>
            `<div style="display:flex;justify-content:space-between;gap:16px;padding:1px 0"><span style="color:#a1a1aa">${escapeHtml(domain.domain)}</span><span>${domain.count.toLocaleString()}</span></div>`,
        )
        .join('');
      return `${header}<div style="font-weight:600;margin-bottom:4px;color:#71717a">${escapeHtml(slice.label)} — ${slice.value.toLocaleString()} total</div>${rows}</div>`;
    }

    return `${header}<div style="display:flex;justify-content:space-between;gap:16px"><span>${escapeHtml(slice.label)}</span><span style="font-weight:600">${slice.value.toLocaleString()}</span></div></div>`;
  };
}

export function clientActivityTooltipFormatter({
  series,
  dataPointIndex,
  w,
}: {
  series: number[][];
  dataPointIndex: number;
  w: { globals: { labels: string[] } };
}) {
  const allowed = (series[0] && series[0][dataPointIndex]) || 0;
  const blocked = (series[1] && series[1][dataPointIndex]) || 0;
  const total = allowed + blocked;
  const allowPct = total > 0 ? ((allowed / total) * 100).toFixed(1) : '0.0';
  const blockPct = total > 0 ? ((blocked / total) * 100).toFixed(1) : '0.0';
  const client = escapeHtml(w.globals.labels[dataPointIndex] || '');
  return `<div style="background:#09090b;border:1px solid #27272a;border-radius:4px;padding:8px 12px;font-family:JetBrains Mono,monospace;font-size:12px;color:#f4f4f5">
          <div style="margin-bottom:6px;font-weight:600">${client}</div>
          <div style="display:flex;align-items:center;gap:6px;margin-bottom:3px">
            <span style="width:8px;height:8px;border-radius:50%;background:#a9dc76;display:inline-block"></span>
            Allowed: ${allowed.toLocaleString()} (${allowPct}%)
          </div>
          <div style="display:flex;align-items:center;gap:6px">
            <span style="width:8px;height:8px;border-radius:50%;background:#ff6188;display:inline-block"></span>
            Blocked: ${blocked.toLocaleString()} (${blockPct}%)
          </div>
          <div style="margin-top:6px;color:#71717a;border-top:1px solid #27272a;padding-top:4px">Total: ${total.toLocaleString()}</div>
        </div>`;
}
