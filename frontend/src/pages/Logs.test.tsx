import { cleanup, render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { get } from '../api/client';
import { getApiQueryLogs } from '../api/operations';
import { AppStateProvider } from '../hooks/AppStateProvider';
import Logs from './Logs';

vi.mock('../api/client', () => ({ get: vi.fn() }));
vi.mock('../api/operations', () => ({ getApiQueryLogs: vi.fn() }));
vi.mock('../hooks/useNodeName', () => ({ useNodeName: () => ({ nodeName: 'Test node' }) }));

afterEach(cleanup);
beforeEach(() => {
  vi.mocked(get).mockResolvedValue([]);
  vi.mocked(getApiQueryLogs).mockResolvedValue({ logs: [], total: 0, limit: 100, offset: 0 });
});

it('distinguishes an empty query history from filters excluding all results', async () => {
  render(
    <AppStateProvider>
      {
        <MemoryRouter>
          <Logs />
        </MemoryRouter>
      }
    </AppStateProvider>,
  );
  expect(await screen.findByText('No DNS queries logged yet')).toBeInTheDocument();
  expect(screen.getByText('0-0')).toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Previous' })).toBeDisabled();
  expect(screen.getByRole('button', { name: 'Next' })).toBeDisabled();
});

it('offers a recovery hint when filters match no queries', async () => {
  render(
    <AppStateProvider>
      {
        <MemoryRouter initialEntries={['/logs?domain=missing.example']}>
          <Logs />
        </MemoryRouter>
      }
    </AppStateProvider>,
  );
  expect(await screen.findByText('No logs matching filters')).toBeInTheDocument();
  expect(screen.getByText('Clear or broaden the filters to see more queries.')).toBeInTheDocument();
});

it('reports a failed load as unavailable rather than an empty history', async () => {
  vi.mocked(getApiQueryLogs).mockRejectedValue(new Error('Network unavailable'));
  render(
    <AppStateProvider>
      {
        <MemoryRouter>
          <Logs />
        </MemoryRouter>
      }
    </AppStateProvider>,
  );
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not load query logs.');
  expect(screen.queryByText('No DNS queries logged yet')).not.toBeInTheDocument();
  expect(screen.getByText('Query count unavailable')).toBeInTheDocument();
  expect(screen.queryByText('0-0')).not.toBeInTheDocument();
  expect(screen.getByRole('button', { name: 'Retry' })).toBeEnabled();
});
