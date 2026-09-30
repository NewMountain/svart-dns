import { fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { describe, expect, it } from 'vitest';
import ApplicationStatus from '../components/ApplicationStatus';
import { AppStateProvider } from './AppStateProvider';
import { useAction } from './useAction';

function EditForm({ save }: { save: (value: string) => Promise<void> | void }) {
  const action = useAction();
  const [value, setValue] = useState('printer.home');
  return (
    <>
      <input
        aria-label="Domain"
        value={value}
        onChange={(event) => {
          setValue(event.currentTarget.value);
        }}
      />
      <button onClick={action(() => save(value))}>Save</button>
    </>
  );
}
describe('application effect boundary', () => {
  it.each([
    () => {
      throw new Error('Invalid IP address');
    },
    () => Promise.reject(new Error('Invalid IP address')),
  ])(
    'reports rejected effects without losing the current form and allows dismissal',
    async (save) => {
      render(
        <AppStateProvider>
          <ApplicationStatus />
          <EditForm save={save} />
        </AppStateProvider>,
      );
      fireEvent.change(screen.getByLabelText('Domain'), { target: { value: 'scanner.home' } });
      fireEvent.click(screen.getByRole('button', { name: 'Save' }));
      expect(await screen.findByRole('alert')).toHaveTextContent('Invalid IP address');
      expect(screen.getByLabelText('Domain')).toHaveValue('scanner.home');
      fireEvent.click(screen.getByRole('button', { name: 'Dismiss error' }));
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
      expect(screen.getByLabelText('Domain')).toHaveValue('scanner.home');
    },
  );
  it('leaves the error projection empty after a successful effect', () => {
    let saved = '';
    render(
      <AppStateProvider>
        <ApplicationStatus />
        <EditForm
          save={(value) => {
            saved = value;
          }}
        />
      </AppStateProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));
    expect(saved).toBe('printer.home');
    expect(screen.queryByRole('alert')).not.toBeInTheDocument();
  });
});
