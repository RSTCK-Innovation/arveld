import { MenuItem, TextField } from '@mui/material';
import Box from '@mui/material/Box';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { IncidentTable } from '../components/incidents';
import { Button, Empty, ErrorState, Loading, PageHeading, Panel } from '../components/ui';
import type { IncidentFilters } from '../data/api';
import { incidentsOptions } from '../data/incidents';

export function AlertsPage() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<IncidentFilters['status']>('open');
  const [severity, setSeverity] = useState<IncidentFilters['severity']>();
  const [before, setBefore] = useState<string>();
  const query = useQuery(incidentsOptions({ status, severity, before, limit: 50 }));
  return (
    <>
      <PageHeading
        eyebrow={t('Alerts')}
        title={t('Incident history')}
        description={t('Follow observed alert episodes and record who is taking care of them.')}
      />
      <Panel>
        <Box sx={{ p: 3, display: 'flex', gap: 2, flexWrap: 'wrap' }}>
          <TextField
            select
            label={t('Status')}
            value={status ?? 'all'}
            sx={{ minWidth: 180 }}
            onChange={(e) => {
              setStatus(
                e.target.value === 'open'
                  ? 'open'
                  : e.target.value === 'closed'
                    ? 'closed'
                    : undefined,
              );
              setBefore(undefined);
            }}
          >
            <MenuItem value="open">{t('Open')}</MenuItem>
            <MenuItem value="closed">{t('Closed')}</MenuItem>
            <MenuItem value="all">{t('All')}</MenuItem>
          </TextField>
          <TextField
            select
            label={t('Severity')}
            value={severity ?? 'all'}
            sx={{ minWidth: 180 }}
            onChange={(e) => {
              setSeverity(
                e.target.value === 'critical'
                  ? 'critical'
                  : e.target.value === 'warning'
                    ? 'warning'
                    : e.target.value === 'info'
                      ? 'info'
                      : undefined,
              );
              setBefore(undefined);
            }}
          >
            <MenuItem value="all">{t('All')}</MenuItem>
            <MenuItem value="critical">{t('Critical')}</MenuItem>
            <MenuItem value="warning">{t('Warning')}</MenuItem>
            <MenuItem value="info">{t('Information')}</MenuItem>
          </TextField>
        </Box>
        {query.isPending ? (
          <Loading />
        ) : query.isError ? (
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        ) : query.data.incidents.length ? (
          <IncidentTable incidents={query.data.incidents} />
        ) : (
          <Empty
            title={t('No incidents')}
            description={t('Observed firing episodes will appear here.')}
          />
        )}
        <Box sx={{ p: 3, display: 'flex', gap: 2, flexWrap: 'wrap' }}>
          {before && <Button onClick={() => setBefore(undefined)}>{t('Newest incidents')}</Button>}
          {query.data?.next_before && (
            <Button onClick={() => setBefore(query.data.next_before)}>
              {t('Older incidents')}
            </Button>
          )}
        </Box>
      </Panel>
    </>
  );
}
