import { FormEvent, useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { getApiAuthCheck, postApiAuthLogin } from '../api/operations';
import { useAction } from '../hooks/useAction';
import { useUiField } from '../hooks/useUiField';

export function useLoginController() {
  const action = useAction();
  const navigate = useNavigate();
  const [username, setUsername] = useUiField('Login.username', '');
  const [password, setPassword] = useUiField('Login.password', '');
  const [error, setError] = useUiField('Login.error', '');
  const [loading, setLoading] = useUiField('Login.loading', true);
  useEffect(() => {
    getApiAuthCheck()
      .then((res) => {
        if (res.authenticated) action(() => navigate('/', { replace: true }))();
        else if ('setup_required' in res && res.setup_required)
          action(() => navigate('/setup', { replace: true }))();
        else setLoading(false);
      })
      .catch(() => {
        setLoading(false);
      });
  }, [navigate, action, setLoading]);
  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError('');
    try {
      await postApiAuthLogin({ username, password });
      action(() => navigate('/', { replace: true }))();
    } catch {
      setError('Invalid credentials');
    }
  }
  if (loading) return null;
  return { action, username, setUsername, password, setPassword, error, handleSubmit };
}
