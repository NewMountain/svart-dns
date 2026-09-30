import { describe, expect, it } from 'vitest';
import { accountProblem, initialSetupState, setupReducer, SetupState } from './setupWizard';

const info = {
  required: true,
  dns_port: '53',
  addresses: ['192.168.1.2'],
  min_password_length: 12,
};

function filled(overrides: Partial<SetupState> = {}): SetupState {
  return {
    ...initialSetupState,
    info,
    token: 'JBSW-Y3DP-EHPK-3PXP-JBSW-Y3DP-EHPK-3PXP',
    username: 'chris',
    password: 'correct horse battery staple',
    confirm: 'correct horse battery staple',
    ...overrides,
  };
}

describe('setupReducer', () => {
  it('walks account → resolvers → blocking → finish, dropping secrets after the account step', () => {
    let s = setupReducer(filled(), { type: 'submitted' });
    expect(s.busy).toBe(true);
    s = setupReducer(s, { type: 'accountCreated' });
    expect(s).toEqual({
      ...filled(),
      step: 'resolvers',
      busy: false,
      token: '',
      password: '',
      confirm: '',
    });
    s = setupReducer(s, {
      type: 'resolversApplied',
      applied: ['Resolvers: https://dns.quad9.net/dns-query'],
    });
    expect(s.step).toBe('blocking');
    s = setupReducer(s, {
      type: 'blockingApplied',
      applied: ['Blocklist: Hagezi Pro (refreshed daily)'],
    });
    expect(s.step).toBe('finish');
    expect(s.applied).toEqual([
      'Resolvers: https://dns.quad9.net/dns-query',
      'Blocklist: Hagezi Pro (refreshed daily)',
    ]);
  });

  it('toggles lists on and off and keeps the recommended default', () => {
    expect(initialSetupState.lists).toEqual(['hagezi-pro']);
    let s = setupReducer(initialSetupState, { type: 'listToggled', id: 'oisd-big' });
    expect(s.lists).toEqual(['hagezi-pro', 'oisd-big']);
    s = setupReducer(s, { type: 'listToggled', id: 'hagezi-pro' });
    expect(s.lists).toEqual(['oisd-big']);
  });

  it('skipping blocking goes to the last step without applying anything', () => {
    const s = setupReducer({ ...filled(), step: 'blocking', error: 'old' }, { type: 'skipped' });
    expect(s.step).toBe('finish');
    expect(s.error).toBe('');
    expect(s.applied).toEqual([]);
  });

  it('a failure stops the spinner and shows the server message; editing a field clears it', () => {
    let s = setupReducer(setupReducer(filled(), { type: 'submitted' }), {
      type: 'failed',
      error: 'wrong setup token',
    });
    expect(s.busy).toBe(false);
    expect(s.error).toBe('wrong setup token');
    s = setupReducer(s, { type: 'fieldChanged', field: 'token', value: 'NEW' });
    expect(s.error).toBe('');
    expect(s.token).toBe('NEW');
  });
});

describe('accountProblem', () => {
  it('accepts a complete account form', () => {
    expect(accountProblem(filled())).toBe('');
  });
  it.each([
    [{ token: '  ' }, 'Paste the setup token from the server log.'],
    [{ username: '' }, 'Choose a username.'],
    [{ password: 'short', confirm: 'short' }, 'Use a password of at least 12 characters.'],
    [{ confirm: 'correct horse battery stapler' }, 'The two passwords do not match.'],
  ])('rejects %j', (overrides, message) => {
    expect(accountProblem(filled(overrides))).toBe(message);
  });
  it('counts characters, not UTF-16 units, for the minimum length', () => {
    expect(accountProblem(filled({ password: '🔑'.repeat(12), confirm: '🔑'.repeat(12) }))).toBe(
      '',
    );
  });
});
