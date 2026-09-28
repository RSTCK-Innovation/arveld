import Check from '@mui/icons-material/Check';
import Save from '@mui/icons-material/SaveOutlined';
import Database from '@mui/icons-material/Storage';
import { MenuItem, TextField } from '@mui/material';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SettingsNavigation } from '../components/settings-navigation';
import { Button, ErrorState, FormError, Loading, PageHeading, Panel } from '../components/ui';
import { retentionOptions } from '../data/instance-settings';
import type { Preferences } from '../data/preferences';
import { usePreferences, useSavePreferences } from '../data/preferences-query';
import { formatDate } from '../i18n/format';

export function SettingsPage() {
  const { t } = useTranslation();
  const preferences = usePreferences();
  const retention = useQuery(retentionOptions);
  return (
    <>
      <PageHeading
        eyebrow={t('YOUR INSTANCE')}
        title={t('Instance settings')}
        description={t('View instance retention and customize preferences saved in this browser.')}
        actions={
          <Button busy={retention.isFetching} onClick={() => void retention.refetch()}>
            {t('Refresh')}
          </Button>
        }
      />
      <SettingsNavigation />
      <Box sx={{ maxWidth: 880 }}>
        <Panel
          title={t('Metrics retention')}
          subtitle={t('Active policy reported by Prometheus')}
          action={<Database />}
        >
          {retention.isPending ? (
            <Loading />
          ) : retention.isError ? (
            <ErrorState error={retention.error} retry={() => void retention.refetch()} />
          ) : (
            <Box sx={{ p: 3, display: 'flex', flexDirection: 'column', gap: 2 }}>
              <Typography
                component="p"
                variant="h5"
                role="status"
                sx={{ fontFamily: 'monospace', overflowWrap: 'anywhere' }}
              >
                {retention.data.storage_retention || t('No time or size limit')}
              </Typography>
              <Typography variant="body2">
                {t('When both limits are enabled, retention uses the first limit reached.')}
              </Typography>
              <Typography variant="body2" color="text.secondary">
                {t('To change retention, edit the instance configuration and restart Arveld.')}
              </Typography>
              <Typography variant="body2" color="text.secondary">
                {t('Last successful refresh')}:{' '}
                {formatDate(retention.dataUpdatedAt, { dateStyle: 'medium', timeStyle: 'medium' })}
              </Typography>
            </Box>
          )}
        </Panel>
        {preferences.isPending ? (
          <Loading />
        ) : preferences.isError ? (
          <ErrorState error={preferences.error} retry={() => void preferences.refetch()} />
        ) : (
          <PreferencesForm key={JSON.stringify(preferences.data)} preferences={preferences.data} />
        )}
      </Box>
    </>
  );
}

function PreferencesForm({ preferences }: { preferences: Preferences }) {
  const { t } = useTranslation();
  const [input, setInput] = useState(preferences);
  const action = useSavePreferences();
  const dirty = JSON.stringify(input) !== JSON.stringify(preferences);
  return (
    <Box
      component="form"
      onSubmit={(e) => {
        e.preventDefault();
        action.mutate(input);
      }}
    >
      <Panel
        title={t('Browser preferences')}
        subtitle={t('Saved on this browser only. These preferences do not change the instance.')}
      >
        <Box sx={{ p: 3, display: 'flex', flexDirection: 'column', gap: 3 }}>
          <TextField
            fullWidth
            label={t('Workspace name')}
            required
            value={input.name}
            onChange={(e) => setInput({ ...input, name: e.target.value })}
            slotProps={{ htmlInput: { minLength: 2, maxLength: 50 } }}
          />
          <TextField
            fullWidth
            select
            label={t('Interface refresh')}
            helperText={t(
              'Reads local state at this interval. Does not generate new measurements.',
            )}
            value={input.refresh}
            onChange={(e) =>
              setInput({ ...input, refresh: e.target.value as Preferences['refresh'] })
            }
          >
            <MenuItem value="off">{t('Manual')}</MenuItem>
            <MenuItem value="15">{t('Every 15 seconds')}</MenuItem>
            <MenuItem value="30">{t('Every 30 seconds')}</MenuItem>
            <MenuItem value="60">{t('Every minute')}</MenuItem>
          </TextField>
          <FormError error={action.error} />
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
          <span>
            {dirty ? (
              t('Unsaved changes')
            ) : (
              <>
                <Check />
                {t('Preferences up to date')}
              </>
            )}
          </span>
          <Button
            startIcon={<Save />}
            type="submit"
            variant="primary"
            busy={action.isPending}
            disabled={!dirty}
          >
            {t('Save')}
          </Button>
        </Box>
      </Panel>
    </Box>
  );
}
