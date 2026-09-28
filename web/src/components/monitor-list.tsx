import Cable from '@mui/icons-material/Cable';
import Dns from '@mui/icons-material/DnsOutlined';
import Globe from '@mui/icons-material/Language';
import Radar from '@mui/icons-material/Radar';
import { Avatar, List, ListItem, Typography } from '@mui/material';
import Box from '@mui/material/Box';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { Monitor } from '../data/api';
import { monitorHistoriesOptions, monitorResultsOptions } from '../data/monitor-results';
import { monitorsOptions } from '../data/monitors';
import { AppLink } from './links';
import { MonitorResult, MonitorUptime } from './monitor-results';
import { Empty, ErrorState, Loading } from './ui';

const monitorIcons = {
  http: <Globe />,
  tcp: <Cable />,
  dns: <Dns />,
  icmp: <Radar />,
};

export function MonitorList({
  q = '',
  agentInstanceUID,
  limit,
}: {
  q?: string;
  agentInstanceUID?: string;
  limit?: number;
}) {
  const { t } = useTranslation();
  const query = useQuery(monitorsOptions);
  if (query.isPending) return <Loading />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  const assigned = query.data.filter(
    (monitor) => !agentInstanceUID || monitor.agent_instance_uid === agentInstanceUID,
  );
  const filtered = assigned.filter((monitor) =>
    `${monitor.name} ${monitor.endpoint} ${monitor.protocol}`
      .toLowerCase()
      .includes(q.toLowerCase()),
  );
  if (!filtered.length)
    return (
      <Empty
        title={assigned.length ? t('No monitors in this view') : t('No monitors yet')}
        description={t('Change your filters or create your first monitor.')}
      />
    );
  return (
    <>
      <MonitorRows monitors={filtered.slice(0, limit)} />
      <Typography variant="body2" sx={{ p: 2 }} color="text.secondary">
        {t('{{count}} monitor shown', {
          count: Math.min(filtered.length, limit ?? filtered.length),
        })}
      </Typography>
    </>
  );
}

function MonitorRows({ monitors }: { monitors: Monitor[] }) {
  const { t } = useTranslation();
  const results = useQuery(monitorResultsOptions(monitors));
  const history = useQuery(monitorHistoriesOptions(monitors));
  return (
    <List disablePadding>
      {monitors.map((monitor) => (
        <ListItem
          key={monitor.id}
          className="monitor-row"
          divider
          sx={{
            p: { xs: 2, sm: 3 },
            display: 'grid',
            gap: 2,
            alignItems: 'center',
            gridTemplateColumns: {
              xs: 'minmax(0, 1fr) 116px',
              md: 'minmax(0, 1fr) minmax(200px, 1fr) 116px',
            },
            gridTemplateAreas: {
              xs: '"identity result" "history history"',
              md: '"identity history result"',
            },
          }}
        >
          <AppLink
            to="/monitors/$monitorId"
            params={{ monitorId: monitor.id }}
            sx={{
              gridArea: 'identity',
              display: 'flex',
              alignItems: 'center',
              gap: 2,
              minWidth: 0,
            }}
          >
            <Avatar variant="rounded">{monitorIcons[monitor.protocol]}</Avatar>
            <Box sx={{ minWidth: 0, overflowWrap: 'anywhere' }}>
              <Typography variant="subtitle2">{monitor.name}</Typography>
              <Typography variant="body2" color="text.secondary" noWrap title={monitor.endpoint}>
                {monitor.protocol.toUpperCase()} ·{' '}
                {monitor.protocol === 'http' ? `${monitor.method} · ` : ''}
                {monitor.endpoint}
              </Typography>
              <Typography variant="caption" color="text.secondary">
                {t('Every {{seconds}} seconds', { seconds: monitor.interval_seconds })}
              </Typography>
            </Box>
          </AppLink>
          <Box className="monitor-history" sx={{ gridArea: 'history', minWidth: 0 }}>
            <MonitorUptime monitor={monitor} query={history} />
          </Box>
          <Box className="monitor-result" sx={{ gridArea: 'result', minWidth: 0 }}>
            <MonitorResult monitor={monitor} query={results} />
          </Box>
        </ListItem>
      ))}
    </List>
  );
}
