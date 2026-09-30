export interface Call {
  path: string;
  method: string;
  body: Record<string, unknown>;
  query: URLSearchParams;
}
export function call(input: RequestInfo | URL, init?: RequestInit): Call {
  const url = new URL(
    typeof input === 'string' ? input : input instanceof URL ? input.href : input.url,
    'http://localhost',
  );
  const parsed: unknown = typeof init?.body === 'string' ? JSON.parse(init.body) : {};
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed))
    throw new Error('Expected JSON object request');
  return {
    path: url.pathname,
    method: init?.method ?? 'GET',
    body: Object.fromEntries(Object.entries(parsed)),
    query: url.searchParams,
  };
}
export function reply(data: unknown) {
  return Promise.resolve(new Response(JSON.stringify({ data, error: null }), { status: 200 }));
}
export function unavailable() {
  return Promise.resolve(
    new Response(
      JSON.stringify({ data: null, error: 'Storage unavailable', error_code: 'unavailable' }),
      { status: 503 },
    ),
  );
}
