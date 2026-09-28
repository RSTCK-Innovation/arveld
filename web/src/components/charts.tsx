import { Tooltip, Typography } from '@mui/material';
import Box from '@mui/material/Box';
import { ChartsTooltipContainer, useItemTooltip } from '@mui/x-charts/ChartsTooltip';
import { useXAxis } from '@mui/x-charts/hooks';
import { LineChart } from '@mui/x-charts/LineChart';
import { frFRLocalText } from '@mui/x-charts/locales';
import { useTranslation } from 'react-i18next';
import { type MetricSeries, metricLabels } from '../data/metrics';
import type { UptimePeriod } from '../data/monitor-results';
import type { Range } from '../data/time-range';
import { formatDate, formatNumber } from '../i18n/format';
export function MetricChart({
  series,
  range,
  height = 350,
}: {
  series: MetricSeries[];
  range: Range;
  height?: number;
}) {
  const { t, i18n } = useTranslation();
  return (
    <Box className="metric-chart" sx={{ minWidth: 0, px: 1.5, pb: 1.5 }}>
      <LineChart
        height={height}
        title={t('Query result')}
        localeText={
          i18n.resolvedLanguage === 'fr'
            ? { ...frFRLocalText, a11yNoValue: t('No value') }
            : undefined
        }
        skipAnimation
        experimentalFeatures={{ enablePositionBasedPointerInteraction: true }}
        grid={{ horizontal: true }}
        slots={{ tooltip: MetricTooltip }}
        slotProps={{
          tooltip: { trigger: 'item' },
          legend: {
            direction: 'horizontal',
            position: { vertical: 'top', horizontal: 'start' },
            sx: {
              maxHeight: 90,
              overflow: 'auto',
              flexWrap: 'wrap',
              '& .MuiChartsLegend-label': { whiteSpace: 'normal', overflowWrap: 'anywhere' },
            },
          },
        }}
        xAxis={[
          {
            data: series[0].values.map(([timestamp]) => new Date(timestamp * 1000)),
            scaleType: 'time',
            tickNumber: 4,
            valueFormatter: (value: Date) =>
              formatDate(
                value,
                range === '7d'
                  ? { day: '2-digit', month: '2-digit' }
                  : { hour: '2-digit', minute: '2-digit' },
              ),
          },
        ]}
        yAxis={[
          {
            width: 70,
            valueFormatter: (value: number) => formatNumber(value, { notation: 'compact' }),
          },
        ]}
        series={series.map((entry) => ({
          id: metricLabels(entry.metric),
          label: metricLabels(entry.metric),
          data: entry.values.map(([, value]) => value),
          showMark: ({ index }) =>
            entry.values[index - 1]?.[1] == null && entry.values[index + 1]?.[1] == null,
          connectNulls: false,
          curve: 'linear',
          valueFormatter: (value) =>
            value === null ? t('No value') : formatNumber(value, { maximumSignificantDigits: 15 }),
        }))}
      />
    </Box>
  );
}

function MetricTooltip() {
  const item = useItemTooltip<'line'>();
  const axis = useXAxis();
  const timestamp =
    item?.identifier.dataIndex === undefined ? undefined : axis.data?.[item.identifier.dataIndex];
  return (
    <ChartsTooltipContainer trigger="item">
      {item && timestamp !== undefined && (
        <Box
          sx={{
            width: 'min(400px, calc(100vw - 32px))',
            maxHeight: 'calc(100vh - 32px)',
            overflow: 'hidden',
            p: 1.5,
            bgcolor: 'background.paper',
            border: 1,
            borderColor: 'divider',
            borderRadius: 1,
            boxShadow: 3,
          }}
        >
          <Typography variant="caption" color="text.secondary">
            {formatDate(timestamp, { dateStyle: 'short', timeStyle: 'medium' })}
          </Typography>
          <Box sx={{ display: 'flex', alignItems: 'flex-start', gap: 1, my: 1 }}>
            <Box
              component="span"
              sx={{
                width: 10,
                height: 10,
                mt: 0.5,
                flexShrink: 0,
                bgcolor: item.color,
                borderRadius: '50%',
              }}
            />
            <Typography
              component="code"
              variant="caption"
              sx={{
                minWidth: 0,
                overflowWrap: 'anywhere',
                display: '-webkit-box',
                WebkitBoxOrient: 'vertical',
                WebkitLineClamp: 4,
                overflow: 'hidden',
              }}
            >
              {item.label}
            </Typography>
          </Box>
          <Typography variant="body2" sx={{ fontWeight: 600, overflowWrap: 'anywhere' }}>
            {item.formattedValue}
          </Typography>
        </Box>
      )}
    </ChartsTooltipContainer>
  );
}

export function UptimeBars({ periods }: { periods: UptimePeriod[] }) {
  const { t } = useTranslation();
  return (
    <Box
      role="group"
      className="uptime-bars"
      aria-label={t('Monitor history: last hour, 40 periods of 90 seconds')}
      sx={{
        display: 'flex',
        height: 24,
        gap: 0.25,
        minWidth: 0,
        width: '100%',
        border: 0,
        p: 0,
        m: 0,
      }}
    >
      {periods.map((period, i) => {
        const status =
          period.status === 'success'
            ? t('Successful')
            : period.status === 'failure'
              ? t('Failure')
              : t('Missing measurements');
        const time =
          period.start !== null && period.end !== null
            ? `${formatDate(period.start, { hour: '2-digit', minute: '2-digit', second: '2-digit' })} – ${formatDate(period.end, { hour: '2-digit', minute: '2-digit', second: '2-digit' })}`
            : t('No measurements in the last hour.');
        const label = `${time} · ${status}`;
        return (
          <Tooltip
            describeChild
            // biome-ignore lint/suspicious/noArrayIndexKey: Samples are fixed positions in this history.
            key={i}
            title={label}
          >
            <Box
              component="span"
              sx={{
                flex: 1,
                minWidth: 0,
                bgcolor:
                  period.status === 'failure'
                    ? 'error.light'
                    : period.status === 'success'
                      ? 'success.light'
                      : 'action.disabled',
                borderRadius: 0.25,
                '&:focus-visible': {
                  outline: '2px solid',
                  outlineColor: 'primary.main',
                  outlineOffset: 2,
                },
              }}
              tabIndex={0}
              role="img"
              aria-label={label}
            />
          </Tooltip>
        );
      })}
    </Box>
  );
}
