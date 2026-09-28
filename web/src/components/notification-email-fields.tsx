import { MenuItem, TextField } from '@mui/material';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { type EmailNotificationConfig, smtpServerSchema } from '../data/api';

// SMTP-specific fields share the channel editor's save and error lifecycle.
export function NotificationEmailFields({
  config,
  onChange,
}: {
  config: EmailNotificationConfig;
  onChange: (value: EmailNotificationConfig) => void;
}) {
  const { t } = useTranslation();
  const [serverTouched, setServerTouched] = useState(false);
  const serverValidation = smtpServerSchema.safeParse(config.smarthost);
  const serverError = serverValidation.success ? '' : t(serverValidation.error.issues[0].message);
  return (
    <>
      <TextField
        label={t('SMTP server')}
        required
        value={config.smarthost}
        error={serverTouched && !!serverError}
        onBlur={() => setServerTouched(true)}
        onInvalid={() => setServerTouched(true)}
        inputRef={(input: HTMLInputElement | null) => input?.setCustomValidity(serverError)}
        onChange={(event) => onChange({ ...config, smarthost: event.target.value })}
        slotProps={{ htmlInput: { maxLength: 320, autoComplete: 'off' } }}
        placeholder="smtp.example.com:587"
        helperText={
          serverTouched && serverError
            ? serverError
            : t('Hostname and port, such as smtp.example.com:587 or [::1]:25.')
        }
      />
      <TextField
        select
        label={t('Connection security')}
        value={config.tls_mode}
        onChange={(event) => {
          const tls_mode = event.target.value;
          if (tls_mode === 'starttls' || tls_mode === 'tls') onChange({ ...config, tls_mode });
          else if (tls_mode === 'none')
            onChange({ ...config, tls_mode, auth_username: '', auth_password: '' });
        }}
        helperText={t(
          'STARTTLS usually uses port 587; implicit TLS usually uses 465. Certificates must be trusted by the controller.',
        )}
      >
        <MenuItem value="starttls">{t('STARTTLS (required)')}</MenuItem>
        <MenuItem value="tls">{t('Implicit TLS')}</MenuItem>
        <MenuItem value="none">{t('Unencrypted relay (no authentication)')}</MenuItem>
      </TextField>
      <TextField
        label={t('Sender email')}
        type="email"
        required
        value={config.from}
        onChange={(event) => onChange({ ...config, from: event.target.value })}
        slotProps={{ htmlInput: { maxLength: 254 } }}
      />
      <TextField
        label={t('Recipient email')}
        type="email"
        required
        value={config.to}
        onChange={(event) => onChange({ ...config, to: event.target.value })}
        slotProps={{ htmlInput: { maxLength: 254 } }}
        helperText={t(
          'One email address without a display name. Use a mailing list address for multiple recipients.',
        )}
      />
      <TextField
        label={t('SMTP username')}
        value={config.auth_username}
        disabled={config.tls_mode === 'none'}
        required={!!config.auth_password}
        onChange={(event) => onChange({ ...config, auth_username: event.target.value })}
        slotProps={{ htmlInput: { maxLength: 254, autoComplete: 'off' } }}
        helperText={t(
          'Leave both credentials empty when your relay does not require authentication.',
        )}
      />
      <TextField
        label={t('SMTP password')}
        type="password"
        value={config.auth_password}
        disabled={config.tls_mode === 'none'}
        required={!!config.auth_username}
        onChange={(event) => onChange({ ...config, auth_password: event.target.value })}
        slotProps={{ htmlInput: { maxLength: 1024, autoComplete: 'new-password' } }}
      />
    </>
  );
}
