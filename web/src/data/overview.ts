import { queryOptions } from '@tanstack/react-query';
import { api } from './api';
import type { Range } from './time-range';

const fields = ['cpu', 'memory', 'disk', 'cores', 'ram'] as const;
type FleetMetric = (typeof fields)[number];
type Reading = Record<FleetMetric, number | null>;
type FleetReadings = Record<string, Reading>;
export type FleetPoint = { timestamp: number; value: number | null; previous: number | null };
const durations = { '1h': 3600, '6h': 21600, '24h': 86400, '7d': 604800 };

function instances(ids: string[]) {
  return [...new Set(ids)].sort();
}

function expressions(ids: string[]) {
  const pattern = instances(ids)
    .map((id) => id.replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
    .join('|');
  const labels = `job="arveld-agent",instance=~${JSON.stringify(pattern)}`;
  const totalCPU = `system_cpu_time_seconds_total{${labels}}`;
  const idle = `system_cpu_time_seconds_total{${labels},state="idle"}`;
  const cpu = `clamp(100 * (1 - sum by(instance)(rate(${idle}[2m])) / sum by(instance)(rate(${totalCPU}[2m]))), 0, 100) and (time() - max by(instance)(timestamp(${totalCPU})) < 60)`;
  const available = `system_linux_memory_available_bytes{${labels}}`;
  const total = `system_memory_limit_bytes{${labels}}`;
  const memory = [
    `100 * (1 - ${available} / (${total} > 0))`,
    `(${available} >= 0)`,
    `(${available} <= ${total})`,
    `(${total} < +Inf)`,
    `(time() - timestamp(${available}) < 60)`,
    `(time() - timestamp(${total}) < 60)`,
  ].join(' and ');
  const used = `system_filesystem_usage_bytes{${labels},mode="rw",state="used"}`;
  const free = `system_filesystem_usage_bytes{${labels},mode="rw",state="free"}`;
  const capacity = `(${used} + ignoring(state) ${free})`;
  const disk = [
    `100 * (${used} / ignoring(state) (${capacity} > 0))`,
    `(${used} >= 0)`,
    `(${free} >= 0)`,
    `(${capacity} < +Inf)`,
    `(time() - timestamp(${used}) < 60)`,
    `(time() - timestamp(${free}) < 60)`,
  ].join(' and ignoring(state) ');
  const cores = `system_cpu_time_seconds_total{${labels},state="idle",cpu!=""}`;
  return {
    cpu,
    memory: `max by(instance)(${memory})`,
    disk: `max by(instance)(${disk})`,
    // The collector exports CPU counters per logical processor, not a core-count gauge.
    cores: `count by(instance)(count by(instance,cpu)(${cores} and (time() - timestamp(${cores}) < 60)))`,
    ram: `max by(instance)((${total} > 0) and (${total} < +Inf) and (time() - timestamp(${total}) < 60))`,
  };
}

export function overviewExpression(ids: string[], history = false) {
  const metrics = expressions(ids);
  return (history ? (['cpu', 'memory'] as const) : fields)
    .map((field) => {
      const expression = metrics[field];
      // Historical comparisons use the same current fleet and leave gaps when coverage changes.
      const value = history
        ? `avg(${expression}) and (count(${expression}) == ${instances(ids).length})`
        : expression;
      return `label_replace((${value}), "arveld_overview_metric", "${field}", "", "")`;
    })
    .join(' or ');
}

export function summarizeFleet(readings: FleetReadings, field: FleetMetric) {
  const values = Object.values(readings).flatMap((reading) =>
    reading[field] === null ? [] : [reading[field]],
  );
  const count = values.length;
  const sum = values.reduce((total, value) => total + value, 0);
  return {
    value: !count
      ? null
      : field === 'disk'
        ? Math.max(...values)
        : field === 'cores' || field === 'ram'
          ? sum
          : sum / count,
    count,
  };
}

export function overviewMetricsOptions(agentIds: string[]) {
  const ids = instances(agentIds);
  return queryOptions({
    queryKey: ['overview-metrics', ids],
    queryFn: async ({ signal }): Promise<FleetReadings> => {
      const readings: FleetReadings = Object.fromEntries(
        ids.map((id) => [id, { cpu: null, memory: null, disk: null, cores: null, ram: null }]),
      );
      if (!ids.length) return readings;
      const result = await api.queryMetrics(overviewExpression(ids), signal);
      for (const sample of result) {
        const reading = Object.hasOwn(readings, sample.metric.instance)
          ? readings[sample.metric.instance]
          : undefined;
        const field = fields.find((field) => field === sample.metric.arveld_overview_metric);
        const value = sample.value[1];
        if (
          reading &&
          field &&
          value !== null &&
          Number.isFinite(value) &&
          value >= 0 &&
          (field === 'cores' || field === 'ram' || value <= 100)
        )
          reading[field] = value;
      }
      return readings;
    },
    retry: false,
    refetchInterval: 15000,
    refetchOnWindowFocus: true,
  });
}

export function overviewHistoryOptions(agentIds: string[], range: Range) {
  const ids = instances(agentIds);
  return queryOptions({
    queryKey: ['overview-history', ids, range],
    queryFn: async ({ signal }): Promise<Record<'cpu' | 'memory', FleetPoint[]>> => {
      if (!ids.length) return { cpu: [], memory: [] };
      const clock = await api.queryMetrics('vector(time())', signal);
      const serverTime = clock[0]?.value[1];
      if (serverTime === null || serverTime === undefined || !Number.isFinite(serverTime))
        throw new Error('Arveld returned an unexpected response.');
      const duration = durations[range];
      const step = duration / 240;
      const end = Math.floor(serverTime / step) * step;
      const start = end - duration;
      const result = await api.queryMetricsRange(
        overviewExpression(ids, true),
        { start: start - duration, end, step },
        signal,
      );
      function points(field: 'cpu' | 'memory'): FleetPoint[] {
        const samples = new Map(
          result.find((series) => series.metric.arveld_overview_metric === field)?.values ?? [],
        );
        return Array.from({ length: 241 }, (_, index) => {
          const timestamp = start + index * step;
          return {
            timestamp: timestamp * 1000,
            value: samples.get(timestamp) ?? null,
            previous: samples.get(timestamp - duration) ?? null,
          };
        });
      }
      return { cpu: points('cpu'), memory: points('memory') };
    },
    retry: false,
    refetchInterval: Math.max(15000, (durations[range] / 240) * 1000),
    refetchOnWindowFocus: true,
  });
}
