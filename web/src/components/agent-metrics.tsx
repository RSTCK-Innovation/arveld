import Box from '@mui/material/Box';
import Tooltip from '@mui/material/Tooltip';
import Typography from '@mui/material/Typography';
import { Gauge, gaugeClasses, useGaugeState } from '@mui/x-charts/Gauge';
import { LineChart } from '@mui/x-charts/LineChart';
import { frFRLocalText } from '@mui/x-charts/locales';
import { useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import {
  type AgentHistoryMetric,
  type AgentMetric,
  agentMetricHistoryOptions,
  agentMetricOptions,
  agentUptimeOptions,
} from '../data/agent-metrics';
import {
  formatDate,
  formatDuration,
  formatNetworkRate,
  formatNumber,
  formatPercent,
} from '../i18n/format';
import { ErrorState, Panel } from './ui';

export function AgentUptime({ instanceUID }: { instanceUID: string }) {
  const { t } = useTranslation();
  const query = useQuery(agentUptimeOptions(instanceUID));
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  return (
    <Box aria-busy={query.isPending} sx={{ py: 2 }}>
      <Typography variant="h5" component="p" sx={{ my: 2 }}>
        {query.isPending
          ? t('Loading data')
          : query.data === null
            ? t('Unavailable')
            : formatDuration(query.data)}
      </Typography>
      <Typography variant="body2" color="text.secondary">
        {t('Since the host last started')}
      </Typography>
    </Box>
  );
}

export function AgentMetricValue({
  metric,
  instanceUID,
}: {
  metric: AgentMetric;
  instanceUID: string;
}) {
  const { t } = useTranslation();
  const query = useQuery(agentMetricOptions(metric, instanceUID));
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  const filesystem =
    metric === 'disk' ? query.data?.metric.mountpoint || query.data?.metric.device : undefined;
  return (
    <Box aria-busy={query.isPending}>
      {!query.isPending && query.data !== null ? (
        <Tooltip
          describeChild
          title={
            metric === 'disk' ? (
              <>
                <div>{t('Most used writable filesystem')}</div>
                <Box sx={{ overflowWrap: 'anywhere' }}>
                  {[query.data.metric.mountpoint, query.data.metric.device]
                    .filter(Boolean)
                    .join(' · ')}
                </Box>
              </>
            ) : (
              ''
            )
          }
        >
          <Box
            role="meter"
            tabIndex={metric === 'disk' ? 0 : undefined}
            aria-label={
              metric === 'cpu'
                ? t('CPU usage')
                : metric === 'memory'
                  ? t('Memory usage')
                  : t('Disk usage')
            }
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={query.data.value}
            aria-valuetext={
              filesystem
                ? `${filesystem}: ${formatPercent(query.data.value)}`
                : formatPercent(query.data.value)
            }
          >
            <Gauge
              skipAnimation
              height={145}
              value={query.data.value}
              valueMin={0}
              valueMax={100}
              startAngle={-90}
              endAngle={90}
              innerRadius="80%"
              cornerRadius="30%"
              text={formatPercent(query.data.value)}
              sx={{
                [`& .${gaugeClasses.valueArc}`]: { fill: 'primary.main' },
                [`& .${gaugeClasses.referenceArc}`]: { fill: 'action.hover' },
                [`& .${gaugeClasses.valueText}`]: { fontSize: 26, fontWeight: 600 },
              }}
            >
              {filesystem && <FilesystemGaugeLabel name={filesystem} />}
            </Gauge>
          </Box>
        </Tooltip>
      ) : (
        <Typography component="p" variant="h6" sx={{ my: 2 }}>
          {query.isPending ? t('Loading data') : t('Unavailable')}
        </Typography>
      )}
      {!query.isPending && query.data === null && (
        <Typography variant="body2" color="text.secondary">
          {metric === 'cpu'
            ? t('Waiting for recent CPU measurements.')
            : metric === 'memory'
              ? t('Waiting for recent memory measurements.')
              : t('Waiting for recent disk measurements.')}
        </Typography>
      )}
    </Box>
  );
}

function FilesystemGaugeLabel({ name }: { name: string }) {
  const { cx, cy, innerRadius } = useGaugeState();
  const width = innerRadius * 1.5;
  return (
    <foreignObject x={cx - width / 2} y={cy - 46} width={width} height={22}>
      <Typography
        component="div"
        variant="caption"
        color="text.secondary"
        noWrap
        sx={{ textAlign: 'center' }}
      >
        {name}
      </Typography>
    </foreignObject>
  );
}

export function AgentMetricHistory({
  metric,
  instanceUID,
}: {
  metric: AgentHistoryMetric;
  instanceUID: string;
}) {
  const { t, i18n } = useTranslation();
  const query = useQuery(agentMetricHistoryOptions(metric, instanceUID));
  const hasMeasurements = query.data?.some((series) =>
    series.values.some((point) => point.value !== null),
  );
  return (
    <Panel
      title={
        metric === 'cpu'
          ? t('CPU usage')
          : metric === 'memory'
            ? t('Memory usage')
            : metric === 'network'
              ? t('Network traffic')
              : t('Disk usage')
      }
      subtitle={
        metric === 'disk'
          ? `${t('Last hour')} · ${t('Top 5 writable filesystems by latest usage')}`
          : metric === 'network'
            ? `${t('Last hour')} · ${t('Top 5 interfaces by peak traffic')}`
            : t('Last hour')
      }
    >
      {query.isError ? (
        <Box sx={{ p: 3 }}>
          <ErrorState error={query.error} retry={() => void query.refetch()} />
        </Box>
      ) : query.isPending || !hasMeasurements ? (
        <Typography sx={{ p: 3 }} color="text.secondary" aria-busy={query.isPending}>
          {query.isPending
            ? t('Loading data')
            : metric === 'cpu'
              ? t('No CPU measurements in the last hour.')
              : metric === 'memory'
                ? t('No memory measurements in the last hour.')
                : metric === 'network'
                  ? t('No network measurements in the last hour.')
                  : t('No disk measurements in the last hour.')}
        </Typography>
      ) : (
        <Box sx={{ minWidth: 0, px: 1.5, pb: 1.5 }}>
          <LineChart
            height={230}
            title={
              metric === 'cpu'
                ? t('CPU usage over the last hour')
                : metric === 'memory'
                  ? t('Memory usage over the last hour')
                  : metric === 'network'
                    ? t('Network traffic over the last hour')
                    : t('Disk usage over the last hour')
            }
            localeText={
              i18n.resolvedLanguage === 'fr'
                ? { ...frFRLocalText, a11yNoValue: t('No value') }
                : undefined
            }
            skipAnimation
            hideLegend={metric !== 'disk' && metric !== 'network'}
            slotProps={{
              legend: {
                direction: 'horizontal',
                position: { vertical: 'bottom', horizontal: 'start' },
                sx: {
                  flexWrap: 'wrap',
                  rowGap: 1,
                  maxWidth: '100%',
                  '& .MuiChartsLegend-label': { whiteSpace: 'normal', overflowWrap: 'anywhere' },
                },
              },
            }}
            grid={{ horizontal: true }}
            xAxis={[
              {
                data: query.data[0].values.map((point) => new Date(point.timestamp)),
                scaleType: 'time',
                valueFormatter: (value: Date) =>
                  formatDate(value, { hour: '2-digit', minute: '2-digit' }),
                tickNumber: 4,
              },
            ]}
            yAxis={[
              {
                min: 0,
                max: metric === 'network' ? undefined : 100,
                width: metric === 'network' ? 80 : 50,
                valueFormatter: (value: number) =>
                  metric === 'network' ? formatNetworkRate(value) : `${formatNumber(value)} %`,
              },
            ]}
            series={query.data.map((series) => ({
              id: JSON.stringify(
                Object.entries(series.metric).sort(([left], [right]) => left.localeCompare(right)),
              ),
              label:
                metric === 'cpu'
                  ? t('CPU usage')
                  : metric === 'memory'
                    ? t('Memory usage')
                    : metric === 'network'
                      ? `${series.metric.device} · ${series.metric.direction === 'receive' ? t('Receive traffic') : t('Transmit traffic')}`
                      : [series.metric.mountpoint, series.metric.device]
                          .filter(Boolean)
                          .join(' · ') || t('Disk usage'),
              data: series.values.map((point) => point.value),
              showMark: series.values.filter((point) => point.value !== null).length === 1,
              connectNulls: false,
              area: metric !== 'disk' && metric !== 'network',
              curve: 'linear',
              valueFormatter: (value) =>
                value === null
                  ? t('No value')
                  : metric === 'network'
                    ? formatNetworkRate(value)
                    : formatPercent(value),
            }))}
          />
          {metric === 'network' && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', px: 1.5 }}>
              {t('Received and sent per interface visible to this Agent.')}
            </Typography>
          )}
        </Box>
      )}
    </Panel>
  );
}
