import ArrowRight from '@mui/icons-material/ArrowForward';
import { TextField } from '@mui/material';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AuthLayout } from '../components/auth-layout';
import { useNotify } from '../components/notifications';
import { Button, FormError } from '../components/ui';
import { validateSignup } from '../data/account';
import { ApiError, api } from '../data/api';
import { refreshSession } from '../data/session';
import { useSubmit } from '../data/use-submit';
export function SignupPage() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const notify = useNotify();
  const create = useSubmit();
  const [name, setName] = useState('');
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [confirmation, setConfirmation] = useState('');
  return (
    <AuthLayout
      title={
        <>
          {t('A space of your own.')}
          <br />
          {t('A view of everything.')}
        </>
      }
      description={t('Your machines, your services, your ideas. Find your services in one place.')}
    >
      <Typography component="h1" variant="h5" sx={{ mb: 2 }}>
        {t('Welcome home.')}
      </Typography>
      <Typography component="p" variant="body2" sx={{ mb: 2 }}>
        {t('First launch: create the administrator account for your workspace.')}
      </Typography>
      <Box
        component="form"
        onSubmit={(e) => {
          e.preventDefault();
          const input = { name, email, password, confirmation };
          setPassword('');
          setConfirmation('');
          void create.submit(async () => {
            const profile = validateSignup(input);
            try {
              await api.createAccount({ ...profile, password: input.password });
            } catch (error) {
              if (error instanceof ApiError && error.status === 409) {
                notify(error, 'error');
                await refreshSession(client);
                return;
              }
              throw error;
            }
            notify('Account created. Sign in to continue.');
            await refreshSession(client);
          });
        }}
        sx={{ display: 'flex', flexDirection: 'column', gap: 3 }}
      >
        <TextField
          fullWidth
          label={t('Full name')}
          autoComplete="name"
          placeholder={t('Camille Martin')}
          required
          value={name}
          onChange={(e) => setName(e.target.value)}
          slotProps={{
            htmlInput: {
              minLength: 2,
            },
          }}
        />
        <TextField
          fullWidth
          label={t('Email address')}
          autoComplete="email"
          type="email"
          placeholder={t('camille@example.com')}
          required
          value={email}
          onChange={(e) => setEmail(e.target.value)}
          slotProps={{
            htmlInput: {
              maxLength: 254,
            },
          }}
        />
        <Box
          component="div"
          sx={{
            display: 'grid',
            gap: 3,
            gridTemplateColumns: { xs: 'minmax(0,1fr)', sm: 'repeat(2,minmax(0,1fr))' },
          }}
        >
          <TextField
            fullWidth
            label={t('Password')}
            autoComplete="new-password"
            type="password"
            placeholder={t('15 to 128 characters')}
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            slotProps={{
              htmlInput: {
                minLength: 15,
              },
            }}
          />
          <TextField
            fullWidth
            label={t('Confirm password')}
            autoComplete="new-password"
            type="password"
            placeholder={t('Once more')}
            required
            value={confirmation}
            onChange={(e) => setConfirmation(e.target.value)}
          />
        </Box>

        <FormError error={create.error} />
        <Button endIcon={<ArrowRight />} type="submit" variant="primary" busy={create.pending}>
          {t('Create my account')}
        </Button>
      </Box>
    </AuthLayout>
  );
}
