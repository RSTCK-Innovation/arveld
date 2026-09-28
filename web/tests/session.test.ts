import { expect, test } from 'bun:test';
import { ApiError } from '../src/data/api';
import { queryClient, type Session, sessionOptions } from '../src/data/session';

test('a rejected background read cannot replace first-launch setup with login', async () => {
  try {
    queryClient.setQueryData<Session>(sessionOptions.queryKey, { status: 'setup' });
    await expect(
      queryClient.fetchQuery({
        queryKey: ['background-read'],
        retry: false,
        queryFn: async () => {
          throw new ApiError(401, 'Your session has expired. Sign in again.');
        },
      }),
    ).rejects.toThrow('Your session has expired. Sign in again.');
    expect(queryClient.getQueryData<Session>(sessionOptions.queryKey)).toEqual({ status: 'setup' });
  } finally {
    queryClient.clear();
  }
});

test('a rejected authenticated read still clears the session and private query data', async () => {
  try {
    const session: Session = {
      status: 'authenticated',
      account: { name: 'Camille', email: 'camille@example.com' },
    };
    queryClient.setQueryData<Session>(sessionOptions.queryKey, () => session);
    queryClient.setQueryData(['agents'], [{ hostname: 'private-host' }]);
    await expect(
      queryClient.fetchQuery({
        queryKey: ['background-read'],
        retry: false,
        queryFn: async () => {
          throw new ApiError(401, 'Your session has expired. Sign in again.');
        },
      }),
    ).rejects.toThrow('Your session has expired. Sign in again.');
    expect(queryClient.getQueryData<Session>(sessionOptions.queryKey)).toEqual({
      status: 'anonymous',
    });
    expect(queryClient.getQueryData(['agents'])).toBeUndefined();
  } finally {
    queryClient.clear();
  }
});
