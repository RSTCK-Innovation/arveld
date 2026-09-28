import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { SettingsNavigation } from '../components/settings-navigation';
import { Badge, Button, ErrorState, Loading, PageHeading, Panel } from '../components/ui';
import { readinessOptions } from '../data/instance-health';
import { formatDate } from '../i18n/format';

export function InstanceHealthPage() {
  const { t } = useTranslation();
  const query = useQuery(readinessOptions);
  const services = [
    { key: 'arveld', label: t('Arveld API') },
    { key: 'prometheus', label: t('Metrics and alert evaluation') },
    { key: 'alertmanager', label: t('Notification service') },
  ] as const;
  return (
    <>
      <PageHeading
        eyebrow={t('YOUR INSTANCE')}
        title={t('Arveld health')}
        description={t('Current availability of your instance services.')}
        actions={
          <Button busy={query.isFetching} onClick={() => void query.refetch()}>
            {t('Refresh')}
          </Button>
        }
      />
      <SettingsNavigation />
      <Panel
        title={t('Services')}
        subtitle={t(
          'Service readiness does not confirm Agent data reception or notification delivery.',
        )}
      >
        {query.isPending ? (
          <Loading />
        ) : query.isError ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : (
          <>
            <Typography component="p" role="status" variant="body2" sx={{ px: 3, pt: 3 }}>
              {query.data.ready ? t('All services are ready.') : t('Some services are not ready.')}
            </Typography>
            {services.map(({ key, label }) => (
              <Box
                key={key}
                sx={{
                  display: 'flex',
                  flexWrap: 'wrap',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  gap: 2,
                  p: 3,
                }}
              >
                <Typography component="strong" variant="subtitle2">
                  {label}
                </Typography>
                <Badge tone={query.data.components[key].ready ? 'good' : 'warning'}>
                  {query.data.components[key].ready ? t('Ready') : t('Not ready')}
                </Badge>
              </Box>
            ))}
            <Typography variant="body2" color="text.secondary" sx={{ px: 3, pb: 3 }}>
              {t('Last successful refresh')}:{' '}
              {formatDate(query.dataUpdatedAt, { dateStyle: 'medium', timeStyle: 'medium' })}
            </Typography>
          </>
        )}
      </Panel>
      <Panel title={t('Arveld version')}>
        <Box sx={{ p: 3 }}>
          <Typography variant="h5" component="strong">
            {query.data?.version ?? '—'}
          </Typography>
        </Box>
      </Panel>
    </>
  );
}
