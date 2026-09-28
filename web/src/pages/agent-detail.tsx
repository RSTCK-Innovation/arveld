import Clock from '@mui/icons-material/AccessTime';
import ArrowLeft from '@mui/icons-material/ArrowBack';
import Server from '@mui/icons-material/DnsOutlined';
import Cpu from '@mui/icons-material/Memory';
import HardDrive from '@mui/icons-material/Storage';
import { Alert, Paper, ToggleButton } from '@mui/material';
import Box from '@mui/material/Box';
import Typography from '@mui/material/Typography';
import { useQuery } from '@tanstack/react-query';
import { useNavigate, useParams, useSearch } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { AgentConfiguration } from '../components/agent-configuration';
import { AgentMetricHistory, AgentMetricValue, AgentUptime } from '../components/agent-metrics';
import { ResourceAlertRules } from '../components/alert-rules';
import { ResourceIncidents } from '../components/incidents';
import { AppLink, ButtonLink } from '../components/links';
import { MemoryStickIcon } from '../components/memory-stick-icon';
import { MonitorList } from '../components/monitor-list';
import { NotificationSilences } from '../components/notification-silences';
import {
  Badge,
  Empty,
  ErrorState,
  Loading,
  PageHeading,
  Panel,
  relativeTime,
} from '../components/ui';
import { agentsOptions } from '../data/agents';

export function AgentDetailPage() {
  const { t } = useTranslation();
  const { agentId } = useParams({ from: '/agents/$agentId' });
  const query = useQuery(agentsOptions);
  const search = useSearch({ from: '/agents/$agentId' });
  const navigate = useNavigate({ from: '/agents/$agentId' });
  const tab = search.tab ?? 'overview';
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  const a = query.data.find((item) => item.instance_uid === agentId);
  if (!a)
    return (
      <Empty
        title={t('Agent not found')}
        description={t('This agent is no longer available.')}
        action={
          <ButtonLink
            to="/agents"
            search={{ q: '', status: 'all' }}
            variant="contained"
            color="primary"
          >
            {t('All agents')}
          </ButtonLink>
        }
      />
    );
  const lastSeen = a.last_seen_at ? relativeTime(a.last_seen_at) : t('Unavailable');
  return (
    <>
      <AppLink
        to="/agents"
        search={{ q: '', status: 'all' }}
        sx={{ display: 'flex', alignItems: 'center', gap: 2, mb: 2 }}
      >
        <ArrowLeft />
        {t('All agents')}
      </AppLink>
      <Box sx={{ overflowWrap: 'anywhere' }}>
        <PageHeading
          eyebrow={t('Agent')}
          title={a.hostname ?? a.instance_uid}
          description={t('Connection and identity reported by this agent.')}
          actions={
            <Badge tone={a.connected ? 'good' : 'neutral'}>
              {a.connected ? t('Connected') : t('Offline')}
            </Badge>
          }
        />
      </Box>
      {!a.connected && (
        <Alert severity="warning" sx={{ mb: 3 }}>
          {t('This agent is offline. Last seen: {{time}}', { time: lastSeen })}
        </Alert>
      )}
      <Box sx={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 2, mb: 3 }}>
        {(
          [
            ['overview', t('Overview')],
            ['configuration', t('Configuration')],
            ['alerts', t('Alerts')],
            ['activity', t('Activity')],
          ] as const
        ).map(([key, label]) => (
          <ToggleButton
            type="button"
            key={key}
            selected={tab === key}
            value={key}
            onClick={() => void navigate({ search: { tab: key } })}
          >
            {label}
          </ToggleButton>
        ))}
      </Box>
      {tab === 'overview' && (
        <>
          <Box
            sx={{
              display: 'grid',
              gap: 3,
              gridTemplateColumns: {
                xs: '1fr',
                sm: 'repeat(2,minmax(0,1fr))',
                lg: 'repeat(4,minmax(0,1fr))',
              },
              mb: 3,
            }}
          >
            {[
              { label: t('CPU usage'), icon: Cpu, metric: 'cpu' as const },
              { label: t('Memory usage'), icon: MemoryStickIcon, metric: 'memory' as const },
              { label: t('Disk usage'), icon: HardDrive, metric: 'disk' as const },
              { label: t('Uptime'), icon: Clock },
            ].map((item) => (
              <Paper variant="outlined" key={item.label} sx={{ p: 3 }}>
                <Box
                  sx={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    gap: 2,
                  }}
                >
                  <Typography variant="body2" color="text.secondary">
                    {item.label}
                  </Typography>
                  <item.icon sx={{ width: 17, height: 17 }} />
                </Box>
                {item.metric ? (
                  <AgentMetricValue metric={item.metric} instanceUID={a.instance_uid} />
                ) : (
                  <AgentUptime instanceUID={a.instance_uid} />
                )}
              </Paper>
            ))}
          </Box>
          <Box
            sx={{
              display: 'grid',
              gap: 3,
              gridTemplateColumns: { xs: 'minmax(0,1fr)', lg: 'repeat(2,minmax(0,1fr))' },
              gridAutoRows: '1fr',
              mb: 3,
              '& > section': { alignSelf: 'stretch', mb: 0 },
            }}
          >
            <AgentMetricHistory metric="cpu" instanceUID={a.instance_uid} />
            <AgentMetricHistory metric="memory" instanceUID={a.instance_uid} />
            <AgentMetricHistory metric="disk" instanceUID={a.instance_uid} />
            <AgentMetricHistory metric="network" instanceUID={a.instance_uid} />
          </Box>
          <Panel title={t('Agent information')} action={<Server />}>
            <Box
              component="dl"
              sx={{
                overflowWrap: 'anywhere',
                display: 'grid',
                gap: 3,
                gridTemplateColumns: { xs: 'minmax(0,1fr)', lg: 'repeat(2,minmax(0,1fr))' },
                m: 0,
                p: 3,
              }}
            >
              {[
                [t('Instance UID'), a.instance_uid],
                [t('Hostname'), a.hostname ?? t('Unavailable')],
                [t('Version'), a.version ?? t('Unavailable')],
                [t('Last seen'), lastSeen],
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
            </Box>
          </Panel>
          <Panel title={t('Monitors run by this agent')}>
            <MonitorList agentInstanceUID={a.instance_uid} />
          </Panel>
        </>
      )}
      {tab === 'configuration' && (
        <AgentConfiguration key={a.instance_uid} instanceUID={a.instance_uid} />
      )}
      {tab === 'alerts' && (
        <>
          <ResourceAlertRules scope="agent" id={a.instance_uid} />
          <NotificationSilences agentInstanceUID={a.instance_uid} />
          <ResourceIncidents kind="agent" id={a.instance_uid} />
        </>
      )}
      {tab === 'activity' && (
        <Panel title={t('Activity')}>
          <Empty
            title={t('Unavailable')}
            description={t('Activity history is not available yet.')}
          />
        </Panel>
      )}
    </>
  );
}
