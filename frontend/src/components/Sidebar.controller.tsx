import { useEffect } from 'react';
import { getApiAuthCheck, postApiAuthLogout } from '../api/operations';
import { useAction } from '../hooks/useAction';
import { useUiField } from '../hooks/useUiField';

export function useSidebarController() {
  const action = useAction();
  const [username, setUsername] = useUiField('Sidebar.username', 'admin');
  const [authEnabled, setAuthEnabled] = useUiField('Sidebar.authEnabled', false);
  useEffect(() => {
    action(() =>
      getApiAuthCheck().then((data) => {
        if (data.username) {
          setUsername(data.username);
        }
        setAuthEnabled(data.authenticated);
      }),
    )();
  }, [action, setAuthEnabled, setUsername]);
  async function handleLogout() {
    try {
      await postApiAuthLogout();
    } catch {
      /* best effort */
    }
    window.location.assign('/login');
  }
  return { action, username, authEnabled, handleLogout };
}
