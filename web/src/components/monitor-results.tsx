import { Box, Stack, Tooltip, Typography } from '@mui/material';
import { LineChart } from '@mui/x-charts/LineChart';
import { frFRLocalText } from '@mui/x-charts/locales';
import { type UseQueryResult, useQuery } from '@tanstack/react-query';
import { useTranslation } from 'react-i18next';
import type { Monitor } from '../data/api';
import { rangeLabels } from '../data/metrics';
import {
  healthyMonitorHistory,
  type MonitorHistoryData,
  type MonitorReading,
  monitorHistoriesOptions,
  monitorResultsOptions,
  monitorUptime,
} from '../data/monitor-results';
import { monitorsOptions } from '../data/monitors';
import type { Range } from '../data/time-range';
import { formatDate, formatNumber, formatPercent } from '../i18n/format';
import { errorMessage } from '../i18n/messages';
import { UptimeBars } from './charts';
import { Badge, Button, ErrorState, Panel } from './ui';

export function MonitorMeasurement({ monitor }: { monitor: Monitor }) {
  const query = useQuery(monitorResultsOptions([monitor]));
  return <MonitorResult monitor={monitor} query={query} detail />;
}

export function MonitorResult({
  monitor,
  query,
  detail = false,
}: {
  monitor: Monitor;
  query: UseQueryResult<Record<string, MonitorReading>>;
  detail?: boolean;
}) {
  const { t } = useTranslation();
  if (query.isError)
    return detail ? (
      <ErrorState error={query.error} retry={() => void query.refetch()} />
    ) : (
      <Stack role="alert" spacing={0.5} sx={{ alignItems: 'flex-start' }}>
        <Typography variant="caption" color="error.main">
          {errorMessage(query.error)}
        </Typography>
        <Button size="small" onClick={() => void query.refetch()}>
          {t('Retry')}
        </Button>
      </Stack>
    );
  const result = query.data?.[monitor.id];
  const status = result?.status ?? 'unknown';
  const statusLabel = query.isPending
    ? t('Loading data')
    : status === 'success'
      ? t('Successful')
      : status === 'failure'
        ? t('Failure')
        : monitor.protocol === 'http' && monitor.validations?.length && result?.measuredAt != null
          ? t('Validation unavailable')
          : t('No recent data');
  return (
    <Stack spacing={1} aria-busy={query.isPending} sx={{ minWidth: 0 }}>
      <Stack
        direction={detail ? 'row' : 'column-reverse'}
        sx={{
          alignItems: detail ? 'center' : 'flex-start',
          flexWrap: 'wrap',
          gap: detail ? 1.5 : 0.5,
          '& .MuiChip-root': { maxWidth: '100%', height: 'auto', minHeight: 24 },
          '& .MuiChip-label': { whiteSpace: 'normal', py: 0.25 },
        }}
      >
        <Badge tone={status === 'success' ? 'good' : status === 'failure' ? 'danger' : 'neutral'}>
          {statusLabel}
        </Badge>
        <Typography
          variant={detail ? 'h5' : 'subtitle2'}
          component="span"
          sx={{ whiteSpace: 'nowrap', fontVariantNumeric: 'tabular-nums' }}
        >
          {result?.latency !== null && result?.latency !== undefined
            ? `${formatNumber(result.latency)} ms`
            : '—'}
        </Typography>
      </Stack>
      {detail && (
        <>
          <Typography variant="body2" color="text.secondary">
            {result?.measuredAt !== null && result?.measuredAt !== undefined
              ? t('Measured at {{time}}', {
                  time: formatDate(result.measuredAt * 1000, {
                    hour: '2-digit',
                    minute: '2-digit',
                    second: '2-digit',
                  }),
                })
              : t('Waiting for a recent measurement from the agent.')}
          </Typography>
          {result?.loss !== null && result?.loss !== undefined && (
            <Typography variant="body2">
              {t('Packet loss: {{value}}', { value: formatPercent(result.loss) })}
            </Typography>
          )}
          <Typography variant="caption" color="text.secondary">
            {monitor.protocol === 'http'
              ? monitor.validations?.length
                ? t('Successful: HTTP 2xx or 3xx response and all validations passed.')
                : t('Successful: HTTP 2xx or 3xx response.')
              : monitor.protocol === 'tcp'
                ? t('Successful: TCP connection established.')
                : monitor.protocol === 'icmp'
                  ? t('Successful: no packets lost. Latency is the average round-trip time.')
                  : t('Successful: the DNS query completed without an error.')}
          </Typography>
        </>
      )}
    </Stack>
  );
}

export function MonitorUptime({
  monitor,
  query,
}: {
  monitor: Monitor;
  query: UseQueryResult<Record<string, MonitorHistoryData>>;
}) {
  const { t } = useTranslation();
  const uptime = monitorUptime(query.isError ? [] : (query.data?.[monitor.id]?.status ?? []));
  return (
    <Stack spacing={0.5} aria-busy={query.isPending}>
      <UptimeBars periods={uptime.periods} />
      {query.isError ? (
        <ErrorState error={query.error} retry={() => void query.refetch()} />
      ) : (
        <Stack direction="row" sx={{ justifyContent: 'space-between', gap: 1 }}>
          <Typography variant="caption" color="text.secondary">
            {query.isPending ? t('Loading data') : t('Last hour')}
          </Typography>
          <Tooltip title={t('Observed intervals without failures. Missing data is excluded.')}>
            <Typography
              variant="caption"
              color="text.secondary"
              tabIndex={0}
              sx={{ fontVariantNumeric: 'tabular-nums' }}
            >
              {uptime.successRate === null ? '—' : formatPercent(uptime.successRate)}
            </Typography>
          </Tooltip>
        </Stack>
      )}
    </Stack>
  );
}

