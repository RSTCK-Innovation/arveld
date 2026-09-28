import { QueryCache, QueryClient, queryOptions, useQuery } from '@tanstack/react-query';
import type { AccountProfile } from './account';
import { ApiError, api } from './api';

export type Session =
  | { status: 'setup' | 'anonymous' }
  | { status: 'authenticated'; account: AccountProfile };

export const sessionOptions = queryOptions({
  queryKey: ['session'],
  queryFn: async ({ signal }): Promise<Session> => {
    const account = await api.session(signal);
    if (account) return { status: 'authenticated', account };
    const { required } = await api.setup(signal);
    return { status: required ? 'setup' : 'anonymous' };
  },
  retry: false,
  staleTime: 0,
  refetchOnWindowFocus: true,
  refetchInterval: 60_000,
});

export const queryClient = new QueryClient({
  queryCache: new QueryCache({
    onError: (error) => {
      if (
        error instanceof ApiError &&
        error.status === 401 &&
        queryClient.getQueryData<Session>(sessionOptions.queryKey)?.status === 'authenticated'
      )
        void forgetSession(queryClient);
    },
  }),
  defaultOptions: {
    queries: { retry: 1, refetchOnWindowFocus: false },
    mutations: { retry: false },
  },
});

export function useSession() {
  return useQuery(sessionOptions);
}

export async function refreshSession(client: QueryClient) {
  await client.invalidateQueries({ queryKey: sessionOptions.queryKey });
}

export async function forgetSession(client: QueryClient) {
  await client.cancelQueries();
  client.removeQueries({ predicate: (query) => query.queryKey[0] !== 'session' });
  client.setQueryData(sessionOptions.queryKey, { status: 'anonymous' });
}
