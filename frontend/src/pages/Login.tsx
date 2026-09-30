import { useLoginController } from './Login.controller';

import '../styles/pages/login.css';

export default function Login() {
  const model = useLoginController();
  if (model === null) return null;
  return <LoginView model={model} />;
}

function LoginView({ model }: { model: NonNullable<ReturnType<typeof useLoginController>> }) {
  const { action, username, setUsername, password, setPassword, error, handleSubmit } = model;
  return (
    <div className="login-page">
      <div className="login-card">
        <div className="login-logo">
          <div className="login-logo-icon" />
          <span className="login-logo-text">
            SVART <span className="login-logo-sub">DNS</span>
          </span>
          <p className="login-subtitle">Sign in to continue</p>
        </div>
        <form onSubmit={action(handleSubmit)}>
          <div className="login-field">
            <label className="login-label" htmlFor="login-username">
              Username
            </label>
            <input
              id="login-username"
              className="login-input"
              type="text"
              value={username}
              onChange={(e) => {
                setUsername(e.target.value);
              }}
              placeholder="Enter username"
              autoComplete="username"
            />
          </div>
          <div className="login-field">
            <label className="login-label" htmlFor="login-password">
              Password
            </label>
            <input
              id="login-password"
              className="login-input"
              type="password"
              value={password}
              onChange={(e) => {
                setPassword(e.target.value);
              }}
              placeholder="Enter password"
              autoComplete="current-password"
              autoFocus
            />
          </div>
          {error && <p className="login-error">{error}</p>}
          <button className="login-btn" type="submit">
            Sign In
          </button>
        </form>
      </div>
    </div>
  );
}
