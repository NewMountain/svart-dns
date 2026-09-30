import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, expect, it, vi } from 'vitest';
import { postApiBlocklistsIdToggle } from '../api/operations';
import { AppStateProvider } from '../hooks/AppStateProvider';
import { useApi } from '../hooks/useApi';
import Filters from './Filters';
vi.mock('../api/operations', () => ({ postApiBlocklistsIdToggle: vi.fn() }));
vi.mock('../hooks/useApi', () => ({ useApi: vi.fn() }));
vi.mock('../hooks/useNodeName', () => ({ useNodeName: () => ({ nodeName: 'Fixture' }) }));
const refresh = vi.fn();
beforeEach(() => {
  vi.clearAllMocks();
  vi.mocked(useApi).mockReturnValue({
    data: [
      {
        id: 9,
        alias: 'Example list',
        url: 'https://example.com/list.txt',
        enabled: true,
        domain_count: 42,
        compatibility: {
          assessed: false,
          assessed_at: '',
          lines: 0,
          applied: 0,
          unsupported: 0,
          invalid: 0,
          diagnostic_count: 0,
        },
        last_updated: '',
        refresh_interval: 60,
      },
    ],
    error: null,
    loading: false,
    hasResult: true,
    refresh,
    reload: vi.fn(),
  });
  vi.mocked(postApiBlocklistsIdToggle).mockResolvedValue({ success: true });
});
it('toggles a list through the supported operation and refreshes after success', async () => {
  render(
    <AppStateProvider>
      {
        <MemoryRouter>
          <Filters />
        </MemoryRouter>
      }
    </AppStateProvider>,
  );
  fireEvent.click(screen.getByRole('checkbox'));
  await waitFor(() => {
    expect(postApiBlocklistsIdToggle).toHaveBeenCalledWith(9);
  });
  await waitFor(() => {
    expect(refresh).toHaveBeenCalledTimes(1);
  });
});
