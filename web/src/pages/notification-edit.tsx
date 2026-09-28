import { MenuItem, TextField } from '@mui/material';
import Box from '@mui/material/Box';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { useNavigate, useParams } from '@tanstack/react-router';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { ButtonLink } from '../components/links';
import { NotificationEmailFields } from '../components/notification-email-fields';
import { NotificationPagerDutyFields } from '../components/notification-pagerduty-fields';
import { NotificationTelegramFields } from '../components/notification-telegram-fields';
import {
  Button,
  Empty,
  ErrorState,
  FormError,
  Loading,
  PageHeading,
  Panel,
} from '../components/ui';
import {
  api,
  defaultNotificationDelivery,
  type NotificationChannel,
  type NotificationInput,
} from '../data/api';
import { notificationOptions } from '../data/notifications';

export function NotificationCreatePage() {
  return <NotificationEditor />;
}
export function NotificationEditPage() {
  const { notificationId } = useParams({ from: '/notifications/$notificationId' });
  const query = useQuery(notificationOptions(notificationId));
  const { t } = useTranslation();
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  if (!query.data)
    return (
      <Empty
        title={t('Channel not found.')}
        description={t('This channel may have been deleted.')}
        action={<ButtonLink to="/notifications">{t('Notifications')}</ButtonLink>}
      />
    );
  return <NotificationEditor key={query.data.id} channel={query.data} />;
}
function NotificationEditor({ channel }: { channel?: NotificationChannel }) {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const client = useQueryClient();
  const [input, setInput] = useState<NotificationInput>(() => {
    if (channel) {
      const { id: _id, ...value } = channel;
      return value;
    }
    return {
      name: '',
      type: 'webhook',
      config: { url: '' },
      delivery: defaultNotificationDelivery,
    };
  });
  const [wait, setWait] = useState(String(input.delivery.group_wait_seconds));
  const [interval, setInterval] = useState(String(input.delivery.group_interval_seconds));
  const [repeat, setRepeat] = useState(String(input.delivery.repeat_interval_seconds));
  const save = useMutation({
    mutationFn: () => {
      const value = {
        ...input,
        delivery: {
          ...input.delivery,
          group_wait_seconds: Number(wait),
          group_interval_seconds: Number(interval),
          repeat_interval_seconds: Number(repeat),
        },
      };
      return channel ? api.updateNotification(channel.id, value) : api.createNotification(value);
    },
    onSuccess: async (value) => {
      client.setQueryData(notificationOptions(value.id).queryKey, value);
      await client.invalidateQueries({ queryKey: ['notifications'] });
      await navigate({ to: '/notifications' });
    },
  });
  return (
    <>
      <ButtonLink to="/notifications">← {t('Notifications')}</ButtonLink>
      <PageHeading
        eyebrow={t('Notifications')}
        title={channel ? t('Edit channel') : t('Create channel')}
        description={t(
          'Saved changes are applied automatically. Delivery starts when a rule uses this channel.',
        )}
      />
      <Panel>
        <Box
          component="form"
          onSubmit={(event) => {
            event.preventDefault();
            save.mutate();
          }}
          sx={{ p: 3, display: 'flex', flexDirection: 'column', gap: 3, maxWidth: 720 }}
        >
          <TextField
            label={t('Channel name')}
            required
            value={input.name}
            onChange={(e) => setInput({ ...input, name: e.target.value })}
            slotProps={{ htmlInput: { minLength: 2, maxLength: 80 } }}
          />
          <TextField
            select
            label={t('Channel')}
            value={input.type}
            onChange={(event) => {
              const type = event.target.value;
              if (type === 'pagerduty')
                setInput({
                  ...input,
                  type,
                  config: { url: 'https://events.pagerduty.com/v2/enqueue', routing_key: '' },
                });
              else if (type === 'telegram')
                setInput({
                  ...input,
                  type,
                  config: {
                    api_url: 'https://api.telegram.org',
                    bot_token: '',
                    chat_id: 0,
                    message_thread_id: 0,
                  },
                });
              else if (type === 'email')
                setInput({
                  ...input,
                  type,
                  config: {
                    smarthost: '',
                    from: '',
                    to: '',
                    tls_mode: 'starttls',
                    auth_username: '',
                    auth_password: '',
                  },
                });
              else if (
                type === 'webhook' ||
                type === 'discord' ||
                type === 'slack' ||
                type === 'msteams'
              )
                setInput({ ...input, type, config: { url: '' } });
            }}
          >
            <MenuItem value="webhook">{t('Webhook')}</MenuItem>
            <MenuItem value="discord">Discord</MenuItem>
            <MenuItem value="slack">Slack</MenuItem>
            <MenuItem value="msteams">Microsoft Teams</MenuItem>
            <MenuItem value="telegram">Telegram</MenuItem>
            <MenuItem value="pagerduty">PagerDuty</MenuItem>
            <MenuItem value="email">{t('Email')}</MenuItem>
          </TextField>
          {input.type === 'pagerduty' ? (
            <NotificationPagerDutyFields
              config={input.config}
              onChange={(config) => setInput({ ...input, config })}
            />
          ) : input.type === 'telegram' ? (
            <NotificationTelegramFields
              config={input.config}
              onChange={(config) => setInput({ ...input, config })}
            />
          ) : input.type === 'email' ? (
            <NotificationEmailFields
              config={input.config}
              onChange={(config) => setInput({ ...input, config })}
            />
          ) : (
            <TextField
              label={t('Webhook URL')}
              type="url"
              required
              value={input.config.url}
              onChange={(e) => setInput({ ...input, config: { url: e.target.value } })}
              slotProps={{ htmlInput: { maxLength: 2048, autoComplete: 'off' } }}
              helperText={
                input.type === 'msteams'
                  ? t(
                      'Paste a Teams Workflows webhook URL that accepts requests from anyone. Messages use adaptive cards.',
                    )
                  : input.type === 'webhook'
                    ? t('HTTP or HTTPS. The destination receives the Alertmanager webhook payload.')
                    : t(
                        'Paste the incoming webhook URL provided by the service. Alerts and ended conditions are sent to its linked channel.',
                      )
              }
            />
          )}
          <TextField
            select
            label={t('Group alerts')}
            value={input.delivery.group_by}
            onChange={(e) =>
              setInput({
                ...input,
                delivery: {
                  ...input.delivery,
                  group_by: e.target.value === 'resource' ? 'resource' : 'rule',
                },
              })
            }
            helperText={t(
              'Resource grouping combines rules for the same Agent or Monitor. Each Monitor stays separate from its Agent.',
            )}
          >
            <MenuItem value="rule">{t('By rule')}</MenuItem>
            <MenuItem value="resource">{t('By Agent or Monitor')}</MenuItem>
          </TextField>
          <TextField
            label={t('Initial wait (seconds)')}
            type="number"
            required
            value={wait}
            onChange={(e) => setWait(e.target.value)}
            slotProps={{ htmlInput: { min: 0, max: 3600, step: 1 } }}
            helperText={t(
              'Wait before the first message to collect alerts. From 0 to 3600 seconds.',
            )}
          />
          <TextField
            label={t('Update interval (seconds)')}
            type="number"
            required
            value={interval}
            onChange={(e) => setInterval(e.target.value)}
            slotProps={{ htmlInput: { min: 1, max: 86400, step: 1 } }}
            helperText={t(
              'Minimum interval between messages about changes in a group. From 1 to 86400 seconds.',
            )}
          />
          <TextField
            label={t('Repeat interval (seconds)')}
            type="number"
            required
            value={repeat}
            onChange={(e) => setRepeat(e.target.value)}
            slotProps={{
              htmlInput: { min: Number(interval) || 1, max: 432000, step: Number(interval) || 1 },
            }}
            helperText={t(
              'Reminder for an unchanged alert group. Use a multiple of the update interval, up to 432000 seconds (5 days).',
            )}
          />
          <FormError error={save.error} />
          <Box sx={{ display: 'flex', gap: 2, flexWrap: 'wrap' }}>
            <Button type="submit" variant="primary" busy={save.isPending}>
              {t('Save channel')}
            </Button>
            <ButtonLink to="/notifications">{t('Cancel')}</ButtonLink>
          </Box>
        </Box>
      </Panel>
    </>
  );
}
