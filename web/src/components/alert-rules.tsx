import Box from '@mui/material/Box';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type AlertRule, api } from '../data/api';
import {
  alertRuleStateOptions,
  alertRulesOptions,
  notificationsOptions,
  ruleConditionLabels,
} from '../data/notifications';
import { formatNumber } from '../i18n/format';
import { ButtonLink } from './links';
import { RecordsTable } from './records-table';
import { Badge, Button, Confirm, Empty, ErrorState, Loading, Panel } from './ui';

export function NativeRuleState({ id }: { id: string }) {
  const { t } = useTranslation();
  const query = useQuery(alertRuleStateOptions(id));
  if (query.isPending) return <span>{t('Loading…')}</span>;
  if (query.isError) return <Badge tone="warning">{t('Evaluation unavailable')}</Badge>;
  const state = query.data;
  if (state.sync_status === 'pending')
    return <Badge tone="neutral">{t('Waiting for publication')}</Badge>;
  if (state.health !== 'ok') return <Badge tone="warning">{t('Evaluation unavailable')}</Badge>;
  const name = {
    unknown: 'Unknown',
    inactive: 'No active alert',
    pending: 'Condition pending',
    firing: 'Firing',
  }[state.state ?? 'unknown'];
  return (
    <Badge
      tone={state.state === 'firing' ? 'danger' : state.state === 'pending' ? 'warning' : 'neutral'}
    >
      {t(name)}
    </Badge>
  );
}
export function ResourceAlertRules({ id, scope }: { id: string; scope: 'monitor' | 'agent' }) {
  const { t } = useTranslation();
  const query = useQuery(alertRulesOptions(id, scope));
  const channels = useQuery(notificationsOptions);
  const client = useQueryClient();
  const [deleting, setDeleting] = useState<AlertRule | null>(null);
  const remove = useMutation({
    mutationFn: api.deleteAlertRule,
    onSuccess: async () => {
      setDeleting(null);
      await client.invalidateQueries({ queryKey: alertRulesOptions(id, scope).queryKey });
    },
  });
  return (
    <Panel
      title={t('Monitoring rules')}
      action={
        <ButtonLink
          to={
            scope === 'agent'
              ? '/agents/$agentId/alert-rules/new'
              : '/monitors/$monitorId/alert-rules/new'
          }
          params={scope === 'agent' ? { agentId: id } : { monitorId: id }}
          variant="contained"
        >
          {t('Create a rule')}
        </ButtonLink>
      }
    >
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : query.data.length ? (
        <RecordsTable
          label={t('Monitoring rules')}
          columns={[
            t('Condition'),
            t('Duration'),
            t('Severity'),
            t('Notification channels'),
            t('Status'),
            t('Actions'),
          ]}
          rows={query.data.map((rule) => ({
            id: rule.id,
            cells: [
              rule.threshold !== undefined
                ? `${t(ruleConditionLabels[rule.condition])} > ${formatNumber(rule.threshold)} ${rule.condition === 'latency' ? 'ms' : '%'}`
                : t(ruleConditionLabels[rule.condition]),
              t('{{count}} second', { count: rule.for_seconds }),
              t({ info: 'Information', warning: 'Warning', critical: 'Critical' }[rule.severity]),
              rule.notification_ids
                .map((id) => channels.data?.find((channel) => channel.id === id)?.name ?? id)
                .join(', ') || t('None'),
              <NativeRuleState key="state" id={rule.id} />,
              <Box key="actions" sx={{ display: 'flex', gap: 1, flexWrap: 'wrap' }}>
                <ButtonLink to="/alert-rules/$ruleId" params={{ ruleId: rule.id }}>
                  {t('Edit')}
                </ButtonLink>
                <Button
                  variant="danger"
                  onClick={() => {
                    remove.reset();
                    setDeleting(rule);
                  }}
                >
                  {t('Delete')}
                </Button>
              </Box>,
            ],
          }))}
        />
      ) : (
        <Empty
          title={t('No rules')}
          description={t('Choose a condition and the channels that should receive it.')}
        />
      )}
      <Confirm
        open={!!deleting}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
        title={t('Delete this rule?')}
        description={t('The rule and its channel assignments will be removed.')}
        busy={remove.isPending}
        error={remove.error}
        label="Delete"
        onConfirm={() => {
          if (deleting) remove.mutate(deleting.id);
        }}
      />
    </Panel>
  );
}
