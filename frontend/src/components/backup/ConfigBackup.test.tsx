import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ConfigExport } from '../../api/generated';
import { AppStateProvider } from '../../hooks/AppStateProvider';
import ConfigBackup from './ConfigBackup';

const backup: ConfigExport = {
  version: '1',
  exported_at: '2026-09-28T12:00:00Z',
  upstreams: [],
  blocklists: [],
  allowlists: [],
  rewrites: [],
  policies: [],
  groups: [],
  ranges: [],
  settings: { timezone: 'UTC' },
  bootstrap_servers: [],
  clients: [],
};
const fetchMock = vi.fn<typeof fetch>();
const response = (data: unknown) =>
  new Response(JSON.stringify({ data, error: null }), {
    status: 200,
    headers: { 'Content-Type': 'application/json' },
  });
function choose(content: string, filename = 'home.json') {
  const file = Object.assign(new File([content], filename, { type: 'application/json' }), {
    text: () => Promise.resolve(content),
  });
  fireEvent.change(screen.getByLabelText('Configuration JSON file'), { target: { files: [file] } });
}

describe('configuration backup controls', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', fetchMock);
    fetchMock.mockReset();
  });
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it('requires a valid file, imports through the secured client, and verifies exact readback', async () => {
    fetchMock
      .mockResolvedValueOnce(response({ success: true }))
      .mockResolvedValueOnce(response(backup));
    render(<AppStateProvider>{<ConfigBackup />}</AppStateProvider>);
    expect(screen.getByRole('button', { name: 'Import Configuration' })).toBeDisabled();
    choose(JSON.stringify(backup));
    await screen.findByText('Selected: home.json');
    fireEvent.click(screen.getByRole('button', { name: 'Import Configuration' }));
    await screen.findByText(/Import verified: the persisted configuration matches your backup/);
    expect(fetchMock).toHaveBeenCalledTimes(2);
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/config/import');
    expect(fetchMock.mock.calls[0]?.[1]).toEqual({
      method: 'POST',
      body: JSON.stringify(backup),
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      cache: 'no-store',
    });
    expect(fetchMock.mock.calls[1]?.[0]).toBe('/api/config/export');
  });

  it.each(['{invalid json', JSON.stringify({ version: '1', settings: [] })])(
    'reports invalid files without sending any request',
    async (content) => {
      render(<AppStateProvider>{<ConfigBackup />}</AppStateProvider>);
      choose(content);
      expect(await screen.findByRole('alert')).toHaveTextContent('Cannot read this backup');
      expect(screen.getByRole('button', { name: 'Import Configuration' })).toBeDisabled();
      expect(fetchMock).not.toHaveBeenCalled();
    },
  );

  it('does not verify a server response that declined the import', async () => {
    fetchMock.mockResolvedValueOnce(response({ success: false }));
    render(<AppStateProvider>{<ConfigBackup />}</AppStateProvider>);
    choose(JSON.stringify(backup));
    await screen.findByText('Selected: home.json');
    fireEvent.click(screen.getByRole('button', { name: 'Import Configuration' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'The server did not accept the import',
    );
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it('retains the selected file on a server rejection and permits a retry', async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          data: null,
          error: 'Invalid upstream address',
          error_code: 'invalid_request',
        }),
        { status: 400 },
      ),
    );
    render(<AppStateProvider>{<ConfigBackup />}</AppStateProvider>);
    choose(JSON.stringify(backup));
    await screen.findByText('Selected: home.json');
    fireEvent.click(screen.getByRole('button', { name: 'Import Configuration' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(
      'Import not verified: Invalid upstream address',
    );
    expect(screen.getByText('Selected: home.json')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Import Configuration' })).toBeEnabled();
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it.each([
    [
      'different persisted settings',
      () => Promise.resolve(response({ ...backup, settings: { timezone: 'America/Los_Angeles' } })),
      'persisted configuration differs',
    ],
    [
      'unavailable readback',
      () => Promise.reject(new Error('Network unavailable')),
      'Network unavailable',
    ],
  ])('does not claim success after %s', async (_label, readback, error) => {
    fetchMock.mockResolvedValueOnce(response({ success: true })).mockImplementationOnce(readback);
    render(<AppStateProvider>{<ConfigBackup />}</AppStateProvider>);
    choose(JSON.stringify(backup));
    await screen.findByText('Selected: home.json');
    fireEvent.click(screen.getByRole('button', { name: 'Import Configuration' }));
    expect(await screen.findByRole('alert')).toHaveTextContent(error);
    expect(screen.queryByText(/Import verified:/)).not.toBeInTheDocument();
  });

  it('downloads complete JSON from the export endpoint', async () => {
    fetchMock.mockResolvedValueOnce(response(backup));
    const createURL = vi.fn<(...args: unknown[]) => string>(() => 'blob:configuration');
    const revokeURL = vi.fn();
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createURL });
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revokeURL });
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {});
    render(<AppStateProvider>{<ConfigBackup />}</AppStateProvider>);
    fireEvent.click(screen.getByRole('button', { name: 'Download Configuration' }));
    await waitFor(() => {
      expect(click).toHaveBeenCalledOnce();
    });
    expect(createURL.mock.calls[0]?.[0]).toBeInstanceOf(Blob);
    expect(revokeURL).toHaveBeenCalledWith('blob:configuration');
    expect(screen.getByRole('status')).toHaveTextContent('2026-09-28T12:00:00Z');
    expect(fetchMock.mock.calls[0]?.[0]).toBe('/api/config/export');
  });
});
