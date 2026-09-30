import { describe, expect, it } from 'vitest';
import { withEscapedChartText } from './chartTooltips';

const hostile = '<svg onload=alert(1)>ads.example.com';
const escaped = '&lt;svg onload=alert(1)&gt;ads.example.com';

describe('withEscapedChartText', () => {
  it('escapes the legend, tooltip title and series name when no formatter exists', () => {
    const out = withEscapedChartText({ labels: [hostile] });
    expect(out.legend?.formatter?.(hostile)).toBe(escaped);
    expect(out.tooltip?.x?.formatter?.(hostile as unknown as number)).toBe(escaped);
    const y = out.tooltip?.y;
    if (Array.isArray(y) || !y) throw new Error('expected a single y tooltip');
    expect(y.title?.formatter?.(hostile)).toBe(escaped);
  });

  it('keeps existing formatters and escapes their output', () => {
    const out = withEscapedChartText({
      legend: { position: 'bottom', formatter: (name) => `${name} (list)` },
      tooltip: {
        theme: 'dark',
        x: { show: true, formatter: (val) => `client ${String(val)}` },
        y: {
          formatter: (val: number) => `${String(val)} queries`,
          title: { formatter: (s) => `${s}:` },
        },
      },
    });
    expect(out.legend?.position).toBe('bottom');
    expect(out.legend?.formatter?.('Tom & Sam')).toBe('Tom &amp; Sam (list)');
    expect(out.tooltip?.theme).toBe('dark');
    expect(out.tooltip?.x?.show).toBe(true);
    expect(out.tooltip?.x?.formatter?.(hostile as unknown as number)).toBe(`client ${escaped}`);
    const y = out.tooltip?.y;
    if (Array.isArray(y) || !y) throw new Error('expected a single y tooltip');
    expect(y.formatter?.(42)).toBe('42 queries');
    expect(y.title?.formatter?.(hostile)).toBe(`${escaped}:`);
  });

  it('escapes every entry of a per-series y tooltip array', () => {
    const out = withEscapedChartText({
      tooltip: { y: [{}, { title: { formatter: (s) => s.toUpperCase() } }] },
    });
    const y = out.tooltip?.y;
    if (!Array.isArray(y)) throw new Error('expected an array');
    expect(y[0]?.title?.formatter?.('<b>RSS</b>')).toBe('&lt;b&gt;RSS&lt;/b&gt;');
    expect(y[1]?.title?.formatter?.('<b>heap</b>')).toBe('&lt;B&gt;HEAP&lt;/B&gt;');
  });
});
