import { queryOptions } from '@tanstack/react-query';
import { api } from './api';

export const agentsOptions = queryOptions({
  queryKey: ['agents'],
  queryFn: ({ signal }) => api.listAgents(signal),
  retry: false,
  refetchOnWindowFocus: true,
  refetchInterval: 10_000,
});

export function agentConfigOptions(instanceUID: string) {
  return queryOptions({
    queryKey: ['agent-config', instanceUID],
    queryFn: ({ signal }) => api.agentConfigStatus(instanceUID, signal),
    retry: false,
    refetchOnWindowFocus: true,
    refetchInterval: 10_000,
  });
}
