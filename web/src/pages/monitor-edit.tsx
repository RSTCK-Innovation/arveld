import { useQuery } from '@tanstack/react-query';
import { useParams } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { ButtonLink } from '../components/links';
import { MonitorForm } from '../components/monitor-wizard';
import { Empty, ErrorState, Loading, PageHeading, Panel } from '../components/ui';
import { monitorOptions } from '../data/monitors';

export function MonitorEditPage() {
  const { t } = useTranslation();
  const { monitorId } = useParams({ from: '/monitors/$monitorId/edit' });
  const query = useQuery(monitorOptions(monitorId));
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  const monitor = query.data;
  if (!monitor)
    return (
      <Empty
        title={t('Monitor not found.')}
        description={t('This monitor may have been deleted.')}
        action={
          <ButtonLink to="/monitors" search={{ q: '', status: 'all', create: false }}>
            ← {t('Monitors')}
          </ButtonLink>
        }
      />
    );
  return (
    <>
      <ButtonLink to="/monitors/$monitorId" params={{ monitorId }}>
        ← {t('Monitor details')}
      </ButtonLink>
      <PageHeading eyebrow={t('Monitors')} title={t('Edit monitor')} description={monitor.name} />
      <Panel>
        <MonitorForm monitor={monitor} />
      </Panel>
    </>
  );
}
