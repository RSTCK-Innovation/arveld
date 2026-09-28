import { Alert, MenuItem, TextField } from '@mui/material';
import Box from '@mui/material/Box';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams, useRouter } from '@tanstack/react-router';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
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
import { api, silenceInputSchema } from '../data/api';
import { monitorOptions } from '../data/monitors';

function localInput(time: number) {
  const date = new Date(time);
  return new Date(time - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}

export function MonitorSilenceCreatePage() {
  const { monitorId } = useParams({ from: '/monitors/$monitorId/silences/new' });
  return <SilenceForm key={monitorId} scope="monitor" id={monitorId} />;
}

export function AgentSilenceCreatePage() {
  const { agentId } = useParams({ from: '/agents/$agentId/silences/new' });
  return <SilenceForm key={agentId} scope="agent" id={agentId} />;
}

function SilenceForm({ scope, id }: { scope: 'monitor' | 'agent'; id: string }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const router = useRouter();
  const client = useQueryClient();
  const monitor = useQuery({ ...monitorOptions(id), enabled: scope === 'monitor' });
  const agents = useQuery({ ...agentsOptions, enabled: scope === 'agent' });
  const agent = agents.data?.find((value) => value.instance_uid === id);
  const ownerQuery = scope === 'agent' ? agents : monitor;
  const owner = scope === 'agent' ? agent : monitor.data;
  const ownerLink =
    scope === 'agent'
      ? ({ to: '/agents/$agentId', params: { agentId: id }, search: { tab: 'alerts' } } as const)
      : ({ to: '/monitors/$monitorId', params: { monitorId: id } } as const);
  const goBack = () => {
    if (router.history.canGoBack()) router.history.back();
    else void navigate({ ...ownerLink, replace: true });
  };
  const [minutes, setMinutes] = useState('30');
  const [comment, setComment] = useState('');
  const [scheduled, setScheduled] = useState(false);
  const [start, setStart] = useState(() => localInput(Date.now() + 3600_000));
  const [end, setEnd] = useState(() => localInput(Date.now() + 7200_000));
  const startTime = new Date(start).getTime();
  const endTime = new Date(end).getTime();
  const validDates =
    Number.isFinite(startTime) &&
    Number.isFinite(endTime) &&
    localInput(startTime) === start &&
    localInput(endTime) === end;
  const input = scheduled
    ? {
        starts_at: validDates ? new Date(startTime).toISOString() : '',
        duration_seconds: (endTime - startTime) / 1000,
        comment,
      }
    : { duration_seconds: Number(minutes) * 60, comment };
  const valid =
    (scheduled ? validDates : Number.isInteger(Number(minutes))) &&
    silenceInputSchema.safeParse(input).success;
  const save = useMutation({
    mutationFn: async () => {
      if (scope === 'agent') await api.createAgentSilence(id, input);
      else await api.createMonitorSilence(id, input);
    },
    onSuccess: async () => {
      await client.invalidateQueries({ queryKey: ['silences'] });
      goBack();
    },
    onError: () => client.invalidateQueries({ queryKey: ['silences'] }),
  });
  const back = (
    <Button variant="ghost" onClick={goBack} disabled={save.isPending}>
      ← {t('Back')}
    </Button>
  );
  if (ownerQuery.isPending) return <Loading />;
  if (ownerQuery.isError)
    return <ErrorState error={ownerQuery.error} retry={() => void ownerQuery.refetch()} />;
  if (!owner)
    return (
      <Empty
        title={t(scope === 'agent' ? 'Agent not found' : 'Monitor not found.')}
        description={t(
          scope === 'agent'
            ? 'This agent is no longer available.'
            : 'This monitor may have been deleted.',
        )}
        action={back}
      />
    );
  return (
    <>
      {back}
      <PageHeading
        eyebrow={t('Notification silences')}
        title={t('Create silence')}
        description={t(
          scope === 'agent'
            ? 'Suspend notifications for {{name}} and all its Monitors, now or later. Measurements and alert evaluation continue.'
            : 'Suspend notifications for all alert rules on {{name}}, now or later. Measurements and alert evaluation continue.',
          { name: scope === 'agent' ? (agent?.hostname ?? id) : monitor.data?.name },
        )}
      />
      <Panel>
        <Box
          component="form"
          onSubmit={(event) => {
            event.preventDefault();
            if (valid && !save.isPending) save.mutate();
          }}
          sx={{ p: 3, maxWidth: 720, display: 'flex', flexDirection: 'column', gap: 3 }}
        >
          <TextField
            select
            label={t('Start')}
            value={scheduled ? 'scheduled' : 'now'}
            onChange={(event) => setScheduled(event.target.value === 'scheduled')}
            disabled={save.isPending}
          >
            <MenuItem value="now">{t('Start now')}</MenuItem>
            <MenuItem value="scheduled">{t('Schedule for later')}</MenuItem>
          </TextField>
          {scheduled ? (
            <>
              <TextField
                type="datetime-local"
                label={t('Start date and time')}
                required
                value={start}
                onChange={(event) => setStart(event.target.value)}
                disabled={save.isPending}
                slotProps={{ inputLabel: { shrink: true } }}
                error={!!start && (!validDates || startTime <= Date.now())}
                helperText={t('Choose a future start. Times use your browser’s time zone.')}
              />
              <TextField
                type="datetime-local"
                label={t('End date and time')}
                required
                value={end}
                onChange={(event) => setEnd(event.target.value)}
                disabled={save.isPending}
                slotProps={{ inputLabel: { shrink: true } }}
                error={
                  !!end && (!validDates || endTime <= startTime || endTime - startTime > 604800_000)
                }
                helperText={t('The end must follow the start, within 7 days.')}
              />
            </>
          ) : (
            <TextField
              label={t('Duration (minutes)')}
              type="number"
              required
              value={minutes}
              onChange={(event) => setMinutes(event.target.value)}
              disabled={save.isPending}
              slotProps={{ htmlInput: { min: 1, max: 10080, step: 1 } }}
              helperText={t('From 1 minute to 7 days. The silence starts when you save.')}
            />
          )}
          <TextField
            label={t('Reason')}
            multiline
            minRows={3}
            required
            value={comment}
            onChange={(event) => setComment(event.target.value)}
            disabled={save.isPending}
            error={
              comment.length > 0 && !silenceInputSchema.shape.comment.safeParse(comment).success
            }
            slotProps={{ htmlInput: { maxLength: 1024 } }}
            helperText={
              comment.length > 0 && !silenceInputSchema.shape.comment.safeParse(comment).success
                ? t('The reason is empty, too long or contains an invalid character.')
                : t('Briefly explain why notifications are suspended.')
            }
          />
          <FormError error={save.error} />
          {save.isError && (
            <Alert severity="warning">
              {t(
                scope === 'agent'
                  ? 'The silence may already have been created. Check the Agent’s silences before trying again.'
                  : 'The silence may already have been created. Check the Monitor’s silences before trying again.',
              )}
            </Alert>
          )}
          <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 2 }}>
            <Button type="submit" variant="primary" busy={save.isPending} disabled={!valid}>
              {t('Create silence')}
            </Button>
            <Button variant="ghost" onClick={goBack} disabled={save.isPending}>
              {t('Back')}
            </Button>
          </Box>
        </Box>
      </Panel>
    </>
  );
}
