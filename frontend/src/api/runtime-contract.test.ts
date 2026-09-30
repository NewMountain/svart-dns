import { afterEach, describe, expect, it, vi } from 'vitest';
import rawContract from '../../../docs/swagger.json?raw';
import * as generated from './generated';
import * as operations from './operations';

// Cross-format contract tests: OpenAPI drives fixtures, while the independently
// emitted TypeScript runtime must preserve valid values and reject corrupt data.
const contract = generated.parseDocument(JSON.parse(rawContract));
const definitions = contract.components.schemas ?? {};
type Schema = generated.Schema;

function resolve(schema: Schema): Schema {
  if (!schema.$ref) return schema;
  const name = schema.$ref.replace('#/components/schemas/', '');
  const target = definitions[name];
  if (!target) throw new Error(`Unresolved contract schema: ${name}`);
  return resolve(target);
}

function sample(schema: Schema, mode: number, depth = 0): unknown {
  const shape = resolve(schema);
  const variants = shape.anyOf;
  if (variants?.length) {
    const selected = variants[mode % variants.length];
    if (!selected) throw new Error('Contract contains an empty union member');
    return sample(selected, mode, depth + 1);
  }
  if (shape.enum?.length) return shape.enum[mode % shape.enum.length];
  switch (shape.type) {
    case 'null':
      return null;
    case 'boolean':
      return mode % 2 === 0;
    case 'integer':
      return mode === 0 ? 0 : 42;
    case 'number':
      return mode === 0 ? 0 : 12.75;
    case 'string':
      return mode === 0 ? '' : 'example.com / café?x=1&y=2';
    case 'array':
      return mode === 0 || depth > 4 ? [] : [sample(shape.items ?? {}, mode, depth + 1)];
    case 'object': {
      const value: Record<string, unknown> = {};
      for (const [name, field] of Object.entries(shape.properties ?? {})) {
        if ((mode === 0 || depth > 4) && !shape.required?.includes(name)) continue;
        if (!field) throw new Error(`Missing field schema: ${name}`);
        value[name] = sample(field, mode, depth + 1);
      }
      if (shape.additionalProperties && mode !== 0 && depth <= 4) {
        value['example-key'] = sample(shape.additionalProperties, mode, depth + 1);
      }
      return value;
    }
    case undefined:
      return { nested: ['example.com', 42, false, null] };
    default:
      throw new Error(`Unrecognized OpenAPI type: ${shape.type}`);
  }
}

function callable(module: object, name: string): (...args: unknown[]) => unknown {
  const fn: unknown = Reflect.get(module, name);
  if (typeof fn !== 'function') throw new Error(`Missing generated runtime export: ${name}`);
  return (...args) => Reflect.apply(fn, undefined, args) as unknown;
}

