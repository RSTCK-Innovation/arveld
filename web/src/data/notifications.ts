import { queryOptions } from '@tanstack/react-query';
import { api } from './api';

export function silencesOptions(monitorId?: string, agentInstanceUID?: string) {
  return queryOptions({
    queryKey: ['silences', monitorId, agentInstanceUID],
    queryFn: ({ signal }) =>
      monitorId
        ? api.listMonitorSilences(monitorId, signal)
        : agentInstanceUID
          ? api.listAgentSilences(agentInstanceUID, signal)
          : api.listSilences(signal),
    retry: false,
    refetchOnWindowFocus: true,
    refetchInterval: 5_000,
  });
}

export const notificationsOptions = queryOptions({
  queryKey: ['notifications'],
  queryFn: ({ signal }) => api.listNotifications(signal),
  retry: false,
  refetchOnWindowFocus: true,
});
export function notificationOptions(id: string) {
  return queryOptions({
    queryKey: ['notification', id],
    queryFn: ({ signal }) => api.notification(id, signal),
    retry: false,
  });
}
export function alertRulesOptions(id: string, scope: 'monitor' | 'agent' = 'monitor') {
  return queryOptions({
    queryKey: ['alert-rules', scope, id],
    queryFn: ({ signal }) =>
      scope === 'agent' ? api.listAgentAlertRules(id, signal) : api.listAlertRules(id, signal),
    retry: false,
    refetchInterval: 10_000,
  });
}
export function alertRuleOptions(id: string) {
  return queryOptions({
    queryKey: ['alert-rule', id],
    queryFn: ({ signal }) => api.alertRule(id, signal),
    retry: false,
  });
}
export function alertRuleStateOptions(id: string) {
  return queryOptions({
    queryKey: ['alert-rule-state', id],
    queryFn: ({ signal }) => api.alertRuleState(id, signal),
    retry: false,
    refetchInterval: 5_000,
  });
}

export const ruleConditionLabels = {
  failed: 'Monitor failure',
  no_data: 'Missing measurements',
  latency: 'Monitor latency',
  cpu: 'CPU usage',
  memory: 'Memory',
  disk: 'Disk',
} as const;
