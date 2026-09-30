import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { AppStateProvider } from '../hooks/AppStateProvider';
import * as apiHooks from '../hooks/useApi';
import Admin from './Admin';
import type { APIToken } from '../api/generated';

vi.mock('../hooks/useApi', () => ({
  useApi: vi.fn(),
  usePolling: vi.fn(),
}));

const { getMock, putMock } = vi.hoisted(() => ({
  getMock: vi.fn<(path: string) => Promise<unknown>>(),
  putMock: vi.fn<(path: string, body?: unknown) => Promise<unknown>>(),
}));
vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client');
  return {
    ...actual,
    request: (path: string, _decode: unknown, options: RequestInit) => {
      if (options.method === 'GET') return getMock(path);
      const body: unknown = typeof options.body === 'string' ? JSON.parse(options.body) : undefined;
      return putMock(path, body);
    },
  };
});

const useApiMock = vi.mocked(apiHooks.useApi);
const usePollingMock = vi.mocked(apiHooks.usePolling);

const refreshStub = vi.fn();
let tokensData: APIToken[] = [];

describe('Admin', () => {
  beforeEach(() => {
    getMock.mockReset();
    putMock.mockReset();
    useApiMock.mockReset();
    usePollingMock.mockReset();
    tokensData = [];

    useApiMock.mockImplementation((path: string | null) => {
      if (path === '/api/tokens') {
        return {
          data: tokensData,
          error: null,
          loading: false,
          hasResult: true,
          reload: vi.fn(),
          refresh: refreshStub,
        };
      }
      if (path === '/api/users') {
        return {
          data: [],
          error: null,
          loading: false,
          hasResult: true,
          reload: vi.fn(),
          refresh: refreshStub,
        };
      }
      return {
        data: null,
        error: null,
        loading: false,
        hasResult: false,
        reload: vi.fn(),
        refresh: refreshStub,
      };
    });

    usePollingMock.mockImplementation((path: string | null) => {
      if (path === '/api/peers') {
        return {
          data: {
            replicate_identity: false,
            node_id: 'node-a',
            node_name: 'node-a',
            peers: [],
            sync_interval: '5s',
            has_secret: true,
            tls_configured: true,
          },
          error: null,
          loading: false,
          hasResult: true,
          reload: vi.fn(),
          refresh: refreshStub,
        };
      }
      return {
        data: null,
        error: null,
        loading: false,
        hasResult: false,
        reload: vi.fn(),
        refresh: refreshStub,
      };
    });
  });

  it('hides the shared secret field for readonly users', async () => {
    getMock.mockResolvedValue({ authenticated: true, role: 'readonly', username: 'viewer' });

    render(<AppStateProvider>{<Admin />}</AppStateProvider>);

    await waitFor(() => {
      expect(getMock).toHaveBeenCalledWith('/api/auth/check');
    });

    expect(screen.queryByText('Shared Secret')).not.toBeInTheDocument();
    expect(screen.getByText('Sync Interval')).toBeInTheDocument();
  });

  it('shows the shared secret field for admin users', async () => {
    getMock.mockResolvedValue({ authenticated: true, role: 'admin', username: 'admin' });

    render(<AppStateProvider>{<Admin />}</AppStateProvider>);

    expect(await screen.findByText('Shared Secret')).toBeInTheDocument();
  });

  it('treats the configured shared secret as write-only', async () => {
    getMock.mockResolvedValue({ authenticated: true, role: 'admin', username: 'admin' });
    putMock.mockResolvedValue({ success: true });

    render(<AppStateProvider>{<Admin />}</AppStateProvider>);

    const input = await screen.findByPlaceholderText('Configured — enter a new secret to rotate');
    expect(input).toHaveValue('');
    expect(screen.queryByTitle('Show secret')).not.toBeInTheDocument();

    fireEvent.change(input, { target: { value: '0123456789abcdef0123456789abcdef' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    await waitFor(() => {
      expect(putMock).toHaveBeenCalledWith('/api/settings/sync_secret', {
        value: '0123456789abcdef0123456789abcdef',
      });
    });
  });

  it('does not clear the shared secret when the write-only field is blank', async () => {
    getMock.mockResolvedValue({ authenticated: true, role: 'admin', username: 'admin' });

    render(<AppStateProvider>{<Admin />}</AppStateProvider>);

    const input = await screen.findByPlaceholderText('Configured — enter a new secret to rotate');
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(putMock).not.toHaveBeenCalled();
  });

  it('does not submit a weak shared secret', async () => {
    getMock.mockResolvedValue({ authenticated: true, role: 'admin', username: 'admin' });

    render(<AppStateProvider>{<Admin />}</AppStateProvider>);

    const input = await screen.findByPlaceholderText('Configured — enter a new secret to rotate');
    fireEvent.change(input, { target: { value: 'too-short' } });
    fireEvent.keyDown(input, { key: 'Enter' });

    expect(putMock).not.toHaveBeenCalled();
  });

  it('shows stored token metadata without a reveal affordance', async () => {
    tokensData = [
      {
        id: 1,
        name: 'Grafana',
        token_prefix: 'sv_deadbe',
        role: 'readonly',
        created_at: '2026-03-29T00:00:00Z',
      },
    ];
    getMock.mockResolvedValue({ authenticated: true, role: 'admin', username: 'admin' });

    render(<AppStateProvider>{<Admin />}</AppStateProvider>);

    expect(await screen.findByText('sv_deadbe...')).toBeInTheDocument();
    expect(screen.queryByTitle('Reveal')).not.toBeInTheDocument();
  });
});
