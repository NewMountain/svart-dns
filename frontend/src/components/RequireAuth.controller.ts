import { useEffect } from 'react';
import { getApiAuthCheck } from '../api/operations';
import { useUiField } from '../hooks/useUiField';
export function useAuthController() {
  const [state, setState] = useUiField('RequireAuth.state', 'checking');

  useEffect(() => {
    getApiAuthCheck()
      .then((res) => {
        setState(
          res.authenticated
            ? 'ok'
            : 'setup_required' in res && res.setup_required
              ? 'setup'
              : 'login',
        );
      })
      .catch(() => {
        setState('login');
      });
  }, [setState]);

  return state;
}
