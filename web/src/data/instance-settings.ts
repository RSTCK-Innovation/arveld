import { queryOptions } from '@tanstack/react-query';
import { api } from './api';

export const retentionOptions = queryOptions({
  queryKey: ['instance-retention'],
  queryFn: ({ signal }) => api.retention(signal),
  retry: false,
  networkMode: 'always',
  refetchOnWindowFocus: true,
});
