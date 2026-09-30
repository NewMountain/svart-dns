import React, { Suspense } from 'react';
import ReactDOM from 'react-dom/client';
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom';
import App from './App';
import ApplicationStatus from './components/ApplicationStatus';
import LegacyTiersRedirect from './components/LegacyTiersRedirect';
import RouteFallback from './components/RouteFallback';
import { AppStateProvider } from './hooks/AppStateProvider';
import {
  Admin,
  Analysis,
  Config,
  Dashboard,
  Filters,
  Investigation,
  Login,
  Logs,
  Rewrites,
  Setup,
  Tiers,
} from './routeComponents';

const root = document.getElementById('root');
if (!root) throw new Error('The application root element is missing');
ReactDOM.createRoot(root).render(
  <React.StrictMode>
    <AppStateProvider>
      <ApplicationStatus />
      <BrowserRouter>
        <Suspense fallback={<RouteFallback />}>
          <Routes>
            <Route path="/login" element={<Login />} />
            <Route path="/setup" element={<Setup />} />
            <Route element={<App />}>
              <Route index element={<Dashboard />} />
              <Route path="logs" element={<Logs />} />
              <Route path="filter-lists" element={<Filters />} />
              <Route path="assignments" element={<Tiers />} />
              {/* Old names kept working for bookmarks. */}
              <Route path="filters" element={<Navigate to="/filter-lists" replace />} />
              <Route path="tiers" element={<LegacyTiersRedirect />} />
              <Route path="rewrites" element={<Rewrites />} />
              <Route path="config" element={<Config />} />
              <Route path="analysis" element={<Analysis />} />
              <Route path="admin" element={<Admin />} />
              <Route path="investigation" element={<Investigation />} />
            </Route>
            <Route path="*" element={<Navigate to="/" replace />} />
          </Routes>
        </Suspense>
      </BrowserRouter>
    </AppStateProvider>
  </React.StrictMode>,
);
