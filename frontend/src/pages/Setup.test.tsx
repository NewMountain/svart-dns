import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import * as apiClient from '../api/client';
import { AppStateProvider } from '../hooks/AppStateProvider';
import Setup from './Setup';

const { getMock, postMock } = vi.hoisted(() => ({
  getMock: vi.fn<(path: string) => Promise<unknown>>(),
  postMock: vi.fn<(path: string, body?: unknown) => Promise<unknown>>(),
}));
vi.mock('../api/client', async () => {
  const actual = await vi.importActual<typeof import('../api/client')>('../api/client');
  return {
    ...actual,
    request: (path: string, _decode: unknown, options: RequestInit) => {
      if (options.method === 'GET') return getMock(path);
      const body: unknown = typeof options.body === 'string' ? JSON.parse(options.body) : undefined;
      return postMock(path, body);
    },
  };
});

function renderSetup() {
  return render(
    <AppStateProvider>
      {
        <MemoryRouter initialEntries={['/setup']}>
          <Setup />
        </MemoryRouter>
      }
    </AppStateProvider>,
  );
}

describe('Setup', () => {
  beforeEach(() => {
    getMock.mockReset();
    postMock.mockReset();
    getMock.mockImplementation((path: string) => {
      if (path === '/api/setup')
        return Promise.resolve({
          required: true,
          dns_port: '5353',
          addresses: ['192.168.1.2'],
          min_password_length: 12,
        });
      if (path === '/api/upstreams') return Promise.resolve([]);
      return Promise.resolve(null);
    });
  });

  it('creates the account with the pasted token, then offers resolvers', async () => {
    postMock.mockResolvedValue({ authenticated: true });
    renderSetup();
    fireEvent.change(screen.getByLabelText('Setup token'), {
      target: { value: 'JBSW-Y3DP-EHPK-3PXP' },
    });
    fireEvent.change(screen.getByLabelText('Username'), { target: { value: 'chris' } });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'correct horse battery staple' },
    });
    fireEvent.change(screen.getByLabelText('Password again'), {
      target: { value: 'correct horse battery staple' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));
    await screen.findByRole('heading', { name: 'Choose where lookups go' });
    expect(postMock).toHaveBeenCalledWith('/api/setup', {
      token: 'JBSW-Y3DP-EHPK-3PXP',
      username: 'chris',
      password: 'correct horse battery staple',
    });
    expect(screen.getByRole('radio', { name: /Privacy mix \(recommended\)/ })).toBeChecked();
  });

  it('shows the server error for a wrong token and stays on the account step', async () => {
    postMock.mockRejectedValue(
      new apiClient.ApiError(401, 'wrong setup token: copy it from the server log'),
    );
    renderSetup();
    fireEvent.change(screen.getByLabelText('Setup token'), { target: { value: 'NOPE' } });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'correct horse battery staple' },
    });
    fireEvent.change(screen.getByLabelText('Password again'), {
      target: { value: 'correct horse battery staple' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'wrong setup token: copy it from the server log',
    );
    expect(screen.getByRole('heading', { name: 'Create your account' })).toBeInTheDocument();
  });

  it('refuses to submit a short password without calling the API', async () => {
    renderSetup();
    await waitFor(() => {
      expect(getMock).toHaveBeenCalledWith('/api/setup');
    });
    fireEvent.change(screen.getByLabelText('Setup token'), { target: { value: 'JBSW' } });
    fireEvent.change(screen.getByLabelText('Password'), { target: { value: 'hunter2' } });
    fireEvent.change(screen.getByLabelText('Password again'), { target: { value: 'hunter2' } });
    fireEvent.click(screen.getByRole('button', { name: 'Create account' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Use a password of at least 12 characters.',
    );
    expect(postMock).not.toHaveBeenCalled();
  });
});
