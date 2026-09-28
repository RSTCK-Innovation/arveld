import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type Agent, api, type Monitor, type Silence } from '../data/api';
import { silencesOptions } from '../data/notifications';
import { formatDate } from '../i18n/format';
import { AppLink, ButtonLink } from './links';
import { Badge, Button, Confirm, Empty, ErrorState, Loading, Panel } from './ui';

export function NotificationSilences({
  monitorId,
  agentInstanceUID,
  monitors,
  agents,
}: {
  monitorId?: string;
  agentInstanceUID?: string;
  monitors?: Monitor[];
  agents?: Agent[];
}) {
  const { t } = useTranslation();
  const query = useQuery(silencesOptions(monitorId, agentInstanceUID));
  const client = useQueryClient();
  const [cancelling, setCancelling] = useState<Silence | null>(null);
  const cancel = useMutation({
    mutationFn: (silence: Silence) =>
      'monitor_id' in silence
        ? api.cancelMonitorSilence(silence.monitor_id, silence.id)
        : api.cancelAgentSilence(silence.agent_instance_uid, silence.id),
    onSuccess: () => setCancelling(null),
    onSettled: () => client.invalidateQueries({ queryKey: ['silences'] }),
  });
  return (
    <Panel
      title={t('Notification silences')}
      subtitle={t(
        monitorId
          ? 'Suspend notifications for this Monitor while measurements and alert evaluation continue.'
          : agentInstanceUID
            ? 'Silences for this Agent also cover its Monitors. Measurements and alert evaluation continue.'
            : 'Immediate and scheduled silences across all Agents and Monitors.',
      )}
      action={
        monitorId ? (
          <ButtonLink
            to="/monitors/$monitorId/silences/new"
            params={{ monitorId }}
            variant="contained"
          >
            {t('Create silence')}
          </ButtonLink>
        ) : agentInstanceUID ? (
          <ButtonLink
            to="/agents/$agentId/silences/new"
            params={{ agentId: agentInstanceUID }}
            variant="contained"
          >
            {t('Create silence')}
          </ButtonLink>
        ) : undefined
      }
    >
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : query.data.length === 0 ? (
        <Empty
          title={t('No silences')}
          description={t(
            monitorId
              ? 'No notification silences are currently recorded for this Monitor.'
              : agentInstanceUID
                ? 'No notification silences are currently recorded for this Agent.'
                : 'No notification silences are currently recorded.',
          )}
        />
      ) : (
        <Box
          component="ul"
          aria-label={t('Notification silences')}
          sx={{ listStyle: 'none', m: 0, p: 0 }}
        >
          {query.data.map((silence) => {
            const monitorSilence = 'monitor_id' in silence ? silence : undefined;
            const owner = monitors?.find((monitor) => monitor.id === monitorSilence?.monitor_id);
            const agentUID =
              'agent_instance_uid' in silence ? silence.agent_instance_uid : undefined;
            const agentOwner = agents?.find((agent) => agent.instance_uid === agentUID);
            return (
              <Box
                component="li"
                key={silence.id}
                sx={{
                  p: 3,
                  borderBottom: 1,
                  borderColor: 'divider',
                  '&:last-child': { borderBottom: 0 },
                  overflowWrap: 'anywhere',
                }}
              >
                {!monitorId && !agentInstanceUID && (
                  <Typography sx={{ mb: 2 }}>
                    {agentUID ? (
                      agentOwner ? (
                        <AppLink to="/agents/$agentId" params={{ agentId: agentUID }}>
                          {t('Agent')}: {agentOwner.hostname ?? agentUID}
                        </AppLink>
                      ) : (
                        t('Unknown Agent ({{id}})', { id: agentUID })
                      )
                    ) : owner ? (
                      <AppLink to="/monitors/$monitorId" params={{ monitorId: owner.id }}>
                        {owner.name}
                      </AppLink>
                    ) : (
                      t('Deleted Monitor ({{id}})', { id: monitorSilence?.monitor_id })
                    )}
                  </Typography>
                )}
                <Box
                  sx={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    flexWrap: 'wrap',
                    gap: 2,
                  }}
                >
                  <Badge tone={silence.state === 'active' ? 'warning' : 'neutral'}>
                    {t(
                      {
                        active: 'Silence active',
                        pending: 'Silence pending',
                        expired: 'Silence expired',
                      }[silence.state],
                    )}
                  </Badge>
                  {silence.state !== 'expired' &&
                    (monitorSilence ? monitorId || owner : agentInstanceUID || agentOwner) && (
                      <Button
                        onClick={() => {
                          cancel.reset();
                          setCancelling(silence);
                        }}
                      >
                        {t('Cancel silence')}
                      </Button>
                    )}
                </Box>
                {agentUID && (
                  <Typography variant="body2" color="text.secondary" sx={{ mt: 2 }}>
                    {t('Covers this Agent and its Monitors.')}
                  </Typography>
                )}
                <Typography sx={{ my: 2, whiteSpace: 'pre-wrap' }}>{silence.comment}</Typography>
                <Box
                  component="dl"
                  sx={{
                    m: 0,
                    display: 'grid',
                    gap: 2,
                    gridTemplateColumns: { xs: 'minmax(0,1fr)', sm: 'repeat(3,minmax(0,1fr))' },
                  }}
                >
                  {[
                    [
                      t('Start'),
                      formatDate(silence.starts_at, { dateStyle: 'medium', timeStyle: 'short' }),
                    ],
                    [
                      t('End'),
                      formatDate(silence.ends_at, { dateStyle: 'medium', timeStyle: 'short' }),
                    ],
                    [t('Created by'), silence.created_by],
                  ].map(([label, value]) => (
                    <div key={label}>
                      <Typography component="dt" variant="body2" color="text.secondary">
                        {label}
                      </Typography>
                      <Typography component="dd" variant="body2" sx={{ m: 0 }}>
                        {value}
                      </Typography>
                    </div>
                  ))}
                </Box>
              </Box>
            );
          })}
        </Box>
      )}
      <Typography variant="body2" color="text.secondary" sx={{ p: 3 }}>
        {t(
          'Expired silences remain visible for a limited time. Times use your browser’s time zone.',
        )}
      </Typography>
      <Confirm
        open={!!cancelling}
        onOpenChange={(open) => {
          if (!open && !cancel.isPending) setCancelling(null);
        }}
        title={t('Cancel this silence?')}
        description={t(
          cancelling && 'agent_instance_uid' in cancelling
            ? '“{{comment}}” will end for this Agent and its Monitors. Other silences and notification delivery delays still apply.'
            : '“{{comment}}” will end. Other silences and notification delivery delays still apply.',
          { comment: cancelling?.comment ?? '' },
        )}
        busy={cancel.isPending}
        error={cancel.error}
        label="Cancel silence"
        onConfirm={() => {
          if (cancelling) cancel.mutate(cancelling);
        }}
      />
    </Panel>
  );
}
