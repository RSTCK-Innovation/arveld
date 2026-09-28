import { queryOptions } from '@tanstack/react-query';
import { api } from './api';

export const readinessOptions = queryOptions({
  queryKey: ['instance-readiness'],
  queryFn: ({ signal }) => api.readiness(signal),
  retry: false,
  networkMode: 'always',
  refetchOnWindowFocus: true,
  refetchInterval: 5_000,
});
