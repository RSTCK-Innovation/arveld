import { queryOptions } from '@tanstack/react-query';
import { api } from './api';

export const monitorsOptions = queryOptions({
  queryKey: ['monitors'],
  queryFn: ({ signal }) => api.listMonitors(signal),
  retry: false,
  refetchOnWindowFocus: true,
  refetchInterval: 10_000,
});

export function monitorOptions(id: string) {
  return queryOptions({
    queryKey: ['monitor', id],
    queryFn: ({ signal }) => api.monitor(id, signal),
    retry: false,
    refetchOnWindowFocus: true,
  });
}
