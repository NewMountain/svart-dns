import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { AppStateProvider } from '../hooks/AppStateProvider';
import Login from './Login';

const signedOut = {
  authenticated: false,
  username: '',
  role: '',
  setup_required: false,
};
const signedIn = { authenticated: true, username: 'operator', role: 'admin' };
const success = (data: unknown) => Response.json({ data, error: null });

function renderLogin() {
  window.history.replaceState(null, '', '/login');
  return render(
    <AppStateProvider>
      {
        <MemoryRouter initialEntries={['/login']}>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/" element={<h1>Authenticated dashboard</h1>} />
            <Route path="/setup" element={<h1>First-run setup</h1>} />
          </Routes>
        </MemoryRouter>
      }
    </AppStateProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
  window.history.replaceState(null, '', '/');
});

describe('Login routing and credentials through generated HTTP operations', () => {
  it.each([
    [signedIn, 'Authenticated dashboard'],
    [{ ...signedOut, setup_required: true }, 'First-run setup'],
  ])('routes the complete auth identity %j to %s', async (identity, heading) => {
    const fetch = vi.fn().mockResolvedValue(success(identity));
    vi.stubGlobal('fetch', fetch);
    renderLogin();
    expect(await screen.findByRole('heading', { name: heading })).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Sign In' })).not.toBeInTheDocument();
    expect(fetch).toHaveBeenCalledWith('/api/auth/check', {
      method: 'GET',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      cache: 'no-store',
    });
  });

  it('waits for the auth check and preserves entered credentials across rejection and retry', async () => {
    let finishCheck: ((response: Response) => void) | undefined;
    const fetch = vi
      .fn()
      .mockImplementationOnce(
        () =>
          new Promise<Response>((resolve) => {
            finishCheck = resolve;
          }),
      )
      .mockResolvedValueOnce(
        Response.json(
          { error: 'invalid credentials', error_code: 'unauthorized' },
          { status: 401 },
        ),
      )
      .mockResolvedValueOnce(success(signedIn));
    vi.stubGlobal('fetch', fetch);
    renderLogin();
    expect(screen.queryByRole('button', { name: 'Sign In' })).not.toBeInTheDocument();
    finishCheck?.(success(signedOut));
    fireEvent.change(await screen.findByLabelText('Username'), {
      target: { value: 'operator' },
    });
    fireEvent.change(screen.getByLabelText('Password'), {
      target: { value: 'test-password' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Sign In' }));
    expect(await screen.findByText('Invalid credentials')).toBeInTheDocument();
    expect(screen.getByLabelText('Username')).toHaveValue('operator');
    expect(screen.getByLabelText('Password')).toHaveValue('test-password');
    expect(fetch).toHaveBeenNthCalledWith(2, '/api/auth/login', {
      method: 'POST',
      body: JSON.stringify({ username: 'operator', password: 'test-password' }),
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      cache: 'no-store',
    });
    fireEvent.click(screen.getByRole('button', { name: 'Sign In' }));
    expect(
      await screen.findByRole('heading', { name: 'Authenticated dashboard' }),
    ).toBeInTheDocument();
    expect(screen.queryByText('Invalid credentials')).not.toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(3);
  });

  it.each(['network failure', 'malformed identity'])(
    'shows the login form after %s without entering setup',
    async (failure) => {
      const fetch = vi.fn();
      if (failure === 'network failure') fetch.mockRejectedValue(new Error('connection lost'));
      else fetch.mockResolvedValue(success({ authenticated: 'yes' }));
      vi.stubGlobal('fetch', fetch);
      renderLogin();
      await waitFor(() => {
        expect(screen.getByRole('button', { name: 'Sign In' })).toBeEnabled();
      });
      expect(screen.queryByRole('heading', { name: 'First-run setup' })).not.toBeInTheDocument();
      expect(
        screen.queryByRole('heading', { name: 'Authenticated dashboard' }),
      ).not.toBeInTheDocument();
    },
  );
});
