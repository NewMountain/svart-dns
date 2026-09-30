import { FormEvent, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { ApiError } from '../api/client';
import {
  getApiSetup,
  getApiUpstreams,
  postApiBlocklists,
  postApiRanges,
  postApiRangesIdBlocklistsListId,
  postApiSetup,
  postApiUpstreams,
} from '../api/operations';
import { useAction } from '../hooks/useAction';
import { useUiReducer } from '../hooks/useUiField';
import {
  accountProblem,
  everyoneRanges,
  initialSetupState,
  listPresets,
  resolverPresets,
  SetupField,
  setupReducer,
} from '../lib/setupWizard';

const stepIndex = { account: 0, resolvers: 1, blocking: 2, finish: 3 } as const;
export function useSetupController() {
  const action = useAction();
  const navigate = useNavigate();
  const [s, dispatch] = useUiReducer('Setup.model', setupReducer, initialSetupState);
  useEffect(() => {
    getApiSetup()
      .then((info) => {
        // Setup already done and nobody signed in mid-wizard: go sign in.
        if (!info.required && s.step === 'account')
          action(() => navigate('/login', { replace: true }))();
        else dispatch({ type: 'infoLoaded', info });
      })
      .catch((err: unknown) => {
        dispatch({ type: 'failed', error: errorText(err) });
      });
    // Only on first render: later steps run signed in, after setup is complete.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- runs once by design
  }, []);
  const field = (name: SetupField) => ({
    value: s[name],
    onChange: (e: React.ChangeEvent<HTMLInputElement>) => {
      dispatch({ type: 'fieldChanged', field: name, value: e.target.value });
    },
  });
  async function createAccount(e: FormEvent) {
    e.preventDefault();
    const problem = accountProblem(s);
    if (problem) {
      dispatch({ type: 'failed', error: problem });
      return;
    }
    dispatch({ type: 'submitted' });
    try {
      await postApiSetup({ token: s.token, username: s.username.trim(), password: s.password });
      dispatch({ type: 'accountCreated' });
    } catch (err) {
      dispatch({ type: 'failed', error: errorText(err) });
    }
  }
  async function applyResolvers() {
    const preset = resolverPresets.find((p) => p.id === s.resolver);
    if (!preset) return;
    dispatch({ type: 'submitted' });
    try {
      const existing = (await getApiUpstreams()) ?? [];
      const have = new Set(existing.map((u) => u.upstream));
      for (const upstream of preset.upstreams) {
        if (!have.has(upstream)) await postApiUpstreams({ upstream, enabled: true });
      }
      dispatch({
        type: 'resolversApplied',
        applied: [`Resolvers: ${preset.upstreams.join(', ')}`],
      });
    } catch (err) {
      dispatch({ type: 'failed', error: errorText(err) });
    }
  }
  async function applyBlocking() {
    dispatch({ type: 'submitted' });
    try {
      const applied: string[] = [];
      const listIDs: number[] = [];
      for (const preset of listPresets.filter((p) => s.lists.includes(p.id))) {
        const created = await postApiBlocklists({
          url: preset.url,
          alias: preset.alias,
          enabled: true,
          refresh_interval: 86400,
        });
        listIDs.push(created.id);
        applied.push(`Blocklist: ${preset.alias} (refreshed daily)`);
      }
      if (s.protectEveryone && listIDs.length > 0) {
        for (const r of everyoneRanges) {
          const range = await postApiRanges({ name: r.name, cidr: r.cidr });
          for (const id of listIDs) await postApiRangesIdBlocklistsListId(range.id, id);
        }
        applied.push(
          'Every device on the network uses these lists (networks 0.0.0.0/0 and ::/0 under Assignments)',
        );
      }
      dispatch({ type: 'blockingApplied', applied });
    } catch (err) {
      dispatch({ type: 'failed', error: errorText(err) });
    }
  }
  const step = stepIndex[s.step];
  const port = s.info?.dns_port ?? '53';
  return {
    action,
    navigate,
    s,
    dispatch,
    field,
    createAccount,
    applyResolvers,
    applyBlocking,
    step,
    port,
  };
}

function errorText(err: unknown): string {
  if (err instanceof ApiError) return err.message;
  return 'Svart did not answer. Check that the server is still running and try again.';
}
