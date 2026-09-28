import { useTranslation } from 'react-i18next';
import { ButtonLink } from '../components/links';
import { MonitorForm } from '../components/monitor-wizard';
import { PageHeading, Panel } from '../components/ui';

export function MonitorCreatePage() {
  const { t } = useTranslation();
  return (
    <>
      <ButtonLink to="/monitors" search={{ q: '', status: 'all', create: false }}>
        ← {t('Monitors')}
      </ButtonLink>
      <PageHeading
        eyebrow={t('Monitors')}
        title={t('Create a monitor')}
        description={t('Define the service to monitor in your services.')}
      />
      <Panel>
        <MonitorForm />
      </Panel>
    </>
  );
}
