import { ReactNode } from 'react';
import { Navigate } from 'react-router-dom';
import { useAuthController } from './RequireAuth.controller';

import RouteFallback from './RouteFallback';

// RequireAuth checks the session once before rendering the app, so pages
// never fire API calls that 401 and bounce, and a fresh install goes straight
// to first-run setup instead of a login form nobody can use yet.
export default function RequireAuth({ children }: { children: ReactNode }) {
  const state = useAuthController();

  if (state === 'checking') return <RouteFallback />;
  if (state === 'setup') return <Navigate to="/setup" replace />;
  if (state === 'login') return <Navigate to="/login" replace />;
  return <>{children}</>;
}
