import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import { incidentsOptions } from '../data/incidents';
import { IncidentTable } from './incidents';
import { ButtonLink } from './links';
import { Empty, ErrorState, Loading, Panel } from './ui';

export function OverviewAlerting() {
  const { t } = useTranslation();
  const query = useQuery(incidentsOptions({ status: 'open', limit: 5 }));
  return (
    <Panel
      title={t('Open incidents')}
      action={<ButtonLink to="/alerts">{t('All incidents')}</ButtonLink>}
    >
      {query.isPending ? (
        <Loading />
      ) : query.isError ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : query.data.incidents.length ? (
        <IncidentTable incidents={query.data.incidents} />
      ) : (
        <Empty
          title={t('No open incidents')}
          description={t('Observed firing episodes will appear here.')}
        />
      )}
    </Panel>
  );
}
