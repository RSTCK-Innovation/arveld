import { queryOptions } from '@tanstack/react-query';
import { api } from './api';
import type { Range } from './time-range';

export const metricSuggestions = [
  {
    label: 'CPU time by core and state',
    expression: 'rate(system_cpu_time_seconds_total{job="arveld-agent"}[2m])',
  },
  { label: 'Total memory in bytes', expression: 'system_memory_limit_bytes{job="arveld-agent"}' },
  {
    label: 'Available memory in bytes',
    expression: 'system_linux_memory_available_bytes{job="arveld-agent"}',
  },
  {
    label: 'Filesystem usage in bytes',
    expression: 'system_filesystem_usage_bytes{job="arveld-agent"}',
  },
  {
    label: 'Network bytes per second',
    expression: 'rate(system_network_io_bytes_total{job="arveld-agent"}[2m])',
  },
  {
    label: 'HTTP Monitor latency in milliseconds',
    expression: 'httpcheck_duration_milliseconds{job="arveld-agent"}',
  },
  {
    label: 'TCP Monitor latency in milliseconds',
    expression: 'tcpcheck_duration_milliseconds{job="arveld-agent"}',
  },
  {
    label: 'DNS Monitor latency in milliseconds',
    expression: 'dnscheck_duration_milliseconds{job="arveld-agent"}',
  },
];

export type MetricSeries = Awaited<ReturnType<typeof api.queryMetricsRange>>[number];

export function metricLabels(metric: MetricSeries['metric']) {
  return JSON.stringify(
    Object.fromEntries(Object.entries(metric).sort(([a], [b]) => a.localeCompare(b))),
  );
}

export function metricsCSV(series: MetricSeries[]) {
  return [
    'timestamp,labels,value',
    ...series.flatMap((entry) => {
      const labels = `"${metricLabels(entry.metric).replaceAll('"', '""')}"`;
      return entry.values.map(
        ([time, value]) => `${new Date(time * 1000).toISOString()},${labels},${value ?? ''}`,
      );
    }),
  ].join('\n');
}

export function metricsOptions(expression: string, range: Range, run = 0) {
  return queryOptions({
    queryKey: ['metrics-explorer', expression, range, run],
    queryFn: async ({ signal }) => {
      const clock = await api.queryMetrics('vector(time())', signal);
      const serverTime = clock[0]?.value[1];
      if (serverTime === null || serverTime === undefined || !Number.isFinite(serverTime))
        throw new Error('Arveld returned an unexpected response.');
      const duration = { '1h': 3600, '6h': 21600, '24h': 86400, '7d': 604800 }[range];
      const step = duration / 240;
      const end = Math.floor(serverTime / step) * step;
      const start = end - duration;
      const result = await api.queryMetricsRange(expression, { start, end, step }, signal);
      return {
        start,
        end,
        series: result.map((series) => {
          const samples = new Map(series.values);
          return {
            metric: series.metric,
            values: Array.from({ length: 241 }, (_, index): [number, number | null] => {
              const timestamp = start + index * step;
              return [timestamp, samples.get(timestamp) ?? null];
            }),
          };
        }),
      };
    },
    retry: false,
    staleTime: 0,
    refetchOnWindowFocus: false,
    refetchOnReconnect: false,
  });
}

export const rangeLabels: Record<Range, string> = {
  '1h': 'Last hour',
  '6h': 'Last 6 hours',
  '24h': 'Last 24 hours',
  '7d': 'Last 7 days',
};
