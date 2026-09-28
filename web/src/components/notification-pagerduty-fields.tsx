import { TextField } from '@mui/material';
import { useTranslation } from 'react-i18next';
import type { PagerDutyNotificationConfig } from '../data/api';

export function NotificationPagerDutyFields({
  config,
  onChange,
}: {
  config: PagerDutyNotificationConfig;
  onChange: (value: PagerDutyNotificationConfig) => void;
}) {
  const { t } = useTranslation();
  return (
    <>
      <TextField
        label={t('PagerDuty routing key')}
        type="password"
        required
        value={config.routing_key}
        onChange={(event) => onChange({ ...config, routing_key: event.target.value })}
        slotProps={{ htmlInput: { maxLength: 512, autoComplete: 'new-password' } }}
        helperText={t(
          'Use an Events API v2 integration key. Grouped alerts share one PagerDuty event, which closes when all conditions end.',
        )}
      />
      <TextField
        label={t('PagerDuty Events API URL')}
        type="url"
        required
        value={config.url}
        onChange={(event) => onChange({ ...config, url: event.target.value })}
        slotProps={{ htmlInput: { maxLength: 2048 } }}
        helperText={t(
          'Keep the default, or use https://events.eu.pagerduty.com/v2/enqueue for the EU region.',
        )}
      />
    </>
  );
}
