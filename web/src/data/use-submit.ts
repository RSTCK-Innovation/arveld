import { useQueryClient } from '@tanstack/react-query';
import { useRef, useState } from 'react';
import { ApiError } from './api';
import { forgetSession } from './session';

// Keep form values and returned secrets out of the mutation cache.
export function useSubmit() {
  const client = useQueryClient();
  const active = useRef(false);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<Error | null>(null);
  async function submit(action: () => Promise<void>) {
    if (active.current) return;
    active.current = true;
    setPending(true);
    setError(null);
    try {
      await action();
    } catch (caught) {
      if (caught instanceof ApiError && caught.status === 401) await forgetSession(client);
      setError(caught instanceof Error ? caught : new Error('Unable to complete this action.'));
    } finally {
      active.current = false;
      setPending(false);
    }
  }
  return { submit, pending, error, reset: () => setError(null) };
}
