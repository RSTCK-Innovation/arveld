import { queryOptions } from '@tanstack/react-query';
import { api } from './api';

export type AgentMetric = 'cpu' | 'memory' | 'disk';
export type AgentHistoryMetric = AgentMetric | 'network';

const step = 15;

export function cpuExpression(instanceUID: string) {
  const labels = `job="arveld-agent",instance=${JSON.stringify(instanceUID)}`;
  const total = `system_cpu_time_seconds_total{${labels}}`;
  const idle = `system_cpu_time_seconds_total{${labels},state="idle"}`;
  // Divide rates from the same window so a new agent does not appear busy
  // merely because it has less than two minutes of samples.
  const usage = `100 * (1 - sum(rate(${idle}[2m])) / sum(rate(${total}[2m])))`;
  return `clamp(${usage}, 0, 100) and (time() - max(timestamp(${total})) < 60)`;
}

export function memoryExpression(instanceUID: string) {
  const labels = `job="arveld-agent",instance=${JSON.stringify(instanceUID)}`;
  const available = `system_linux_memory_available_bytes{${labels}}`;
  const total = `system_memory_limit_bytes{${labels}}`;
  // Memory is a current amount, so divide the values directly rather than taking a rate.
  return [
    `100 * (1 - ${available} / (${total} > 0))`,
    `(${available} >= 0)`,
    `(${available} <= ${total})`,
    `(${total} < +Inf)`,
    `(time() - timestamp(${available}) < 60)`,
    `(time() - timestamp(${total}) < 60)`,
  ].join(' and ');
}

function filesystemUsageExpression(instanceUID: string) {
  const labels = `job="arveld-agent",instance=${JSON.stringify(instanceUID)},mode="rw"`;
  const used = `system_filesystem_usage_bytes{${labels},state="used"}`;
  const free = `system_filesystem_usage_bytes{${labels},state="free"}`;
  // Match states within each filesystem; reserved space is unavailable to applications.
  const capacity = `(${used} + ignoring(state) ${free})`;
  return [
    `100 * (${used} / ignoring(state) (${capacity} > 0))`,
    `(${used} >= 0)`,
    `(${free} >= 0)`,
    `(${capacity} < +Inf)`,
    `(time() - timestamp(${used}) < 60)`,
    `(time() - timestamp(${free}) < 60)`,
  ].join(' and ignoring(state) ');
}

export function diskExpression(instanceUID: string) {
  // Keep the winning filesystem's labels alongside its usage.
  return `topk(1, ${filesystemUsageExpression(instanceUID)})`;
}

export function diskHistoryExpression(instanceUID: string, end: number) {
  // Fix the selection at the end of the window so rank changes cannot add a sixth line.
  // Last valid values within the hour preserve history when the agent goes offline.
  const usage = filesystemUsageExpression(instanceUID);
  return `(${usage}) and topk(5, last_over_time((${usage})[1h:${step}s] @ ${end}))`;
}

export function networkExpression(instanceUID: string) {
  const labels = `job="arveld-agent",instance=${JSON.stringify(instanceUID)},device!="",device!~"lo|lo0",direction=~"receive|transmit"`;
  const bytes = `system_network_io_bytes_total{${labels}}`;
  const rate = `rate(${bytes}[2m])`;
  // Rate each counter before aggregation so resets cannot create traffic spikes.
  return `sum by (device, direction) ((${rate} >= 0) and (${rate} < +Inf) and (${bytes} >= 0) and (${bytes} < +Inf) and (time() - timestamp(${bytes}) < 60))`;
}

export function networkHistoryExpression(instanceUID: string, end: number) {
  const rates = networkExpression(instanceUID);
  // Select up to five interfaces once for the whole hour, retaining both directions.
  const busiest = `topk(5, max_over_time((sum by (device) (${rates}))[1h:${step}s] @ ${end}))`;
  return `(${rates}) and on (device) (${busiest})`;
}

const expressions = { cpu: cpuExpression, memory: memoryExpression, disk: diskExpression };

export function metricHistory(
  samples: [number, number | null][],
  start: number,
  end: number,
  samplingStep = step,
) {
  const values = new Map(samples);
  // Prometheus omits missing steps; explicit nulls keep the chart from joining gaps.
  return Array.from({ length: Math.floor((end - start) / samplingStep) + 1 }, (_, index) => {
    const timestamp = start + index * samplingStep;
    return { timestamp: timestamp * 1000, value: values.get(timestamp) ?? null };
  });
}

export function agentMetricOptions(metric: AgentMetric, instanceUID: string) {
  return queryOptions({
    queryKey: [`agent-${metric}`, instanceUID],
    queryFn: async ({ signal }) => {
      const result = await api.queryMetrics(expressions[metric](instanceUID), signal);
      const sample = result[0];
      if (!sample || sample.value[1] === null) return null;
      return { metric: sample.metric, value: sample.value[1] };
    },
    retry: false,
    refetchInterval: step * 1000,
    refetchOnWindowFocus: true,
  });
}

export function agentUptimeOptions(instanceUID: string) {
  const uptime = `system_uptime_seconds{job="arveld-agent",instance=${JSON.stringify(instanceUID)}}`;
  return queryOptions({
    queryKey: ['agent-uptime', instanceUID],
    queryFn: async ({ signal }) => {
      const result = await api.queryMetrics(
        `(${uptime} >= 0) and (${uptime} < +Inf) and (time() - timestamp(${uptime}) < 60)`,
        signal,
      );
      return result[0]?.value[1] ?? null;
    },
    retry: false,
    refetchInterval: step * 1000,
    refetchOnWindowFocus: true,
  });
}

export function agentMetricHistoryOptions(metric: AgentHistoryMetric, instanceUID: string) {
  return queryOptions({
    queryKey: [`agent-${metric}-history`, instanceUID],
    queryFn: async ({ signal }) => {
      const clock = await api.queryMetrics('vector(time())', signal);
      const serverTime = clock[0]?.value[1];
      if (serverTime === null || serverTime === undefined) {
        throw new Error('Arveld returned an unexpected response.');
      }
      const end = Math.floor(serverTime / step) * step;
      const start = end - 3600;
      const result = await api.queryMetricsRange(
        metric === 'disk'
          ? diskHistoryExpression(instanceUID, end)
          : metric === 'network'
            ? networkHistoryExpression(instanceUID, end)
            : expressions[metric](instanceUID),
        { start, end, step },
        signal,
      );
      return result.map((series) => ({
        metric: series.metric,
        values: metricHistory(series.values, start, end),
      }));
    },
    retry: false,
    refetchInterval: step * 1000,
    refetchOnWindowFocus: true,
  });
}
