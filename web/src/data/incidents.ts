import { queryOptions } from '@tanstack/react-query';
import { api, type IncidentFilters } from './api';

export function incidentsOptions(filters: IncidentFilters = {}) {
  return queryOptions({
    queryKey: ['incidents', filters],
    queryFn: ({ signal }) => api.listIncidents(filters, signal),
    retry: false,
    refetchInterval: 5_000,
    refetchOnWindowFocus: true,
  });
}
export function incidentOptions(id: string) {
  return queryOptions({
    queryKey: ['incident', id],
    queryFn: ({ signal }) => api.incident(id, signal),
    retry: false,
    refetchInterval: 5_000,
  });
}
export const incidentClosureLabels = {
  condition_ended: 'Condition ended',
  rule_changed: 'Rule changed',
  rule_removed: 'Rule removed',
} as const;
