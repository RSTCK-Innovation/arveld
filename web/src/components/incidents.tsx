import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { Incident } from '../data/api';
import { incidentsOptions } from '../data/incidents';
import { ruleConditionLabels } from '../data/notifications';
import { formatDate, formatNumber } from '../i18n/format';
import { ButtonLink } from './links';
import { RecordsTable } from './records-table';
import { Badge, Empty, ErrorState, Loading, Panel } from './ui';

export function IncidentTable({ incidents }: { incidents: Incident[] }) {
  const { t } = useTranslation();
  return (
    <RecordsTable
      label={t('Incident history')}
      columns={[
        t('Resource'),
        t('Condition'),
        t('Severity'),
        t('Status'),
        t('Opened'),
        t('Actions'),
      ]}
      rows={incidents.map((incident) => ({
        id: incident.id,
        cells: [
          incident.owner_name,
          incident.threshold === undefined
            ? t(ruleConditionLabels[incident.condition])
            : `${t(ruleConditionLabels[incident.condition])} > ${formatNumber(incident.threshold)} ${incident.condition === 'latency' ? 'ms' : '%'}`,
          t({ info: 'Information', warning: 'Warning', critical: 'Critical' }[incident.severity]),
          <Badge
            key="status"
            tone={
              incident.status === 'closed'
                ? 'neutral'
                : incident.acknowledged_at
                  ? 'warning'
                  : 'danger'
            }
          >
            {t(
              incident.status === 'closed'
                ? 'Closed'
                : incident.acknowledged_at
                  ? 'Acknowledged'
                  : 'Open',
            )}
          </Badge>,
          formatDate(incident.opened_at, { dateStyle: 'medium', timeStyle: 'short' }),
          <ButtonLink key="detail" to="/alerts/$alertId" params={{ alertId: incident.id }}>
            {t('View incident')}
          </ButtonLink>,
        ],
      }))}
    />
  );
}

export function ResourceIncidents({ kind, id }: { kind: 'monitor' | 'agent'; id: string }) {
  const { t } = useTranslation();
  const query = useQuery(incidentsOptions({ owner_kind: kind, owner_id: id, limit: 10 }));
  return (
    <Panel
      title={t('Incident history')}
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
          title={t('No incidents')}
          description={t('Observed firing episodes will appear here.')}
        />
      )}
    </Panel>
  );
}
