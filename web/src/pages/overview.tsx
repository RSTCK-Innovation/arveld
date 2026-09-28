import Plus from '@mui/icons-material/Add';
import ArrowRight from '@mui/icons-material/ArrowForward';
import Server from '@mui/icons-material/DnsOutlined';
import Cpu from '@mui/icons-material/Memory';
import ArrowUpRight from '@mui/icons-material/NorthEast';
import Memory from '@mui/icons-material/StorageOutlined';
import {
  Box,
  LinearProgress,
  Paper,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  Tooltip,
  Typography,
} from '@mui/material';
import { LineChart } from '@mui/x-charts/LineChart';
import { frFRLocalText } from '@mui/x-charts/locales';
import { useQuery } from '@tanstack/react-query';
import { type ReactNode, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { AppLink, ButtonLink } from '../components/links';
import { MonitorList } from '../components/monitor-list';
import { MonitorSummary } from '../components/monitor-results';
import { OverviewAlerting } from '../components/overview-alerting';
import {
  Badge,
  ErrorState,
  PageHeading,
  Panel,
  Progress,
  RangeSelect,
  relativeTime,
} from '../components/ui';
import { agentsOptions } from '../data/agents';
import {
  type FleetPoint,
  overviewHistoryOptions,
  overviewMetricsOptions,
  summarizeFleet,
} from '../data/overview';
import type { Range } from '../data/time-range';
import { formatDate, formatNumber, formatPercent } from '../i18n/format';
import './overview.css';

export function OverviewPage() {
  const { t, i18n } = useTranslation();
  const inventory = useQuery(agentsOptions);
  const agents = inventory.isError ? [] : (inventory.data ?? []);
  const ids = agents.filter((agent) => agent.connected).map((agent) => agent.instance_uid);
  const [range, setRange] = useState<Range>('24h');
  const metrics = useQuery(overviewMetricsOptions(ids));
  const history = useQuery(overviewHistoryOptions(ids, range));
  const readings = metrics.isError ? {} : (metrics.data ?? {});
  const cpu = summarizeFleet(readings, 'cpu');
  const memory = summarizeFleet(readings, 'memory');
  const disk = summarizeFleet(readings, 'disk');
  const cores = summarizeFleet(readings, 'cores');
  const ram = summarizeFleet(readings, 'ram');
  const pending = inventory.isPending || metrics.isPending;
  const coverage = (count: number) =>
    pending
      ? t('Loading data')
      : t('Measurements from {{count}} / {{total}} connected agents', { count, total: ids.length });
  const trend = (field: 'cpu' | 'memory') => (
    <FleetTrend
      points={history.isError ? [] : (history.data?.[field] ?? [])}
      label={t('Fleet trend · {{range}}', { range })}
    />
  );
  const points = history.isError ? [] : (history.data?.cpu ?? []);
  const hasHistory = points.some((point) => point.value !== null || point.previous !== null);
  return (
    <>
      <PageHeading
        eyebrow={t('YOUR CONTROL CENTER')}
        title={t('Your services, in view.')}
        description={t('A look at your machines, your services and what needs your attention.')}
        actions={
          <>
            <RangeSelect value={range} onChange={setRange} />
            <ButtonLink startIcon={<Plus />} to="/monitors/new" variant="contained" color="primary">
              {t('Create a monitor')}
            </ButtonLink>
          </>
        }
      />
      {inventory.isError && (
        <ErrorState error={inventory.error} retry={() => void inventory.refetch()} />
      )}
      {metrics.isError && <ErrorState error={metrics.error} retry={() => void metrics.refetch()} />}
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
        <Stat
          label={t('Connected agents')}
          value={inventory.isPending || inventory.isError ? '—' : String(ids.length)}
          suffix={inventory.data && !inventory.isError ? `/ ${agents.length}` : undefined}
          detail={
            inventory.isPending
              ? t('Loading data')
              : inventory.isError
                ? t('No recent data')
                : t('{{value1}} offline', { value1: agents.length - ids.length })
          }
          icon={<Server />}
        >
          <Tooltip title={t('Current agent connections')}>
            <Box sx={{ width: 82 }}>
              <LinearProgress
                aria-label={t('Current agent connections')}
                variant="determinate"
                value={agents.length ? (ids.length / agents.length) * 100 : 0}
                sx={{ height: 5, borderRadius: 1 }}
              />
            </Box>
          </Tooltip>
        </Stat>
        <Paper variant="outlined">
          <MonitorSummary range={range} />
        </Paper>
        <Stat
          label={t('Average CPU load')}
          value={number(cpu.value)}
          suffix={cpu.value === null ? undefined : '%'}
          detail={coverage(cpu.count)}
          icon={<Cpu />}
        >
          {trend('cpu')}
        </Stat>
        <Stat
          label={t('Average memory usage')}
          value={number(memory.value)}
          suffix={memory.value === null ? undefined : '%'}
          detail={coverage(memory.count)}
          icon={<Memory />}
        >
          {trend('memory')}
        </Stat>
      </Box>
      <Box className="overview-panel-row">
        <Panel title={t('Fleet activity')} subtitle={t('CPU usage · Fleet average')}>
          {history.isError ? (
            <Box sx={{ p: 3 }}>
              <ErrorState error={history.error} retry={() => void history.refetch()} />
            </Box>
          ) : !hasHistory ? (
            <Typography
              sx={{ p: 3, minHeight: 260 }}
              color="text.secondary"
              aria-busy={history.isPending}
            >
              {history.isPending || inventory.isPending
                ? t('Loading data')
                : t('No complete fleet measurements in this period.')}
            </Typography>
          ) : (
            <Box sx={{ px: 1.5, pt: 2 }}>
              <LineChart
                height={260}
                title={t('CPU usage · Fleet average')}
                skipAnimation
                localeText={
                  i18n.resolvedLanguage === 'fr'
                    ? { ...frFRLocalText, a11yNoValue: t('No value') }
                    : undefined
                }
                grid={{ horizontal: true }}
                xAxis={[
                  {
                    data: points.map((point) => new Date(point.timestamp)),
                    scaleType: 'time',
                    tickNumber: 4,
                    valueFormatter: (value: Date) =>
                      formatDate(
                        value,
                        range === '7d'
                          ? { month: 'short', day: 'numeric' }
                          : { hour: '2-digit', minute: '2-digit' },
                      ),
                  },
                ]}
                yAxis={[
                  { min: 0, max: 100, width: 50, valueFormatter: (value: number) => `${value}%` },
                ]}
                series={[
                  {
                    id: 'current',
                    data: points.map((point) => point.value),
                    label: t('Selected period'),
                    color: '#557caf',
                    area: true,
                    curve: 'linear',
                    showMark: points.filter((point) => point.value !== null).length === 1,
                    connectNulls: false,
                    valueFormatter: (value) =>
                      value === null ? t('No value') : formatPercent(value),
                  },
                  {
                    id: 'previous',
                    data: points.map((point) => point.previous),
                    label: t('Previous period'),
                    color: '#91a9c6',
                    curve: 'linear',
                    showMark: points.filter((point) => point.previous !== null).length === 1,
                    connectNulls: false,
                    valueFormatter: (value) =>
                      value === null ? t('No value') : formatPercent(value),
                  },
                ]}
              />
            </Box>
          )}
          <Typography
            variant="caption"
            color="text.secondary"
            sx={{ display: 'block', px: 3, pb: 2 }}
          >
            {t('Currently connected agents · gaps indicate incomplete measurements.')}
          </Typography>
        </Panel>
        <Panel
          title={t('Resource distribution')}
          subtitle={t('Live measurements from connected agents')}
          action={<Cpu />}
        >
          <Stack className="overview-resource-content" spacing={2.5} sx={{ p: 3 }}>
            {[
              { label: t('CPU used'), metric: cpu },
              { label: t('Memory'), metric: memory },
              { label: t('Most used filesystem'), metric: disk },
            ].map(({ label, metric }) => (
              <Stack key={label} spacing={0.5}>
                <Usage value={metric.value} label={label} />
                <Typography variant="caption" color="text.secondary">
                  {coverage(metric.count)}
                </Typography>
              </Stack>
            ))}
            <Typography variant="caption" color="text.secondary">
              {t('{{cores}} logical CPUs · {{ram}} GiB RAM', {
                cores: cores.count === ids.length ? number(cores.value) : '—',
                ram:
                  ram.count === ids.length
                    ? number(ram.value === null ? null : ram.value / 1024 ** 3)
                    : '—',
              })}
            </Typography>
          </Stack>
        </Panel>
      </Box>
      <Box className="overview-panel-row overview-operations">
        <Panel
          title={t('Your agents')}
          subtitle={t('The pulse of your machines')}
          action={
            <AppLink to="/agents" search={{ q: '', status: 'all' }}>
              {t('All agents')}
              <ArrowUpRight />
            </AppLink>
          }
        >
          <Box className="overview-agent-table" sx={{ width: '100%', overflowX: 'auto' }}>
            <Table
              size="small"
              aria-label={t('Your agents')}
              sx={{
                height: agents.length >= 5 ? { lg: '100%' } : undefined,
                '& .MuiTableCell-root': { px: { xs: 1.5, sm: 2 } },
                '& .MuiLinearProgress-root': { display: { xs: 'none', sm: 'block' } },
                '& .MuiTypography-caption': { whiteSpace: 'nowrap' },
              }}
            >
              <TableHead>
                <TableRow>
                  <TableCell>{t('Agent')}</TableCell>
                  <TableCell>{t('Status')}</TableCell>
                  <TableCell>{t('CPU')}</TableCell>
                  <TableCell>{t('Memory')}</TableCell>
                </TableRow>
              </TableHead>
              <TableBody>
                {agents.slice(0, 5).map((agent) => (
                  <TableRow key={agent.instance_uid}>
                    <TableCell sx={{ maxWidth: 180, overflowWrap: 'anywhere' }}>
                      <AppLink to="/agents/$agentId" params={{ agentId: agent.instance_uid }}>
                        <Typography component="span" variant="subtitle2">
                          {agent.hostname || agent.instance_uid}
                        </Typography>
                      </AppLink>
                      <Typography
                        variant="caption"
                        color="text.secondary"
                        sx={{ display: 'block' }}
                      >
                        {agent.connected
                          ? agent.version || '—'
                          : agent.last_seen_at
                            ? relativeTime(agent.last_seen_at)
                            : t('No recent data')}
                      </Typography>
                    </TableCell>
                    <TableCell>
                      <Badge tone={agent.connected ? 'good' : 'neutral'}>
                        {agent.connected ? t('Connected') : t('Offline')}
                      </Badge>
                    </TableCell>
                    <TableCell>
                      <Usage value={readings[agent.instance_uid]?.cpu ?? null} />
                    </TableCell>
                    <TableCell>
                      <Usage value={readings[agent.instance_uid]?.memory ?? null} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </Box>
          <Typography
            className="overview-agent-footer"
            sx={{ p: 2 }}
            color="text.secondary"
            variant="body2"
          >
            {inventory.isPending
              ? t('Loading data')
              : inventory.isError
                ? t('No recent data')
                : agents.length
                  ? t('{{count}} agent in your services', { count: agents.length })
                  : t('No agents connected yet.')}
          </Typography>
        </Panel>
        <OverviewAlerting />
      </Box>
      <Panel
        title={t('Monitors')}
        subtitle={t('Latest measurements from your agents')}
        action={
          <AppLink to="/monitors" search={{ q: '', status: 'all', create: false }}>
            {t('Explore monitors')}
            <ArrowRight />
          </AppLink>
        }
      >
        <MonitorList limit={4} />
      </Panel>
    </>
  );
}

function number(value: number | null) {
  return value === null ? '—' : formatNumber(value, { maximumFractionDigits: 0 });
}

function Usage({ value, label }: { value: number | null; label?: string }) {
  const { t } = useTranslation();
  return value === null ? (
    <Stack direction="row" sx={{ justifyContent: 'space-between', gap: 1 }}>
      {label && <Typography variant="body2">{label}</Typography>}
      <Tooltip title={t('No recent data')}>
        <Typography component="span" color="text.secondary">
          —
        </Typography>
      </Tooltip>
    </Stack>
  ) : (
    <Progress value={Math.round(value)} label={label} />
  );
}

function Stat({
  label,
  value,
  suffix,
  detail,
  icon,
  children,
}: {
  label: string;
  value: string;
  suffix?: string;
  detail: string;
  icon: ReactNode;
  children: ReactNode;
}) {
  return (
    <Paper variant="outlined" sx={{ p: 3 }}>
      <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1 }}>
        <Typography>{label}</Typography>
        {icon}
      </Stack>
      <Stack
        direction="row"
        sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1, my: 2 }}
      >
        <Typography component="strong" variant="h4" sx={{ whiteSpace: 'nowrap' }}>
          {value}
          <Typography component="span" variant="h6" color="text.secondary" sx={{ ml: 0.5 }}>
            {suffix}
          </Typography>
        </Typography>
        {children}
      </Stack>
      <Typography>{detail}</Typography>
    </Paper>
  );
}

function FleetTrend({ points, label }: { points: FleetPoint[]; label: string }) {
  const { t } = useTranslation();
  const hasData = points.some((point) => point.value !== null);
  const description = hasData ? label : t('No complete fleet measurements in this period.');
  return (
    <Tooltip title={description}>
      <Box
        role="img"
        aria-label={description}
        tabIndex={0}
        sx={{ width: 82, height: 35, flexShrink: 0, display: 'grid', placeItems: 'center' }}
      >
        {hasData ? (
          <Box aria-hidden="true" inert>
            <LineChart
              width={82}
              height={35}
              margin={{ left: 2, right: 2, top: 4, bottom: 4 }}
              xAxis={[
                {
                  data: points.map((point) => point.timestamp),
                  scaleType: 'linear',
                  position: 'none',
                },
              ]}
              yAxis={[{ position: 'none' }]}
              series={[
                {
                  data: points.map((point) => point.value),
                  curve: 'linear',
                  connectNulls: false,
                  showMark: points.filter((point) => point.value !== null).length === 1,
                },
              ]}
              hideLegend
              skipAnimation
              disableAxisListener
            />
          </Box>
        ) : (
          <Typography color="text.secondary">—</Typography>
        )}
      </Box>
    </Tooltip>
  );
}
