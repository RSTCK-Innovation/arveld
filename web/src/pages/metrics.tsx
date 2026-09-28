import Play from '@mui/icons-material/PlayArrow';
import ChartNoAxesCombined from '@mui/icons-material/ShowChart';
import Table2 from '@mui/icons-material/TableChartOutlined';
import {
  Autocomplete,
  createFilterOptions,
  TableBody,
  TableCell,
  TableHead,
  TablePagination,
  TableRow,
  TextField,
  ToggleButton,
} from '@mui/material';
import Box from '@mui/material/Box';
import Table from '@mui/material/Table';
import Typography from '@mui/material/Typography';
import { useQuery } from '@tanstack/react-query';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { MetricChart } from '../components/charts';
import {
  Button,
  ChartLoading,
  download,
  ErrorState,
  ExportButton,
  PageHeading,
  Panel,
  RangeSelect,
} from '../components/ui';
import { metricLabels, metricSuggestions, metricsCSV, metricsOptions } from '../data/metrics';
import type { Range } from '../data/time-range';
import { formatDate, formatNumber } from '../i18n/format';

export function MetricsPage() {
  const { t } = useTranslation();
  const [expression, setExpression] = useState('');
  const [range, setRange] = useState<Range>('1h');
  const [view, setView] = useState('chart');
  const [page, setPage] = useState(0);
  const [request, setRequest] = useState({ expression: '', range, run: 0 });
  const result = useQuery({
    ...metricsOptions(request.expression, request.range, request.run),
    enabled: request.run > 0,
  });
  const series = result.isSuccess ? result.data.series : [];
  const rows = series
    .flatMap((entry) => {
      const labels = metricLabels(entry.metric);
      return entry.values.map(([timestamp, value]) => ({ labels, timestamp, value }));
    })
    .sort(
      (left, right) => right.timestamp - left.timestamp || left.labels.localeCompare(right.labels),
    );
  const dirty =
    request.run > 0 && (request.expression !== expression.trim() || request.range !== range);
  return (
    <>
      <PageHeading
        eyebrow={t('METRICS EXPLORER')}
        title={t('Your data has a story to tell.')}
        description={t(
          "Explore your services' resources and discover the detail behind each chart.",
        )}
        actions={<RangeSelect value={range} onChange={setRange} />}
      />
      <Panel title={t('Your query')} subtitle={t('Write PromQL or choose an editable suggestion.')}>
        <Box sx={{ p: 3 }}>
          <Box
            component="form"
            onSubmit={(event) => {
              event.preventDefault();
              if (!expression.trim()) return;
              setPage(0);
              setRequest({ expression: expression.trim(), range, run: request.run + 1 });
            }}
            sx={{
              display: 'grid',
              gap: 2,
              alignItems: 'start',
              gridTemplateColumns: { xs: '1fr', lg: 'minmax(0,1fr) auto' },
            }}
          >
            <Autocomplete
              freeSolo
              autoComplete
              openOnFocus
              options={metricSuggestions.map((suggestion) => suggestion.expression)}
              inputValue={expression}
              onInputChange={(_event, value) => setExpression(value)}
              filterOptions={createFilterOptions({
                stringify: (value: string) =>
                  `${value} ${t(metricSuggestions.find((suggestion) => suggestion.expression === value)?.label ?? '')}`,
              })}
              renderOption={({ key, ...props }, option) => (
                <Box
                  component="li"
                  key={key}
                  {...props}
                  sx={{
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'flex-start !important',
                    overflowWrap: 'anywhere',
                  }}
                >
                  <Typography variant="body2">
                    {t(
                      metricSuggestions.find((suggestion) => suggestion.expression === option)
                        ?.label ?? '',
                    )}
                  </Typography>
                  <Typography component="code" variant="caption" color="text.secondary">
                    {option}
                  </Typography>
                </Box>
              )}
              renderInput={(params) => (
                <TextField
                  {...params}
                  label={t('PromQL expression')}
                  helperText={t(
                    'Suggestions are examples; available metrics depend on your Agents and Monitors.',
                  )}
                  slotProps={{
                    ...params.slotProps,
                    htmlInput: { ...params.slotProps.htmlInput, spellCheck: false },
                  }}
                />
              )}
            />
            <Button
              startIcon={<Play />}
              type="submit"
              variant="primary"
              busy={result.isFetching}
              disabled={!expression.trim()}
            >
              {t('Run')}
            </Button>
          </Box>
          {dirty && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mt: 2 }}>
              {t('Changes to run')}
            </Typography>
          )}
        </Box>
      </Panel>
      <Panel
        title={t('Query result')}
        action={
          series.length > 0 ? (
            <Box sx={{ display: 'flex', alignItems: 'center', flexWrap: 'wrap', gap: 1 }}>
              <ToggleButton
                aria-label={t('Chart view')}
                selected={view === 'chart'}
                value="chart"
                onClick={() => setView('chart')}
              >
                <ChartNoAxesCombined />
              </ToggleButton>
              <ToggleButton
                aria-label={t('Table view')}
                selected={view === 'table'}
                value="table"
                onClick={() => setView('table')}
              >
                <Table2 />
              </ToggleButton>
              <ExportButton
                onClick={() =>
                  download(
                    `arveld-metrics-${request.range}.csv`,
                    metricsCSV(series),
                    'text/csv;charset=utf-8',
                  )
                }
              />
            </Box>
          ) : undefined
        }
      >
        {request.run === 0 ? (
          <Typography sx={{ p: 3 }}>
            {t('Run a PromQL expression to explore your metrics.')}
          </Typography>
        ) : result.isPending || result.isFetching ? (
          <ChartLoading />
        ) : result.isError ? (
          <ErrorState
            error={result.error}
            retry={() => {
              setPage(0);
              void result.refetch();
            }}
          />
        ) : (
          <>
            <Box sx={{ p: 3, overflowWrap: 'anywhere' }}>
              <Typography component="code" variant="body2">
                {request.expression}
              </Typography>
              <Typography variant="body2" color="text.secondary" sx={{ mt: 1 }}>
                {t('Returned series: {{count}}', { count: series.length })} ·{' '}
                {formatDate(result.data.start * 1000, { dateStyle: 'short', timeStyle: 'medium' })}{' '}
                – {formatDate(result.data.end * 1000, { dateStyle: 'short', timeStyle: 'medium' })}
              </Typography>
            </Box>
            {!series.length ? (
              <Typography sx={{ px: 3, pb: 3 }}>
                {t('No series returned for this query and period.')}
              </Typography>
            ) : view === 'chart' ? (
              <MetricChart series={series} range={request.range} height={380} />
            ) : (
              <>
                <Box sx={{ width: '100%', maxHeight: 480, overflow: 'auto' }}>
                  <Table aria-label={t('Query result')}>
                    <TableHead>
                      <TableRow>
                        <TableCell>{t('Timestamp')}</TableCell>
                        <TableCell>{t('Labels')}</TableCell>
                        <TableCell>{t('Value')}</TableCell>
                      </TableRow>
                    </TableHead>
                    <TableBody>
                      {rows.slice(page * 25, (page + 1) * 25).map((row) => (
                        <TableRow key={`${row.labels}:${row.timestamp}`}>
                          <TableCell sx={{ whiteSpace: 'nowrap' }}>
                            {formatDate(row.timestamp * 1000, {
                              dateStyle: 'short',
                              timeStyle: 'medium',
                            })}
                          </TableCell>
                          <TableCell
                            sx={{ overflowWrap: 'anywhere', minWidth: 180, maxWidth: 420 }}
                          >
                            <code>{row.labels}</code>
                          </TableCell>
                          <TableCell>
                            {row.value === null
                              ? t('No value')
                              : formatNumber(row.value, { maximumSignificantDigits: 15 })}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </Box>
                <TablePagination
                  component="div"
                  count={rows.length}
                  page={page}
                  onPageChange={(_event, next) => setPage(next)}
                  rowsPerPage={25}
                  rowsPerPageOptions={[25]}
                  sx={{ overflowX: 'auto' }}
                />
              </>
            )}
            {series.length > 0 && (
              <Typography
                variant="caption"
                color="text.secondary"
                sx={{ display: 'block', px: 3, pb: 3 }}
              >
                {t(
                  'Values use the units of your expression. Missing or non-finite samples remain gaps.',
                )}
              </Typography>
            )}
          </>
        )}
      </Panel>
    </>
  );
}
