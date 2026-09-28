import {
  Checkbox,
  FormControl,
  FormControlLabel,
  FormGroup,
  FormLabel,
  MenuItem,
  TextField,
} from '@mui/material';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ButtonLink } from '../components/links';
import {
  Button,
  Empty,
  ErrorState,
  FormError,
  Loading,
  PageHeading,
  Panel,
} from '../components/ui';
import { agentsOptions } from '../data/agents';
import { type AlertRule, type AlertRuleInput, api } from '../data/api';
import { monitorOptions } from '../data/monitors';
import { alertRuleOptions, notificationsOptions, ruleConditionLabels } from '../data/notifications';

export function AlertRuleCreatePage() {
  const { monitorId } = useParams({ from: '/monitors/$monitorId/alert-rules/new' });
  return <AlertRuleEditor scope="monitor" id={monitorId} />;
}
export function AgentAlertRuleCreatePage() {
  const { agentId } = useParams({ from: '/agents/$agentId/alert-rules/new' });
  return <AlertRuleEditor scope="agent" id={agentId} />;
}
export function AlertRuleEditPage() {
  const { ruleId } = useParams({ from: '/alert-rules/$ruleId' });
  const query = useQuery(alertRuleOptions(ruleId));
  const { t } = useTranslation();
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  if (!query.data)
    return (
      <Empty
        title={t('Rule not found.')}
        description={t('This rule may have been deleted.')}
        action={
          <ButtonLink to="/monitors" search={{ q: '', status: 'all', create: false }}>
            {t('Monitors')}
          </ButtonLink>
        }
      />
    );
  return (
    <AlertRuleEditor
      key={query.data.id}
      scope={query.data.agent_instance_uid ? 'agent' : 'monitor'}
      id={query.data.agent_instance_uid ?? query.data.monitor_id ?? ''}
      rule={query.data}
    />
  );
}
function AlertRuleEditor({
  scope,
  id,
  rule,
}: {
  scope: 'monitor' | 'agent';
  id: string;
  rule?: AlertRule;
}) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const client = useQueryClient();
  const monitor = useQuery({ ...monitorOptions(id), enabled: scope === 'monitor' });
  const agents = useQuery({ ...agentsOptions, enabled: scope === 'agent' });
  const agent = agents.data?.find((value) => value.instance_uid === id);
  const conditions: AlertRuleInput['condition'][] =
    scope === 'agent' ? ['cpu', 'memory', 'disk'] : ['failed', 'no_data', 'latency'];
  const ownerLink = {
    to: scope === 'agent' ? ('/agents/$agentId' as const) : ('/monitors/$monitorId' as const),
    params: scope === 'agent' ? { agentId: id } : { monitorId: id },
  };
  const channels = useQuery(notificationsOptions);
  const [input, setInput] = useState<AlertRuleInput>(
    rule
      ? {
          condition: rule.condition,
          threshold: rule.threshold,
          for_seconds: rule.for_seconds,
          severity: rule.severity,
          notification_ids: rule.notification_ids,
        }
      : {
          condition: scope === 'agent' ? 'cpu' : 'failed',
          threshold: scope === 'agent' ? 80 : undefined,
          for_seconds: 60,
          severity: 'warning',
          notification_ids: [],
        },
  );
  const save = useMutation({
    mutationFn: () =>
      rule
        ? api.updateAlertRule(rule.id, input)
        : scope === 'agent'
          ? api.createAgentAlertRule(id, input)
          : api.createAlertRule(id, input),
    onSuccess: async (value) => {
      client.setQueryData(alertRuleOptions(value.id).queryKey, value);
      await client.invalidateQueries({ queryKey: ['alert-rules'] });
      await client.invalidateQueries({ queryKey: ['alert-rule-state', value.id] });
      await navigate(ownerLink);
    },
  });
  if ((scope === 'monitor' ? monitor.isPending : agents.isPending) || channels.isPending)
    return <Loading />;
  if (scope === 'monitor' && monitor.isError)
    return <ErrorState error={monitor.error} retry={() => void monitor.refetch()} />;
  if (scope === 'agent' && agents.isError)
    return <ErrorState error={agents.error} retry={() => void agents.refetch()} />;
  if (channels.isError)
    return <ErrorState error={channels.error} retry={() => void channels.refetch()} />;
  if (scope === 'monitor' ? !monitor.data : !agent)
    return (
      <Empty
        title={t('Resource not found.')}
        description={t('This resource may have been deleted.')}
      />
    );
  return (
    <>
      <ButtonLink {...ownerLink}>
        ← {scope === 'agent' ? (agent?.hostname ?? id) : monitor.data?.name}
      </ButtonLink>
      <PageHeading
        eyebrow={t('Monitoring rules')}
        title={rule ? t('Edit rule') : t('Create a rule')}
        description={t('Choose a condition and the channels that should receive it.')}
      />
      <Panel>
        <Box
          component="form"
          onSubmit={(e) => {
            e.preventDefault();
            save.mutate();
          }}
          sx={{ p: 3, maxWidth: 720, display: 'flex', flexDirection: 'column', gap: 3 }}
        >
          <TextField
            select
            label={t('Condition')}
            value={input.condition}
            onChange={(e) => {
              const condition =
                conditions.find((value) => value === e.target.value) ?? conditions[0];
              setInput({
                ...input,
                condition,
                threshold: condition === 'latency' ? 500 : scope === 'agent' ? 80 : undefined,
              });
            }}
          >
            {conditions.map((condition) => (
              <MenuItem key={condition} value={condition}>
                {t(ruleConditionLabels[condition])}
              </MenuItem>
            ))}
          </TextField>
          {input.threshold !== undefined && (
            <TextField
              label={
                input.condition === 'latency'
                  ? t('Threshold (milliseconds)')
                  : t('Threshold (percent)')
              }
              type="number"
              required
              value={input.threshold}
              onChange={(e) => setInput({ ...input, threshold: Number(e.target.value) })}
              helperText={t('Triggers when a fresh measurement is strictly above this threshold.')}
              slotProps={{
                htmlInput: {
                  min: 0.001,
                  max: input.condition === 'latency' ? 60000 : 100,
                  step: 'any',
                },
              }}
            />
          )}
          <TextField
            label={t('For (seconds)')}
            type="number"
            required
            value={input.for_seconds}
            onChange={(e) => setInput({ ...input, for_seconds: Number(e.target.value) })}
            slotProps={{ htmlInput: { min: 1, max: 86400, step: 1 } }}
          />
          <TextField
            select
            label={t('Severity')}
            value={input.severity}
            onChange={(e) =>
              setInput({
                ...input,
                severity:
                  e.target.value === 'critical'
                    ? 'critical'
                    : e.target.value === 'info'
                      ? 'info'
                      : 'warning',
              })
            }
          >
            <MenuItem value="info">{t('Information')}</MenuItem>
            <MenuItem value="warning">{t('Warning')}</MenuItem>
            <MenuItem value="critical">{t('Critical')}</MenuItem>
          </TextField>
          <FormControl component="fieldset">
            <FormLabel component="legend">{t('Notification channels')}</FormLabel>
            <FormGroup>
              {channels.data.map((channel) => (
                <FormControlLabel
                  key={channel.id}
                  label={channel.name}
                  control={
                    <Checkbox
                      checked={input.notification_ids.includes(channel.id)}
                      onChange={(_, checked) =>
                        setInput({
                          ...input,
                          notification_ids: checked
                            ? [...input.notification_ids, channel.id]
                            : input.notification_ids.filter((id) => id !== channel.id),
                        })
                      }
                    />
                  }
                />
              ))}
            </FormGroup>
            <Typography variant="body2" color="text.secondary">
              {t('Without a channel, the rule is evaluated but no notification is sent.')}
            </Typography>
            <ButtonLink to="/notifications/new">{t('Create channel')}</ButtonLink>
          </FormControl>
          <Typography variant="body2" color="text.secondary">
            {t(
              'A resolved notification means this condition ended; it does not by itself confirm service recovery.',
            )}
          </Typography>
          <FormError error={save.error} />
          <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
            <Button type="submit" variant="primary" busy={save.isPending}>
              {t('Save rule')}
            </Button>
            <ButtonLink {...ownerLink}>{t('Cancel')}</ButtonLink>
          </Box>
        </Box>
      </Panel>
    </>
  );
}
