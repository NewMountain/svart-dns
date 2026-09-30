import { fireEvent, render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import LegacyTiersRedirect from '../components/LegacyTiersRedirect';
import { AppStateProvider } from '../hooks/AppStateProvider';
import Tiers from './Tiers';

vi.mock('../hooks/useApi', () => ({
  useApi: (path: string | null) => ({
    data: path?.split('/').length === 4 ? null : [],
    error: null,
    loading: false,
    refresh: vi.fn(),
  }),
}));

function Location() {
  return (
    <output aria-label="Current location">
      {useLocation().pathname}
      {useLocation().search}
    </output>
  );
}
function renderAt(path: string) {
  return render(
    <AppStateProvider>
      {
        <MemoryRouter initialEntries={[path]}>
          <Location />
          <Routes>
            <Route path="/assignments" element={<Tiers />} />
            <Route path="/tiers" element={<LegacyTiersRedirect />} />
          </Routes>
        </MemoryRouter>
      }
    </AppStateProvider>,
  );
}

describe('Assignments navigation characterization', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it.each([
    ['networks', 'Networks', 'network'],
    ['ranges', 'Networks', 'network'],
    ['groups', 'Groups', 'group'],
    ['clients', 'Clients', null],
    ['bundles', 'Bundles', 'bundle'],
    ['policies', 'Bundles', 'bundle'],
  ])('preserves the %s tab, names, and create controls', (tab, label, create) => {
    renderAt(`/assignments?tab=${tab}`);
    expect(screen.getByRole('button', { name: label })).toHaveClass('active');
    expect(
      screen
        .getAllByRole('button')
        .filter((button) => button.classList.contains('tab-item'))
        .map((button) => button.textContent),
    ).toEqual(['Networks', 'Groups', 'Clients', 'Bundles']);
    if (create) {
      fireEvent.click(screen.getByRole('button', { name: `Create ${create}` }));
      expect(screen.getByPlaceholderText('Name')).toHaveValue('');
      expect(screen.getByRole('button', { name: 'Create' })).toBeEnabled();
      if (create === 'network')
        expect(screen.getByPlaceholderText('CIDR (e.g. 10.0.0.0/24)')).toHaveValue('');
    } else {
      expect(screen.queryByRole('button', { name: /^Create / })).not.toBeInTheDocument();
    }
  });

  it('preserves legacy query parameters and clears selection/search on tab changes', () => {
    renderAt('/tiers?tab=policies&search=Living&source=bookmark');
    expect(screen.getByRole('button', { name: 'Bundles' })).toHaveClass('active');
    expect(screen.getByPlaceholderText('Search bundles...')).toHaveValue('Living');
    expect(screen.getByLabelText('Current location')).toHaveTextContent(
      '/assignments?tab=policies&search=Living&source=bookmark',
    );
    fireEvent.click(screen.getByRole('button', { name: 'Groups' }));
    expect(screen.getByPlaceholderText('Search groups...')).toHaveValue('');
    expect(screen.getByLabelText('Current location')).toHaveTextContent(
      '/assignments?tab=groups&source=bookmark',
    );
    fireEvent.change(screen.getByPlaceholderText('Search groups...'), {
      target: { value: 'Living room' },
    });
    expect(screen.getByLabelText('Current location')).toHaveTextContent(
      '/assignments?tab=groups&source=bookmark&search=Living+room',
    );
  });

  it('falls back to Networks for an unknown tab without renaming the URL', () => {
    renderAt('/assignments?tab=unknown');
    expect(screen.getByRole('button', { name: 'Networks' })).toHaveClass('active');
    expect(screen.getByLabelText('Current location')).toHaveTextContent('/assignments?tab=unknown');
  });
});
