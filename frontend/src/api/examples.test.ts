import { expect, it } from 'vitest';
import rawContract from '../../../docs/swagger.json?raw';
import * as generated from './generated';

function isParser(value: unknown): value is (input: unknown) => unknown {
  return typeof value === 'function';
}

it('validates the published contract and every JSON example with the generated runtime parsers', () => {
  const contract = generated.parseDocument(JSON.parse(rawContract));
  let checked = 0;
  for (const methods of Object.values(contract.paths ?? {})) {
    for (const operation of Object.values(methods ?? {})) {
      const media = [
        ...Object.values(operation.requestBody?.content ?? {}),
        ...Object.values(operation.responses ?? {}).flatMap((response) =>
          Object.values(response.content ?? {}),
        ),
      ];
      for (const item of media) {
        if (item.example === undefined) continue;
        const reference = item.schema?.$ref;
        expect(reference).toMatch(/^#\/components\/schemas\//);
        if (typeof reference !== 'string') throw new Error('Missing example schema reference');
        const name = `parse${reference.replace('#/components/schemas/', '')}`;
        const parse: unknown = Reflect.get(generated, name);
        if (!isParser(parse)) throw new Error(`Missing generated parser: ${name}`);
        expect(() => parse(item.example)).not.toThrow();
        checked++;
      }
    }
  }
  expect(checked).toBeGreaterThan(500);
});
