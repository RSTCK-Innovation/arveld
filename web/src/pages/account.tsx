import Save from '@mui/icons-material/SaveOutlined';
import { TextField } from '@mui/material';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useQueryClient } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { LanguageSelect } from '../components/language-select';
import { useNotify } from '../components/notifications';
import { Button, ErrorState, FormError, Loading, PageHeading, Panel } from '../components/ui';
import { type AccountProfile, validatePasswordChange, validateProfile } from '../data/account';
import { api } from '../data/api';
import { forgetSession, refreshSession, useSession } from '../data/session';
import { useSubmit } from '../data/use-submit';

export function AccountPage() {
  const { t } = useTranslation();
  const query = useSession();
  const client = useQueryClient();
  const logout = useSubmit();
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  if (query.data.status !== 'authenticated') return null;
  const account = query.data.account;
  return (
    <>
      <PageHeading
        eyebrow={t('YOUR PERSONAL SPACE')}
        title={t('My account.')}
        description={t('Your profile and password, in one place.')}
        actions={
          <Button
            busy={logout.pending}
            onClick={() =>
              void logout.submit(async () => {
                await api.logout();
                await forgetSession(client);
              })
            }
          >
            {t('Sign out')}
          </Button>
        }
      />
      <FormError error={logout.error} />
      <Box
        sx={{
          display: 'grid',
          gap: 3,
          alignItems: 'start',
          gridTemplateColumns: { xs: 'minmax(0,1fr)', lg: 'minmax(0,2fr) minmax(0,1fr)' },
        }}
      >
        <Box sx={{ minWidth: 0 }}>
          <ProfileForm key={`${account.name}:${account.email}`} account={account} />
          <Panel title={t('Language and region')}>
            <Box sx={{ p: 3 }}>
              <LanguageSelect />
            </Box>
          </Panel>
          <PasswordForm />
        </Box>
        <Panel title={t('My account')}>
          <Box component="dl" sx={{ p: 3, m: 0 }}>
            <Typography component="dt" variant="body2" color="text.secondary">
              {t('Role')}
            </Typography>
            <Typography component="dd" variant="body2" sx={{ m: 0 }}>
              {t('Administrator')}
            </Typography>
          </Box>
        </Panel>
      </Box>
    </>
  );
}

function ProfileForm({ account }: { account: AccountProfile }) {
  const { t } = useTranslation();
  const client = useQueryClient();
  const notify = useNotify();
  const [name, setName] = useState(account.name);
  const [email, setEmail] = useState(account.email);
  const save = useSubmit();
  const dirty = name !== account.name || email !== account.email;
  return (
    <form
      onSubmit={(event) => {
        event.preventDefault();
        void save.submit(async () => {
          await api.saveProfile(validateProfile({ name, email }));
          await refreshSession(client);
          notify('Profile saved');
        });
      }}
    >
      <Panel
        title={t('Personal information')}
        subtitle={t('How would you like to appear in your workspace?')}
      >
        <Box sx={{ p: 3, display: 'flex', flexDirection: 'column', gap: 3 }}>
          <Box
            sx={{
              display: 'grid',
              gap: 3,
              gridTemplateColumns: { xs: 'minmax(0,1fr)', sm: 'repeat(2,minmax(0,1fr))' },
            }}
          >
            <TextField
              fullWidth
              label={t('Full name')}
              required
              autoComplete="name"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <TextField
              fullWidth
              label={t('Email address')}
              required
              type="email"
              autoComplete="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              slotProps={{ htmlInput: { maxLength: 254 } }}
            />
          </Box>
          <FormError error={save.error} />
        </Box>
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            flexWrap: 'wrap',
            gap: 2,
            p: 3,
            justifyContent: 'space-between',
          }}
        >
          <span>{dirty ? t('Unsaved changes') : t('Profile up to date')}</span>
          <Button
            startIcon={<Save />}
            type="submit"
            variant="primary"
            busy={save.pending}
            disabled={!dirty}
          >
            {t('Save profile')}
          </Button>
        </Box>
      </Panel>
    </form>
  );
}

function PasswordForm() {
  const { t } = useTranslation();
  const client = useQueryClient();
  const notify = useNotify();
  const change = useSubmit();
  return (
    <Panel title={t('Change password')} subtitle={t('Choose a new password for your account.')}>
      <Box
        component="form"
        sx={{ p: 3, display: 'flex', flexDirection: 'column', gap: 3 }}
        onSubmit={(event) => {
          event.preventDefault();
          const form = event.currentTarget;
          const fields = new FormData(form);
          const input = {
            currentPassword: String(fields.get('currentPassword') ?? ''),
            password: String(fields.get('password') ?? ''),
            confirmation: String(fields.get('confirmation') ?? ''),
          };
          form.reset();
          void change.submit(async () => {
            validatePasswordChange(input);
            await api.changePassword(input);
            await forgetSession(client);
            notify('Password changed. Sign in again.');
          });
        }}
      >
        <TextField
          fullWidth
          label={t('Current password')}
          name="currentPassword"
          type="password"
          autoComplete="current-password"
          required
        />
        <TextField
          fullWidth
          label={t('New password')}
          name="password"
          type="password"
          autoComplete="new-password"
          required
          helperText={t('15 to 128 characters')}
        />
        <TextField
          fullWidth
          label={t('Confirm new password')}
          name="confirmation"
          type="password"
          autoComplete="new-password"
          required
        />
        <FormError error={change.error} />
        <Box sx={{ display: 'flex', justifyContent: 'flex-end' }}>
          <Button type="submit" variant="primary" busy={change.pending}>
            {t('Change password')}
          </Button>
        </Box>
      </Box>
    </Panel>
  );
}
