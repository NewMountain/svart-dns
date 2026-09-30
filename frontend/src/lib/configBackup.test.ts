import { describe, expect, it } from 'vitest';
import type { ConfigExport } from '../api/generated';
import { backupReducer, initialBackupState, sameConfiguration } from './configBackup';

const config: ConfigExport = {
  version: '1',
  exported_at: '2026-09-28T12:00:00Z',
  upstreams: [{ upstream: '127.0.0.1:50053', enabled: false }],
  blocklists: [
    {
      alias: 'Family custom',
      url: '',
      enabled: false,
      refresh_interval: 0,
      domains: ['ads.example', 'tracking.example'],
    },
  ],
  allowlists: [],
  rewrites: [{ domain: 'printer.home', ip_addresses: '192.0.2.20', enabled: true }],
  policies: [],
  groups: [],
  ranges: [{ name: 'Home', cidr: '192.0.2.0/24', blocklists: ['Family custom'], allowlists: [] }],
  settings: { timezone: 'UTC', cache_ttl: '120' },
  bootstrap_servers: ['192.0.2.53:53'],
  clients: [],
};

describe('configuration backup verification', () => {
  it('ignores export time and unordered records while preserving every field', () => {
    expect(
      sameConfiguration(config, {
        ...config,
        exported_at: '2026-09-29T13:00:00Z',
        settings: { cache_ttl: '120', timezone: 'UTC' },
        blocklists: [
          {
            alias: 'Family custom',
            url: '',
            enabled: false,
            refresh_interval: 0,
            domains: ['tracking.example', 'ads.example'],
          },
        ],
      }),
    ).toBe(true);
  });
  it.each([
    { ...config, upstreams: [{ upstream: '127.0.0.1:50053', enabled: true }] },
    {
      ...config,
      blocklists: [
        {
          alias: 'Family custom',
          url: '',
          enabled: false,
          refresh_interval: 0,
          domains: ['ads.example'],
        },
      ],
    },
    { ...config, ranges: [] },
    { ...config, settings: { timezone: 'UTC', cache_ttl: '3600' } },
  ])('rejects a partial or different readback', (actual) => {
    expect(sameConfiguration(config, actual)).toBe(false);
  });
  it('retains the selected backup through import and readback failures', () => {
    const selected = backupReducer(initialBackupState, {
      type: 'selected',
      filename: 'home.json',
      config,
    });
    const importing = backupReducer(selected, { type: 'started', operation: 'import' });
    expect(importing).toEqual({
      selection: { filename: 'home.json', config },
      progress: { status: 'busy', operation: 'import' },
    });
    expect(
      backupReducer(importing, {
        type: 'failed',
        message: 'Readback unavailable; retry export to verify.',
      }),
    ).toEqual({
      selection: { filename: 'home.json', config },
      progress: { status: 'error', message: 'Readback unavailable; retry export to verify.' },
    });
  });
  it('reports verified import only after the effect sends a verified message', () => {
    expect(
      backupReducer(initialBackupState, { type: 'verified', exportedAt: config.exported_at }),
    ).toEqual({
      selection: null,
      progress: { status: 'verified', exportedAt: config.exported_at },
    });
  });
});

it('preserves bootstrap fallback order when verifying persisted configuration', () => {
  const expected = { ...config, bootstrap_servers: ['192.0.2.53:53', '192.0.2.54:53'] };
  expect(
    sameConfiguration(expected, {
      ...expected,
      bootstrap_servers: ['192.0.2.54:53', '192.0.2.53:53'],
    }),
  ).toBe(false);
  expect(sameConfiguration(expected, { ...expected, exported_at: '2026-09-29T12:00:00Z' })).toBe(
    true,
  );
});