// These are incompatible JSON types, not unknown sentinels that JSON could never
// transport. Every object shape must reject a non-object, arrays reject scalars,
// and numeric schemas reject strings and non-finite JS values before use.
function invalidValues(schema: Schema): unknown[] {
  const shape = resolve(schema);
  if (shape.anyOf) {
    const types = shape.anyOf.flatMap((part) => (part ? [resolve(part).type] : []));
    if (!types.includes('string')) return ['wrong JSON type'];
    if (!types.includes('array')) return [[]];
    return [];
  }
  switch (shape.type) {
    case 'object':
      return [null, [], 'wrong JSON type', 42];
    case 'array':
      return [{}, 'wrong JSON type'];
    case 'string':
      return [42, false, {}];
    case 'boolean':
      return ['false', 0, {}];
    case 'integer':
      return ['42', 1.25, Number.NaN, Number.POSITIVE_INFINITY];
    case 'number':
      return ['42', Number.NaN, Number.POSITIVE_INFINITY];
    case 'null':
      return [false, 0, ''];
    default:
      return [];
  }
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe('all published schema codecs', () => {
  for (const [name, schema] of Object.entries(definitions)) {
    if (!schema) throw new Error(`Missing schema ${name}`);
    it(`${name}: preserves populated/empty/nullable variants and rejects malformed fields`, () => {
      const parse = callable(generated, `parse${name}`);
      const is = callable(generated, `is${name}`);
      for (const mode of [0, 1, 2, 3]) {
        const valid = sample(schema, mode);
        expect(is(valid)).toBe(true);
        expect(parse(valid)).toEqual(valid);
      }
      for (const invalid of invalidValues(schema)) {
        expect(is(invalid)).toBe(false);
        expect(() => parse(invalid)).toThrow(`Invalid API response: ${name}`);
      }
      const shape = resolve(schema);
      if (shape.type !== 'object') return;
      const populated = sample(schema, 2);
      if (typeof populated !== 'object' || populated === null || Array.isArray(populated))
        throw new Error('Expected object fixture');
      for (const required of shape.required ?? []) {
        const missing = { ...populated };
        Reflect.deleteProperty(missing, required);
        expect(is(missing), `required field ${required}`).toBe(false);
        expect(() => parse(missing)).toThrow();
      }
      for (const [field, child] of Object.entries(shape.properties ?? {})) {
        if (!child) throw new Error(`Missing field schema: ${field}`);
        for (const invalid of invalidValues(child)) {
          const corrupt = { ...populated, [field]: invalid };
          expect(is(corrupt), `invalid field ${field}`).toBe(false);
          expect(() => parse(corrupt)).toThrow();
        }
      }
    });
  }
});

const routes = Object.entries(contract.paths ?? {}).flatMap(([path, methods]) =>
  Object.entries(methods ?? {})
    .filter(
      ([, operation]) =>
        !['/api/openapi.json', '/docs/swagger.json', '/openapi.json'].includes(path) &&
        Object.entries(operation.responses ?? {}).some(
          ([code, response]) => code.startsWith('2') && response.content?.['application/json'],
        ),
    )
    .map(([method, operation]) => ({ path, method, operation })),
);

describe('all generated operations at the HTTP boundary', () => {
  it('covers every exported operation, with no missing or orphan wrapper', () => {
    expect(
      routes
        .map(({ operation }) => operation.operationId.replace(/^./, (c) => c.toLowerCase()))
        .sort(),
    ).toEqual(Object.keys(operations).sort());
  });
  for (const { path, method, operation } of routes) {
    it(`${method.toUpperCase()} ${path}: preserves transport, decoded data, errors and URL encoding`, async () => {
      const name = operation.operationId.replace(/^./, (c) => c.toLowerCase());
      const call = callable(operations, name);
      const pathParameters = operation.parameters?.filter((p) => p.in === 'path') ?? [];
      const args: unknown[] = [];
      let expectedPath = path;
      for (const parameter of pathParameters) {
        const value = '2001:db8::7/a b?x=1&y=%';
        args.push(value);
        expectedPath = expectedPath.replace(`{${parameter.name}}`, encodeURIComponent(value));
      }
      const bodySchema = operation.requestBody?.content?.['application/json']?.schema;
      const body = bodySchema ? sample(bodySchema, 2) : undefined;
      if (bodySchema) args.push(body);
      const successes = Object.entries(operation.responses ?? {}).filter(([code]) =>
        code.startsWith('2'),
      );
      for (const [code, response] of successes) {
        const schema = response.content?.['application/json']?.schema;
        if (!schema) throw new Error('Missing successful JSON schema');
        const envelope = sample(schema, 2);
        if (typeof envelope !== 'object' || envelope === null || !('data' in envelope))
          throw new Error('Missing successful data envelope');
        const query = { search: 'café /?&=', offset: '0' };
        const fetch = vi.fn(() =>
          Promise.resolve(Response.json(envelope, { status: Number(code) })),
        );
        vi.stubGlobal('fetch', fetch);
        expect(await call(...args, query)).toEqual(envelope.data);
        expect(fetch).toHaveBeenLastCalledWith(
          `${expectedPath}?search=caf%C3%A9+%2F%3F%26%3D&offset=0`,
          {
            method: method.toUpperCase(),
            ...(bodySchema ? { body: JSON.stringify(body) } : {}),
            headers: { 'Content-Type': 'application/json' },
            credentials: 'same-origin',
            cache: 'no-store',
          },
        );
        expect(await call(...args)).toEqual(envelope.data);
        expect(fetch.mock.calls).toHaveLength(2);
        expect(fetch).toHaveBeenLastCalledWith(expectedPath, expect.any(Object));
      }
      vi.stubGlobal(
        'fetch',
        vi.fn(() =>
          Promise.resolve(
            Response.json(
              { data: null, error: 'Storage unavailable', error_code: 'unavailable' },
              { status: 503 },
            ),
          ),
        ),
      );
      await expect(call(...args)).rejects.toMatchObject({
        name: 'ApiError',
        status: 503,
        code: 'unavailable',
        message: 'Storage unavailable',
      });
      vi.stubGlobal(
        'fetch',
        vi.fn(() => Promise.resolve(Response.json({ data: 'wrong shape', error: null }))),
      );
      await expect(call(...args)).rejects.toThrow('Invalid API response');
    });
  }
});

it.each(['/dashboard', '/login', '/setup'])(
  'handles unauthorized sessions on %s without decoding protected data',
  async (pathname) => {
    const location = { pathname, href: pathname };
    vi.stubGlobal('window', { location });
    vi.stubGlobal(
      'fetch',
      vi.fn(() =>
        Promise.resolve(
          Response.json(
            { data: null, error: 'Session expired', error_code: 'unauthorized' },
            { status: 401 },
          ),
        ),
      ),
    );
    const client = await import('./client');
    await expect(
      client.get('/api/blocklists', generated.parseGetApiBlocklistsData),
    ).rejects.toMatchObject({
      status: 401,
      code: 'unauthorized',
      message: pathname === '/dashboard' ? 'Unauthorized' : 'Session expired',
    });
    expect(location.href).toBe(pathname === '/dashboard' ? '/login' : pathname);
  },
);

it('encodes query values on direct reads and propagates failed or malformed transport', async () => {
  const client = await import('./client');
  const fetch = vi.fn(() => Promise.resolve(Response.json({ data: [], error: null })));
  vi.stubGlobal('fetch', fetch);
  expect(
    await client.get('/api/blocklists', generated.parseGetApiBlocklistsData, {
      search: 'café /?&=',
    }),
  ).toEqual([]);
  expect(fetch).toHaveBeenCalledWith(
    '/api/blocklists?search=caf%C3%A9+%2F%3F%26%3D',
    expect.objectContaining({ credentials: 'same-origin', cache: 'no-store' }),
  );
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.reject(new Error('Connection interrupted'))),
  );
  await expect(client.get('/api/blocklists', generated.parseGetApiBlocklistsData)).rejects.toThrow(
    'Connection interrupted',
  );
  vi.stubGlobal(
    'fetch',
    vi.fn(() => Promise.resolve(new Response('{unfinished'))),
  );
  await expect(client.get('/api/blocklists', generated.parseGetApiBlocklistsData)).rejects.toThrow(
    SyntaxError,
  );
});
