import { listPresets, resolverPresets } from '../lib/setupWizard';
import '../styles/pages/login.css';
import '../styles/pages/setup.css';
import { useSetupController } from './Setup.controller';

const stepTitles = [
  'Create your account',
  'Choose where lookups go',
  'Choose what to block',
  'Point your network at Svart',
];
const stepLabels = ['Account', 'Resolvers', 'Blocking', 'Connect'];

export default function Setup() {
  const model = useSetupController();
  return <SetupView model={model} />;
}

function SetupView({ model }: { model: NonNullable<ReturnType<typeof useSetupController>> }) {
  const {
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
  } = model;
  return (
    <div className="login-page setup-page">
      <div className="login-card setup-card">
        <div className="login-logo setup-logo">
          <div className="login-logo-icon" />
          <span className="login-logo-text">
            SVART <span className="login-logo-sub">DNS</span>
          </span>
        </div>
        <ol className="setup-steps" aria-label="Setup progress">
          {stepLabels.map((label, i) => (
            <li
              key={label}
              className={i === step ? 'current' : i < step ? 'done' : ''}
              aria-current={i === step ? 'step' : undefined}
            >
              {label}
            </li>
          ))}
        </ol>
        <h1 className="setup-title">{stepTitles[step]}</h1>

        {s.step === 'account' && (
          <form onSubmit={action(createAccount)} noValidate>
            <p className="setup-lead">
              Svart printed a one-time <strong>setup token</strong> when it started. Find the line
              that begins with <code>first-run setup</code> in its log (for Docker:{' '}
              <code>docker logs svart</code>) and paste the token here.
            </p>
            <div className="login-field">
              <label className="login-label" htmlFor="setup-token">
                Setup token
              </label>
              <input
                id="setup-token"
                className="login-input"
                autoComplete="one-time-code"
                spellCheck={false}
                placeholder="XXXX-XXXX-XXXX-…"
                autoFocus
                {...field('token')}
              />
            </div>
            <div className="login-field">
              <label className="login-label" htmlFor="setup-username">
                Username
              </label>
              <input
                id="setup-username"
                className="login-input"
                autoComplete="username"
                {...field('username')}
              />
            </div>
            <div className="login-field">
              <label className="login-label" htmlFor="setup-password">
                Password
              </label>
              <input
                id="setup-password"
                className="login-input"
                type="password"
                autoComplete="new-password"
                {...field('password')}
              />
              <span className="setup-hint">
                At least {s.info?.min_password_length ?? 12} characters. A few random words work
                well.
              </span>
            </div>
            <div className="login-field">
              <label className="login-label" htmlFor="setup-confirm">
                Password again
              </label>
              <input
                id="setup-confirm"
                className="login-input"
                type="password"
                autoComplete="new-password"
                {...field('confirm')}
              />
            </div>
            {s.error && (
              <p className="login-error" role="alert">
                {s.error}
              </p>
            )}
            <button className="login-btn" type="submit" disabled={s.busy}>
              {s.busy ? 'Creating account…' : 'Create account'}
            </button>
          </form>
        )}

        {s.step === 'resolvers' && (
          <div>
            <p className="setup-lead">
              Svart answers from its cache when it can and asks one of these resolvers when it
              can't. All of them are encrypted, so your ISP can't read or rewrite your lookups.
            </p>
            <fieldset className="setup-options">
              <legend className="visually-hidden">Resolver</legend>
              {resolverPresets.map((p) => (
                <label
                  key={p.id}
                  className={`setup-option ${s.resolver === p.id ? 'selected' : ''}`}
                >
                  <input
                    type="radio"
                    name="resolver"
                    checked={s.resolver === p.id}
                    onChange={() => {
                      dispatch({ type: 'resolverChosen', id: p.id });
                    }}
                  />
                  <span className="setup-option-label">{p.label}</span>
                  <span className="setup-option-desc">{p.description}</span>
                </label>
              ))}
            </fieldset>
            {s.error && (
              <p className="login-error" role="alert">
                {s.error}
              </p>
            )}
            <button
              className="login-btn"
              type="button"
              onClick={action(applyResolvers)}
              disabled={s.busy}
            >
              {s.busy ? 'Saving…' : 'Use these resolvers'}
            </button>
            <p className="setup-footnote">
              You can add more, including plain DNS or DNS-over-TLS, later under Config.
            </p>
          </div>
        )}

        {s.step === 'blocking' && (
          <div>
            <p className="setup-lead">
              Pick one or more published blocklists. Svart downloads them now and refreshes them
              every day. Later you can give each device, group or network segment its own lists and
              exceptions.
            </p>
            <fieldset className="setup-options">
              <legend className="visually-hidden">Blocklists</legend>
              {listPresets.map((p) => (
                <label
                  key={p.id}
                  className={`setup-option ${s.lists.includes(p.id) ? 'selected' : ''}`}
                >
                  <input
                    type="checkbox"
                    checked={s.lists.includes(p.id)}
                    onChange={() => {
                      dispatch({ type: 'listToggled', id: p.id });
                    }}
                  />
                  <span className="setup-option-label">{p.label}</span>
                  <span className="setup-option-desc">{p.description}</span>
                </label>
              ))}
            </fieldset>
            <label className="setup-toggle">
              <input
                type="checkbox"
                checked={s.protectEveryone}
                onChange={() => {
                  dispatch({ type: 'protectEveryoneToggled' });
                }}
              />
              Block these for every device on my network
            </label>
            {s.error && (
              <p className="login-error" role="alert">
                {s.error}
              </p>
            )}
            <button
              className="login-btn"
              type="button"
              onClick={action(applyBlocking)}
              disabled={s.busy || s.lists.length === 0}
            >
              {s.busy ? 'Adding lists…' : 'Add lists'}
            </button>
            <button
              className="setup-skip"
              type="button"
              onClick={() => {
                dispatch({ type: 'skipped' });
              }}
              disabled={s.busy}
            >
              Skip — I'll set up blocking later
            </button>
          </div>
        )}

        {s.step === 'finish' && (
          <div>
            <p className="setup-lead">
              Last step: tell your devices to use Svart. The easiest way is your router's DHCP
              settings, so every device picks it up automatically. Set the DNS server to:
            </p>
            {(s.info?.addresses?.length ?? 0) > 0 ? (
              <ul className="setup-addresses">
                {s.info?.addresses?.map((a) => (
                  <li key={a}>
                    <code>{a}</code>
                  </li>
                ))}
              </ul>
            ) : s.info?.in_container ? (
              <p className="setup-lead">
                <strong>your host's LAN IP address</strong>. Svart runs in a container, so it can
                only see the container's internal address. On the host, <code>hostname -I</code>{' '}
                (Linux) or your router's device list shows the right one. Set{' '}
                <code>EXTERNAL_IP</code> to have Svart show it here.
              </p>
            ) : (
              <p className="setup-lead">this machine's LAN IP address.</p>
            )}
            {port !== '53' && (
              <p className="setup-warning">
                Svart is listening on port <code>{port}</code>, but devices only send DNS to port
                53. Publish port 53 to it, for example{' '}
                <code>
                  docker run -p 53:{port}/udp -p 53:{port}/tcp …
                </code>
                , or set <code>DNS_PORT=53</code>.
              </p>
            )}
            {s.applied.length > 0 && (
              <ul className="setup-applied">
                {s.applied.map((a) => (
                  <li key={a}>{a}</li>
                ))}
              </ul>
            )}
            <button
              className="login-btn"
              type="button"
              onClick={action(() => navigate('/', { replace: true }))}
            >
              Open the dashboard
            </button>
          </div>
        )}
      </div>
    </div>
  );
}
