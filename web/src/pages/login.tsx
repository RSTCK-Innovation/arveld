import { TextField } from '@mui/material';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AuthLayout } from '../components/auth-layout';
import { Button, FormError } from '../components/ui';
import { api } from '../data/api';
import { refreshSession } from '../data/session';
import { useSubmit } from '../data/use-submit';

export function LoginPage() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const login = useSubmit();
  return (
    <AuthLayout
      title={t('Welcome back.')}
      description={t('Your agents, monitors and alerts are waiting for you.')}
    >
      <Typography component="h1" variant="h5" sx={{ mb: 2 }}>
        {t('Sign in')}
      </Typography>
      <Typography component="p" variant="body2" sx={{ mb: 2 }}>
        {t('Access your Arveld workspace.')}
      </Typography>
      <Box
        component="form"
        onSubmit={(e) => {
          e.preventDefault();
          const credentials = { email: email.trim().toLowerCase(), password };
          setPassword('');
          void login.submit(async () => {
            await api.login(credentials);
            await refreshSession(client);
          });
        }}
        sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}
      >
        <TextField
          fullWidth
          label={t('Email address')}
          type="email"
          autoComplete="email"
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <TextField
          fullWidth
          label={t('Password')}
          type="password"
          autoComplete="current-password"
          required
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <FormError error={login.error} />
        <Button variant="primary" type="submit" busy={login.pending}>
          {t('Sign in')}
        </Button>
      </Box>
      <Typography component="p" variant="body2" sx={{ mt: 3, mb: 2 }}>
        {t(
          'To reset a password, contact your instance administrator: this is done through the CLI.',
        )}
      </Typography>
    </AuthLayout>
  );
}
