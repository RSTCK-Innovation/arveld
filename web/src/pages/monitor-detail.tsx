import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useQuery } from '@tanstack/react-query';
import { useParams } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { ResourceAlertRules } from '../components/alert-rules';
import { HTTPMonitorDetails } from '../components/http-monitor-details';
import { HTTPOptionsSummary } from '../components/http-monitor-settings';
import { ResourceIncidents } from '../components/incidents';
import { AppLink, ButtonLink } from '../components/links';
import { MonitorActions } from '../components/monitor-actions';
import { MonitorHistory, MonitorMeasurement } from '../components/monitor-results';
import { NotificationSilences } from '../components/notification-silences';
import { Empty, ErrorState, Loading, PageHeading, Panel } from '../components/ui';
import { monitorOptions } from '../data/monitors';

export function MonitorDetailPage() {
  const { t } = useTranslation();
  const { monitorId } = useParams({ from: '/monitors/$monitorId' });
  const query = useQuery(monitorOptions(monitorId));
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  const monitor = query.data;
  const back = (
    <ButtonLink to="/monitors" search={{ q: '', status: 'all', create: false }}>
      ← {t('Monitors')}
    </ButtonLink>
  );
  if (!monitor)
    return (
      <Empty
        title={t('Monitor not found.')}
        description={t('This monitor may have been deleted.')}
        action={back}
      />
    );
  return (
    <>
      {back}
      <Box sx={{ overflowWrap: 'anywhere' }}>
        <PageHeading
          eyebrow={monitor.protocol.toUpperCase()}
          title={monitor.name}
          description={monitor.endpoint}
          actions={<MonitorActions key={monitor.id} monitor={monitor} />}
        />
      </Box>
      <Panel title={t('Latest measurement')}>
        <Box sx={{ p: 3 }}>
          <MonitorMeasurement monitor={monitor} />
        </Box>
      </Panel>
      <MonitorHistory monitor={monitor} />
      <ResourceAlertRules scope="monitor" id={monitor.id} />
      <ResourceIncidents kind="monitor" id={monitor.id} />
      <NotificationSilences key={monitor.id} monitorId={monitor.id} />
      {monitor.protocol === 'http' && <HTTPMonitorDetails monitor={monitor} />}
      <Panel title={t('Configuration')}>
        <Box
          component="dl"
          sx={{
            display: 'grid',
            gap: 3,
            gridTemplateColumns: { xs: 'minmax(0,1fr)', sm: 'repeat(2,minmax(0,1fr))' },
            m: 0,
            p: 3,
            overflowWrap: 'anywhere',
          }}
        >
          {[
            [t('Monitor ID'), monitor.id],
            ...(monitor.protocol === 'http' ? [[t('HTTP method'), monitor.method]] : []),
            ...(monitor.protocol === 'icmp' ? [[t('Ping count'), monitor.ping_count]] : []),
            ...(monitor.protocol === 'dns'
              ? [
                  [t('DNS server'), monitor.dns_server],
                  [t('Record type'), monitor.record_type],
                  [t('DNS transport'), monitor.transport],
                ]
              : []),
            [t('Target'), monitor.endpoint],
            [t('Interval (seconds)'), monitor.interval_seconds],
            [t('Timeout (seconds)'), monitor.timeout_seconds],
          ].map(([label, value]) => (
            <div key={label}>
              <Typography component="dt" variant="body2" color="text.secondary">
                {label}
              </Typography>
              <Typography component="dd" variant="body2" sx={{ m: 0 }}>
                {value}
              </Typography>
            </div>
          ))}
          {monitor.protocol === 'http' && <HTTPOptionsSummary monitor={monitor} />}
          <div>
            <Typography component="dt" variant="body2" color="text.secondary">
              {t('Executor agent')}
            </Typography>
            <Box component="dd" sx={{ m: 0 }}>
              <AppLink to="/agents/$agentId" params={{ agentId: monitor.agent_instance_uid }}>
                {monitor.agent_instance_uid} ↗
              </AppLink>
            </Box>
          </div>
        </Box>
      </Panel>
    </>
  );
}
