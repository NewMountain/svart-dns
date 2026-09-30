import { lazy } from 'react';

export const Dashboard = lazy(() => import('./pages/Dashboard'));
export const Logs = lazy(() => import('./pages/Logs'));
export const Filters = lazy(() => import('./pages/Filters'));
export const Tiers = lazy(() => import('./pages/Tiers'));
export const Rewrites = lazy(() => import('./pages/Rewrites'));
export const Config = lazy(() => import('./pages/Config'));
export const Analysis = lazy(() => import('./pages/Analysis'));
export const Admin = lazy(() => import('./pages/Admin'));
export const Investigation = lazy(() => import('./pages/Investigation'));
export const Login = lazy(() => import('./pages/Login'));
export const Setup = lazy(() => import('./pages/Setup'));
