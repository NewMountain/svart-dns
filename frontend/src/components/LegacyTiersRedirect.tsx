import { Navigate, useLocation } from 'react-router-dom';

// /tiers was renamed /assignments; keep the query (tab, selected, search).
export default function LegacyTiersRedirect() {
  const { search } = useLocation();
  return <Navigate to={`/assignments${search}`} replace />;
}
