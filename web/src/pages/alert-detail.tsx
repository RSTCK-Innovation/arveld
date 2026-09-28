import { List, ListItem, ListItemText, Typography } from '@mui/material';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useParams } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { ButtonLink } from '../components/links';
import {
  Badge,
  Button,
  Empty,
  ErrorState,
  FormError,
  Loading,
  PageHeading,
  Panel,
} from '../components/ui';
import { api } from '../data/api';
import { incidentClosureLabels, incidentOptions } from '../data/incidents';
import { alertRuleOptions, ruleConditionLabels } from '../data/notifications';
import { formatDate, formatNumber } from '../i18n/format';

export function AlertDetailPage() {
  const { alertId } = useParams({ from: '/alerts/$alertId' });
  const query = useQuery(incidentOptions(alertId));
  const client = useQueryClient();
  const rule = useQuery({ ...alertRuleOptions(query.data?.rule_id ?? ''), enabled: !!query.data });
  const { t } = useTranslation();
  const acknowledge = useMutation({
    mutationFn: () => api.acknowledgeIncident(alertId),
    onSuccess: async (value) => {
      client.setQueryData(incidentOptions(alertId).queryKey, value);
      await client.invalidateQueries({ queryKey: ['incidents'] });
    },
  });
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  const incident = query.data;
  if (!incident)
    return (
      <Empty
        title={t('Incident not found.')}
        description={t('This incident is no longer available.')}
        action={<ButtonLink to="/alerts">{t('Alerts')}</ButtonLink>}
      />
    );
  const when = (value: string) => formatDate(value, { dateStyle: 'medium', timeStyle: 'medium' });
  const events = [
    { label: t('Incident opened'), time: incident.opened_at },
    ...(incident.acknowledged_at
      ? [
          {
            label: t('Acknowledged by {{name}}', { name: incident.acknowledged_by }),
            time: incident.acknowledged_at,
          },
        ]
      : []),
    ...(incident.closed_at
      ? [
          {
            label: t(incidentClosureLabels[incident.close_reason ?? 'condition_ended']),
            time: incident.closed_at,
          },
        ]
      : []),
  ].sort((a, b) => a.time.localeCompare(b.time));
  return (
    <>
      <ButtonLink to="/alerts">← {t('Incident history')}</ButtonLink>
      <PageHeading
        eyebrow={t('Incident')}
        title={incident.owner_name}
        description={t(
          'Closure records the end of an alert episode; it does not confirm service recovery.',
        )}
        actions={
          <Button
            variant="primary"
            disabled={!!incident.acknowledged_at || incident.status === 'closed'}
            busy={acknowledge.isPending}
            onClick={() => acknowledge.mutate()}
          >
            {t(incident.acknowledged_at ? 'Acknowledged' : 'Acknowledge')}
          </Button>
        }
      />
      <FormError error={acknowledge.error} />
      <Panel title={t('Alert origin')}>
        <List>
          <ListItem>
            <ListItemText
              primary={t('Condition')}
              secondary={`${t(ruleConditionLabels[incident.condition])}${incident.threshold === undefined ? '' : ` > ${formatNumber(incident.threshold)} ${incident.condition === 'latency' ? 'ms' : '%'}`} `}
            />
            <Badge tone={incident.status === 'closed' ? 'neutral' : 'warning'}>
              {t(incident.status === 'closed' ? 'Closed' : 'Open')}
            </Badge>
          </ListItem>
          <ListItem>
            <ListItemText
              primary={t('Duration')}
              secondary={t('{{count}} second', { count: incident.for_seconds })}
            />
          </ListItem>
          <ListItem>
            <ListItemText
              primary={t('Severity')}
              secondary={t(
                { info: 'Information', warning: 'Warning', critical: 'Critical' }[
                  incident.severity
                ],
              )}
            />
          </ListItem>
          <ListItem>
            <ListItemText
              primary={t('Last observed evaluation')}
              secondary={when(incident.last_evaluated_at)}
            />
          </ListItem>
          {rule.data && (
            <ListItem sx={{ gap: 2, flexWrap: 'wrap' }}>
              <ButtonLink
                to={incident.owner_kind === 'agent' ? '/agents/$agentId' : '/monitors/$monitorId'}
                params={
                  incident.owner_kind === 'agent'
                    ? { agentId: incident.owner_id }
                    : { monitorId: incident.owner_id }
                }
              >
                {t('View resource')}
              </ButtonLink>
              <ButtonLink to="/alert-rules/$ruleId" params={{ ruleId: incident.rule_id }}>
                {t('View rule')}
              </ButtonLink>
              {incident.owner_kind === 'monitor' && (
                <ButtonLink
                  to="/monitors/$monitorId/silences/new"
                  params={{ monitorId: incident.owner_id }}
                >
                  {t('Silence notifications')}
                </ButtonLink>
              )}
            </ListItem>
          )}
          {rule.data === null && (
            <ListItem>
              <Typography color="text.secondary">
                {t('The original rule was removed. Its historical settings are preserved here.')}
              </Typography>
            </ListItem>
          )}
        </List>
      </Panel>
      <Panel title={t('Timeline')}>
        <List>
          {events.map((event) => (
            <ListItem key={event.label}>
              <ListItemText primary={event.label} secondary={when(event.time)} />
            </ListItem>
          ))}
        </List>
      </Panel>
    </>
  );
}
