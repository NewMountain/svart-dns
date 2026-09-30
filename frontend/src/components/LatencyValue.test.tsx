import { cleanup, render, screen } from '@testing-library/react';
import { afterEach, describe, expect, it } from 'vitest';
import { AppStateProvider } from '../hooks/AppStateProvider';
import LatencyValue from './LatencyValue';

afterEach(cleanup);

describe('LatencyValue', () => {
  it('explains that a zero-tick sample is approximate, not instantaneous', () => {
    render(<AppStateProvider>{<LatencyValue microseconds={0} />}</AppStateProvider>);
    expect(screen.getByText('≈0ms')).toHaveAttribute('title', expect.stringContaining('500µs'));
    expect(screen.queryByText('0µs')).not.toBeInTheDocument();
  });

  it.each([
    [501, '501µs'],
    [1000, '1000µs'],
    [16498, '16ms'],
  ])('preserves measured value %i', (microseconds, text) => {
    render(<AppStateProvider>{<LatencyValue microseconds={microseconds} />}</AppStateProvider>);
    expect(screen.getByText(text)).toBeInTheDocument();
  });

  it.each([-940, Number.NaN, Number.POSITIVE_INFINITY])(
    'does not present an invalid sample %s as a latency',
    (microseconds) => {
      render(<AppStateProvider>{<LatencyValue microseconds={microseconds} />}</AppStateProvider>);
      expect(screen.getByText('Unavailable')).toBeInTheDocument();
    },
  );
});