export function MonitorHistory({ monitor }: { monitor: Monitor }) {
  const { t, i18n } = useTranslation();
  const query = useQuery(monitorHistoriesOptions([monitor]));
  return (
    <Box
      sx={{
        display: 'grid',
        gap: 3,
        mb: 3,
        gridTemplateColumns: { xs: 'minmax(0, 1fr)', lg: 'repeat(2, minmax(0, 1fr))' },
      }}
    >
      {(['status', 'latency'] as const).map((kind) => {
        const points = query.data?.[monitor.id]?.[kind] ?? [];
        const status = kind === 'status';
        const title = status ? t('Monitor status') : t('Latency');
        const hasData = points.some((point) => point.value !== null);
        return (
          <Panel key={kind} title={title} subtitle={t('Last hour')}>
            {query.isError ? (
              <Box sx={{ p: 3 }}>
                <ErrorState error={query.error} retry={() => void query.refetch()} />
              </Box>
            ) : query.isPending || !hasData ? (
              <Typography sx={{ p: 3 }} color="text.secondary" aria-busy={query.isPending}>
                {query.isPending ? t('Loading data') : t('No measurements in the last hour.')}
              </Typography>
            ) : (
              <Box sx={{ minWidth: 0, px: 1.5, pb: 1.5 }}>
                <LineChart
                  height={230}
                  title={title}
                  skipAnimation
                  hideLegend
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
                        formatDate(value, { hour: '2-digit', minute: '2-digit' }),
                    },
                  ]}
                  yAxis={[
                    {
                      min: 0,
                      max: status ? 1 : undefined,
                      width: status ? 86 : 55,
                      tickInterval: status ? [0, 1] : undefined,
                      valueFormatter: (value: number) =>
                        status
                          ? value === 1
                            ? t('Successful')
                            : t('Failure')
                          : `${formatNumber(value)} ms`,
                    },
                  ]}
                  series={[
                    {
                      data: points.map((point) => point.value),
                      label: title,
                      connectNulls: false,
                      showMark: points.filter((point) => point.value !== null).length === 1,
                      curve: status ? 'stepAfter' : 'linear',
                      area: !status,
                      valueFormatter: (value) =>
                        value === null
                          ? t('No value')
                          : status
                            ? value === 1
                              ? t('Successful')
                              : t('Failure')
                            : `${formatNumber(value)} ms`,
                    },
                  ]}
                />
              </Box>
            )}
          </Panel>
        );
      })}
    </Box>
  );
}

export function MonitorSummary({ range = '1h' }: { range?: Range }) {
  const { t } = useTranslation();
  const inventory = useQuery(monitorsOptions);
  const query = useQuery(monitorResultsOptions(inventory.data ?? []));
  const results = Object.values(query.data ?? {});
  if (inventory.isError)
    return <ErrorState error={inventory.error} retry={() => void inventory.refetch()} />;
  if (query.isError) return <ErrorState error={query.error} retry={() => void query.refetch()} />;
  const loading = inventory.isPending || query.isPending;
  const successful = results.filter((result) => result.status === 'success').length;
  const failed = results.filter((result) => result.status === 'failure').length;
  return (
    <Box className="monitor-summary" sx={{ p: 3 }} aria-busy={loading}>
      <Stack direction="row" sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1 }}>
        <Typography>{t('Healthy monitors')}</Typography>
        <Activity />
      </Stack>
      <Stack
        direction="row"
        sx={{ alignItems: 'center', justifyContent: 'space-between', gap: 1, my: 2 }}
      >
        <Typography component="strong" variant="h4" sx={{ whiteSpace: 'nowrap' }}>
          {loading ? '—' : successful}
          {!loading && (
            <>
              {' '}
              <Typography component="span" variant="h6" color="text.secondary">
                / {results.length}
              </Typography>
            </>
          )}
        </Typography>
        <MonitorSummaryTrend monitors={inventory.data ?? []} range={range} />
      </Stack>
      <Typography>
        {loading
          ? t('Loading data')
          : t('{{failed}} failed · {{unknown}} without recent data', {
              failed,
              unknown: results.length - successful - failed,
            })}
      </Typography>
    </Box>
  );
}

function MonitorSummaryTrend({ monitors, range }: { monitors: Monitor[]; range: Range }) {
  const { t } = useTranslation();
  const query = useQuery(monitorHistoriesOptions(monitors, range));
  const loading = query.isPending;
  const error = query.error;
  const points = healthyMonitorHistory(
    Object.values(query.data ?? {}).map((history) => history.status),
  );
  const hasData = !error && points.some((point) => point.value !== null);
  const label = loading
    ? t('Loading data')
    : error
      ? errorMessage(error)
      : hasData
        ? t('Healthy monitors · {{range}}', { range: t(rangeLabels[range]) })
        : t('No measurements · {{range}}', { range: t(rangeLabels[range]) });
  return (
    <Tooltip title={label}>
      <Box
        className="monitor-summary-trend"
        role="img"
        aria-label={label}
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
                  showMark: points.filter((point) => point.value !== null).length === 1,
                  connectNulls: false,
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

import Activity from '@mui/icons-material/MonitorHeartOutlined';
