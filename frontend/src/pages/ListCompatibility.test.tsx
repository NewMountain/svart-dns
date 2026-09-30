import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, expect, it, vi } from 'vitest';
import { getApiBlocklistsIdCompatibility } from '../api/operations';
import ListCompatibility from './ListCompatibility';
vi.mock('../api/operations', () => ({ getApiBlocklistsIdCompatibility: vi.fn() }));
const summary = {
  assessed: true,
  assessed_at: '2026-09-29T19:00:00Z',
  lines: 55,
  applied: 3,
  unsupported: 51,
  invalid: 1,
  diagnostic_count: 52,
};
beforeEach(() => vi.resetAllMocks());
it('keeps an unassessed generation distinct from zero unsupported rules', () => {
  render(<ListCompatibility id={3} summary={{ ...summary, assessed: false }} />);
  expect(screen.getByText('Compatibility not assessed; refresh this list.')).toBeInTheDocument();
});
it('shows complete rule text and paginates every diagnostic, preserving results on failure', async () => {
  const rule = '||tracking.example^$client=~192.0.2.8';
  vi.mocked(getApiBlocklistsIdCompatibility).mockResolvedValueOnce({
    summary,
    diagnostics: [{ line: 4, rule, reason: 'Unsupported modifier: client' }],
    total: 52,
    offset: 0,
    limit: 50,
  });
  render(<ListCompatibility id={3} summary={summary} />);
  fireEvent.click(screen.getByText('3 applied · 51 unsupported · 1 invalid'));
  expect(await screen.findByText(rule)).toBeInTheDocument();
  vi.mocked(getApiBlocklistsIdCompatibility).mockRejectedValueOnce(Error('Read unavailable'));
  fireEvent.click(screen.getByRole('button', { name: 'Next diagnostics' }));
  expect(await screen.findByRole('alert')).toHaveTextContent('Read unavailable');
  expect(screen.getByText(rule)).toBeInTheDocument();
  vi.mocked(getApiBlocklistsIdCompatibility).mockResolvedValueOnce({
    summary,
    diagnostics: [
      { line: 55, rule: 'bad.example$unknown', reason: 'Unsupported modifier: unknown' },
    ],
    total: 52,
    offset: 50,
    limit: 50,
  });
  fireEvent.click(screen.getByRole('button', { name: 'Retry diagnostics' }));
  await waitFor(() => {
    expect(getApiBlocklistsIdCompatibility).toHaveBeenLastCalledWith(3, {
      limit: '50',
      offset: '50',
    });
  });
  expect(await screen.findByText('bad.example$unknown')).toBeInTheDocument();
});

it('shows an unassessed current source when its URL changed before the report was opened', async () => {
  vi.mocked(getApiBlocklistsIdCompatibility).mockResolvedValueOnce({
    summary: { ...summary, assessed: false },
    diagnostics: [],
    total: 0,
    offset: 0,
    limit: 50,
  });
  render(<ListCompatibility id={3} summary={summary} />);
  fireEvent.click(screen.getByText('3 applied · 51 unsupported · 1 invalid'));
  expect(
    await screen.findByText('Compatibility not assessed; refresh this list.'),
  ).toBeInTheDocument();
  expect(screen.queryByText('No skipped rules in this generation.')).not.toBeInTheDocument();
});
