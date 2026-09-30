import { expect, it } from 'vitest';
import { errorMessage } from './errors';
it('preserves actionable error messages and explains unknown failures', () => {
  expect(errorMessage(new Error('The list URL must use HTTPS'))).toBe(
    'The list URL must use HTTPS',
  );
  expect(errorMessage('Readback unavailable')).toBe('Readback unavailable');
  expect(errorMessage({ status: 503 })).toBe(
    'The operation failed. Please retry and check the server logs if it continues.',
  );
});
