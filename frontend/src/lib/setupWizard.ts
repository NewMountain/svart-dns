import type { SetupStatus } from '../api/generated';
// First-run setup wizard: presets, state, and the pure update function.
// The page (pages/Setup.tsx) renders this state and runs the API calls;
// every change goes through setupReducer.

export interface ResolverPreset {
  id: string;
  label: string;
  description: string;
  upstreams: string[];
}

export interface ListPreset {
  id: string;
  label: string;
  description: string;
  url: string;
  alias: string;
}

export const resolverPresets: ResolverPreset[] = [
  {
    id: 'privacy-mix',
    label: 'Privacy mix (recommended)',
    description:
      'Quad9, Mullvad and Cloudflare over encrypted DNS-over-HTTPS. Queries are spread across all three, so no single provider sees your whole history.',
    upstreams: [
      'https://dns.quad9.net/dns-query',
      'https://dns.mullvad.net/dns-query',
      'https://cloudflare-dns.com/dns-query',
    ],
  },
  {
    id: 'quad9',
    label: 'Quad9',
    description:
      'Swiss non-profit resolver that also refuses known malware domains. DNS-over-HTTPS.',
    upstreams: ['https://dns.quad9.net/dns-query'],
  },
  {
    id: 'mullvad',
    label: 'Mullvad',
    description: 'No-logging resolver run by the Mullvad VPN company. DNS-over-HTTPS.',
    upstreams: ['https://dns.mullvad.net/dns-query'],
  },
  {
    id: 'cloudflare',
    label: 'Cloudflare',
    description: 'Fastest in most regions. DNS-over-HTTPS.',
    upstreams: ['https://cloudflare-dns.com/dns-query'],
  },
];

export const listPresets: ListPreset[] = [
  {
    id: 'hagezi-pro',
    label: 'Hagezi Pro (recommended)',
    description:
      'Ads, trackers, telemetry and known bad domains, with few false positives. About 230k domains.',
    url: 'https://raw.githubusercontent.com/hagezi/dns-blocklists/main/wildcard/pro-onlydomains.txt',
    alias: 'Hagezi Pro',
  },
  {
    id: 'hagezi-light',
    label: 'Hagezi Light',
    description:
      'The gentlest option: blocks the obvious ads and trackers and almost never breaks a site.',
    url: 'https://raw.githubusercontent.com/hagezi/dns-blocklists/main/wildcard/light-onlydomains.txt',
    alias: 'Hagezi Light',
  },
  {
    id: 'oisd-big',
    label: 'OISD Big',
    description:
      'Broad, carefully curated list focused on not breaking things. About 245k domains.',
    url: 'https://big.oisd.nl/domainswild2',
    alias: 'OISD Big',
  },
  {
    id: 'stevenblack',
    label: 'StevenBlack Unified',
    description: 'The classic hosts file many Pi-hole setups start with. About 80k domains.',
    url: 'https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts',
    alias: 'StevenBlack',
  },
];

// Every device on the network, as the two ranges the policy engine matches.
export const everyoneRanges = [
  { name: 'Everyone (IPv4)', cidr: '0.0.0.0/0' },
  { name: 'Everyone (IPv6)', cidr: '::/0' },
];

export type SetupStep = 'account' | 'resolvers' | 'blocking' | 'finish';

export type SetupInfo = SetupStatus;

export interface SetupState {
  step: SetupStep;
  busy: boolean;
  error: string;
  info: SetupInfo | null;
  token: string;
  username: string;
  password: string;
  confirm: string;
  resolver: string;
  lists: string[];
  protectEveryone: boolean;
  // What the wizard actually changed, shown on the last step.
  applied: string[];
}

export const initialSetupState: SetupState = {
  step: 'account',
  busy: false,
  error: '',
  info: null,
  token: '',
  username: 'admin',
  password: '',
  confirm: '',
  resolver: 'privacy-mix',
  lists: ['hagezi-pro'],
  protectEveryone: true,
  applied: [],
};

export type SetupField = 'token' | 'username' | 'password' | 'confirm';

export type SetupMsg =
  | { type: 'infoLoaded'; info: SetupInfo }
  | { type: 'fieldChanged'; field: SetupField; value: string }
  | { type: 'resolverChosen'; id: string }
  | { type: 'listToggled'; id: string }
  | { type: 'protectEveryoneToggled' }
  | { type: 'submitted' }
  | { type: 'failed'; error: string }
  | { type: 'accountCreated' }
  | { type: 'resolversApplied'; applied: string[] }
  | { type: 'blockingApplied'; applied: string[] }
  | { type: 'skipped' };

const nextStep: Record<SetupStep, SetupStep> = {
  account: 'resolvers',
  resolvers: 'blocking',
  blocking: 'finish',
  finish: 'finish',
};

// accountProblem is the reason the account form can't be submitted yet, or ''.
export function accountProblem(s: SetupState): string {
  const min = s.info?.min_password_length ?? 12;
  if (s.token.trim() === '') return 'Paste the setup token from the server log.';
  if (s.username.trim() === '') return 'Choose a username.';
  if (Array.from(s.password).length < min)
    return `Use a password of at least ${String(min)} characters.`;
  if (s.password !== s.confirm) return 'The two passwords do not match.';
  return '';
}

export function setupReducer(s: SetupState, msg: SetupMsg): SetupState {
  switch (msg.type) {
    case 'infoLoaded':
      return { ...s, info: msg.info };
    case 'fieldChanged':
      return { ...s, [msg.field]: msg.value, error: '' };
    case 'resolverChosen':
      return { ...s, resolver: msg.id };
    case 'listToggled':
      return {
        ...s,
        lists: s.lists.includes(msg.id)
          ? s.lists.filter((id) => id !== msg.id)
          : [...s.lists, msg.id],
      };
    case 'protectEveryoneToggled':
      return { ...s, protectEveryone: !s.protectEveryone };
    case 'submitted':
      return { ...s, busy: true, error: '' };
    case 'failed':
      return { ...s, busy: false, error: msg.error };
    case 'accountCreated':
      // The password is not needed after this point; drop it from state.
      return { ...s, busy: false, step: 'resolvers', password: '', confirm: '', token: '' };
    case 'resolversApplied':
    case 'blockingApplied':
      return { ...s, busy: false, step: nextStep[s.step], applied: [...s.applied, ...msg.applied] };
    case 'skipped':
      return { ...s, busy: false, error: '', step: nextStep[s.step] };
  }
}
