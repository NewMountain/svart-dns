import { parseAPIError } from './generated';

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
    public code: string | null = null,
  ) {
    super(message);
    this.name = 'ApiError';
  }
}

export type Decoder<T> = (value: unknown) => T;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

export async function request<T>(
  path: string,
  decode: Decoder<T>,
  options: RequestInit = {},
): Promise<T> {
  const headers = Object.fromEntries(new Headers(options.headers));
  if (!('content-type' in headers)) headers['Content-Type'] = 'application/json';
  const res = await fetch(path, {
    ...options,
    headers,
    credentials: 'same-origin',
    cache: 'no-store',
  });
  if (res.status === 401) {
    const currentPath = window.location.pathname;
    if (!currentPath.startsWith('/login') && !currentPath.startsWith('/setup')) {
      window.location.href = '/login';
      throw new ApiError(401, 'Unauthorized', 'unauthorized');
    }
  }
  const body: unknown = await res.json();
  if (!isRecord(body)) throw new ApiError(res.status, 'Invalid API response');
  if (typeof body.error === 'string') {
    const failure = parseAPIError(body);
    throw new ApiError(res.status, failure.error, failure.error_code);
  }
  if (!res.ok || body.error !== null || !('data' in body)) {
    throw new ApiError(res.status, 'Invalid API response');
  }
  return decode(body.data);
}

export function get<T>(
  path: string,
  decode: Decoder<T>,
  params?: Record<string, string>,
): Promise<T> {
  return request(params ? `${path}?${new URLSearchParams(params)}` : path, decode);
}
