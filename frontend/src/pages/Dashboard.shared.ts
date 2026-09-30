export type TimeWindow = '5m' | '1h' | '24h' | '7d';
export const validWindows: TimeWindow[] = ['5m', '1h', '24h', '7d'];

export interface DashboardResourceStatus {
  loading: boolean;
  hasResult: boolean;
  error: string | null;
}
