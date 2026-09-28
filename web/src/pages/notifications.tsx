import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ButtonLink } from '../components/links';
import { RecordsTable } from '../components/records-table';
import { Button, Confirm, Empty, ErrorState, Loading, PageHeading, Panel } from '../components/ui';
import { api, type NotificationChannel } from '../data/api';
import { notificationsOptions } from '../data/notifications';

export function NotificationsPage() {
  const { t } = useTranslation();
  const query = useQuery(notificationsOptions);
  const client = useQueryClient();
  const [deleting, setDeleting] = useState<NotificationChannel | null>(null);
  const remove = useMutation({
    mutationFn: api.deleteNotification,
    onSuccess: async () => {
      setDeleting(null);
      await client.invalidateQueries({ queryKey: notificationsOptions.queryKey });
    },
  });
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  return (
    <>
      <PageHeading
        eyebrow={t('Notifications')}
        title={t('Notification channels')}
        description={t('Create destinations, then assign them to an Agent or Monitor rule.')}
        actions={
          <ButtonLink to="/notifications/new" variant="contained">
            {t('Create channel')}
          </ButtonLink>
        }
      />
      <Panel>
        {query.data.length ? (
          <RecordsTable
            label={t('Notification channels')}
            columns={[t('Name'), t('Channel'), t('Group alerts'), t('Actions')]}
            rows={query.data.map((channel) => ({
              id: channel.id,
              cells: [
                <Typography key="name" sx={{ overflowWrap: 'anywhere' }}>
                  {channel.name}
                </Typography>,
                channel.type === 'webhook'
                  ? t('Webhook')
                  : channel.type === 'discord'
                    ? 'Discord'
                    : channel.type === 'slack'
                      ? 'Slack'
                      : channel.type === 'msteams'
                        ? 'Microsoft Teams'
                        : channel.type === 'telegram'
                          ? 'Telegram'
                          : channel.type === 'pagerduty'
                            ? 'PagerDuty'
                            : t('Email'),
                channel.delivery.group_by === 'resource' ? t('By Agent or Monitor') : t('By rule'),
                <Box key="actions" sx={{ display: 'flex', flexWrap: 'wrap', gap: 1 }}>
                  <ButtonLink
                    to="/notifications/$notificationId"
                    params={{ notificationId: channel.id }}
                  >
                    {t('Edit')}
                  </ButtonLink>
                  <Button
                    variant="danger"
                    onClick={() => {
                      remove.reset();
                      setDeleting(channel);
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
            title={t('No notification channels')}
            description={t('Add a destination for your Agent and Monitor alerts.')}
            action={<ButtonLink to="/notifications/new">{t('Create channel')}</ButtonLink>}
          />
        )}
      </Panel>
      <Confirm
        open={!!deleting}
        onOpenChange={(open) => {
          if (!open) setDeleting(null);
        }}
        title={t('Delete this channel?')}
        description={t('A channel assigned to a rule must be unassigned before deletion.')}
        busy={remove.isPending}
        error={remove.error}
        label="Delete"
        onConfirm={() => {
          if (deleting) remove.mutate(deleting.id);
        }}
      />
    </>
  );
}
