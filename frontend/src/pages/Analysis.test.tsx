import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { ApiError } from '../api/client';
import { getApiAnalysisDomains } from '../api/operations';
import { AppStateProvider } from '../hooks/AppStateProvider';
import Analysis from './Analysis';

vi.mock('../api/operations', async () => ({
  ...(await vi.importActual<typeof import('../api/operations')>('../api/operations')),
  getApiAnalysisDomains: vi.fn(),
}));
vi.mock('../components/TopBar', () => ({ default: () => null }));
vi.mock('../components/ClientSearch', () => ({ default: () => null }));

const getMock = vi.mocked(getApiAnalysisDomains);

describe('Analysis ranking prefill', () => {
  beforeEach(() => vi.clearAllMocks());

  it('shows the missing-source error and preserves input, then allows a successful retry', async () => {
    const error =
      'top-sites unavailable: TOP_SITES_PATH is unset; configure a local rank,domain CSV file';
    getMock.mockRejectedValueOnce(new ApiError(503, error));
    getMock.mockResolvedValueOnce({
      source: 'top-sites',
      count: 2,
      domains: ['google.com', 'wikipedia.org'],
    });
    render(
      <AppStateProvider>
        {
          <MemoryRouter>
            <Analysis />
          </MemoryRouter>
        }
      </AppStateProvider>,
    );
    const input = screen.getByRole('textbox', { name: 'Domains' });
    fireEvent.change(input, { target: { value: 'operator.example\nkeep.example' } });
    fireEvent.click(screen.getByRole('button', { name: 'Top 1k sites' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(error);
    expect(input).toHaveValue('operator.example\nkeep.example');
    expect(getMock).toHaveBeenCalledWith({ source: 'top-sites', limit: '1000' });
    fireEvent.click(screen.getByRole('button', { name: 'Top 1k sites' }));
    await waitFor(() => expect(input).toHaveValue('google.com\nwikipedia.org'));
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
