import { queryOptions } from '@tanstack/react-query';
import { metricHistory } from './agent-metrics';
import { api, type Monitor } from './api';
import type { Range } from './time-range';

export type MonitorStatus = 'success' | 'failure' | 'unknown';
type ResultMonitor = Pick<
  Monitor,
  'id' | 'protocol' | 'agent_instance_uid' | 'interval_seconds' | 'timeout_seconds'
> & { validations?: { type: string }[] };
const step = 15;
const metrics = {
  http: ['httpcheck_status', 'httpcheck_duration_milliseconds'],
  tcp: ['tcpcheck_status_ratio', 'tcpcheck_duration_milliseconds'],
  icmp: ['ping_loss_ratio_percent', 'ping_rtt_avg_milliseconds'],
  dns: ['dnscheck_status', 'dnscheck_duration_milliseconds'],
} as const;

// Allow two missed intervals plus the execution timeout, with a one-minute floor.
export function monitorFreshness(monitor: ResultMonitor) {
  return Math.max(60, 2 * monitor.interval_seconds + monitor.timeout_seconds);
}

export function monitorResultExpression(monitor: ResultMonitor, samplingWindow?: number) {
  const labels = `job="arveld-agent",instance=${JSON.stringify(monitor.agent_instance_uid)},arveld_monitor_id=${JSON.stringify(monitor.id)}`;
  const [statusName, durationName] = metrics[monitor.protocol];
  const raw = `${statusName}{${labels}}`;
  const measuredAt = `max(timestamp(${raw}))`;
  const recent = `(${measuredAt} > time() - ${monitorFreshness(monitor)}) and (${measuredAt} <= time())`;
  // HTTP codes and DNS answers change labels. Only the newest measurement counts;
  // older successful series can otherwise survive within Prometheus's lookback.
  const selected =
    monitor.protocol === 'http' ? `${statusName}{${labels},http_status_class=~"[23]xx"}` : raw;
  const current = `(${selected} and (timestamp(${selected}) == scalar(${recent})))`;
  const valid = `(${current} >= 0) and (${current} <= ${monitor.protocol === 'icmp' ? 100 : 1})`;
  const status = monitor.protocol === 'icmp' ? `(max(${valid}) == bool 0)` : `min(${valid})`;
  // HTTP emits zero-valued nonmatching classes alongside the successful class.
  let success = monitor.protocol === 'http' ? `max(${valid})` : status;
  const ruleCount = monitor.protocol === 'http' ? (monitor.validations?.length ?? 0) : 0;
  if (ruleCount) {
    const count = (outcome: 'passed' | 'failed') => {
      const series = `httpcheck_validation_${outcome}{${labels}}`;
      // Without `bool`, >= filters samples while preserving their counts. One
      // validation.type series can represent multiple rules; never count series.
      return `(sum((${series} >= 0) and (timestamp(${series}) == scalar(${recent}))) or vector(0))`;
    };
    const passed = count('passed');
    const failed = count('failed');
    // A sparse failure must belong to this exact scrape. Missing assertions are
    // unknown; HTTP/transport failures and explicit assertion failures remain failures.
    success = `(${success} == 0) or ((${success}) * ((vector(0) and (${failed} > 0)) or (vector(1) and (${passed} == ${ruleCount}) and (${failed} == 0))))`;
  }
  let historicalStatus: string | undefined;
  let historicalChecks: string | undefined;
  if (samplingWindow !== undefined) {
    // Read the raw samples in (time - window, time], not a subquery that could
    // itself skip a failure and recovery between evaluations.
    const window = `[${samplingWindow}s]`;
    if (monitor.protocol === 'http') {
      // Every HTTP check emits all five classes, including transport failures.
      // Counting just 1xx counts each check once; summing 2xx/3xx counts successes
      // even when status-code labels change. Any shortfall means a failure.
      const successes = `sum(sum_over_time(${selected}${window}))`;
      const checks = `sum(count_over_time(${statusName}{${labels},http_status_class="1xx"}${window}))`;
      historicalStatus = `(${successes} == bool ${checks})`;
      if (ruleCount) {
        const failed = `(sum(sum_over_time(httpcheck_validation_failed{${labels}}${window})) or vector(0))`;
        const passed = `(sum(sum_over_time(httpcheck_validation_passed{${labels}}${window})) or vector(0))`;
        historicalChecks = checks;
        historicalStatus = `(vector(0) and ((${successes} < ${checks}) or (${failed} > 0))) or (vector(1) and (${successes} == ${checks}) and (${passed} == ${ruleCount} * ${checks}) and (${failed} == 0))`;
      }
    } else if (monitor.protocol === 'icmp') {
      historicalStatus = `(max(max_over_time(${raw}${window})) == bool 0)`;
    } else {
      historicalStatus = `min(min_over_time(${raw}${window}))`;
    }
  }
  const duration = `${durationName}{${labels}}`;
  const latency = `max((${duration} >= 0) and (${duration} < +Inf) and (timestamp(${duration}) == scalar(${recent})))`;
  const fields = {
    // With no new samples, a slow Monitor can retain its latest fresh reading.
    // The window takes precedence, including failures older than the lookback.
    status: historicalStatus
      ? `(${historicalStatus}) or (${success}${historicalChecks ? ` unless (${historicalChecks} > 0)` : ''})`
      : success,
    latency: monitor.protocol === 'icmp' ? `(${latency}) and (max(${valid}) < 100)` : latency,
    measured_at: recent,
    ...(monitor.protocol === 'icmp' ? { loss: `max(${valid})` } : {}),
  };
  return Object.entries(fields)
    .map(([field, expression]) => `label_replace((${expression}), "result", "${field}", "", "")`)
    .join(' or ');
}

export function resultStatus(value: number | null | undefined): MonitorStatus {
  return value === 1 ? 'success' : value === 0 ? 'failure' : 'unknown';
}

export function healthyMonitorHistory(histories: ReturnType<typeof metricHistory>[]) {
  // Refreshes can cross a sampling boundary. Join by timestamp, not array index,
  // and leave a gap whenever any Monitor lacks a known result for that instant.
  const latest = histories.reduce<ReturnType<typeof metricHistory>>(
    (latest, points) =>
      (points.at(-1)?.timestamp ?? 0) > (latest.at(-1)?.timestamp ?? 0) ? points : latest,
    [],
  );
  const values = histories.map(
    (points) => new Map(points.map((point) => [point.timestamp, point.value])),
  );
  return latest.map(({ timestamp }) => {
    const statuses = values.map((series) => resultStatus(series.get(timestamp)));
    return {
      timestamp,
      value: statuses.includes('unknown')
        ? null
        : statuses.filter((status) => status === 'success').length,
    };
  });
}

export type UptimePeriod = { start: number | null; end: number | null; status: MonitorStatus };

export function monitorUptime(points: ReturnType<typeof metricHistory>) {
  // The range contains 241 endpoints: each bar covers six 15-second windows in
  // (start, end], so 40 bars cover exactly one hour without counting a boundary twice.
  const samples = points.slice(1);
  const periods: UptimePeriod[] = Array.from({ length: 40 }, (_, index) => {
    const values = samples.slice(index * 6, (index + 1) * 6);
    return {
      start: points[index * 6]?.timestamp ?? null,
      end: points[(index + 1) * 6]?.timestamp ?? null,
      status: values.some((point) => point.value === 0)
        ? 'failure'
        : values.length === 6 && values.every((point) => point.value === 1)
          ? 'success'
          : 'unknown',
    };
  });
  const received = samples.filter((point) => resultStatus(point.value) !== 'unknown');
  return {
    periods,
    successRate: received.length
      ? (received.filter((point) => point.value === 1).length / received.length) * 100
      : null,
  };
}

export type MonitorReading = {
  status: MonitorStatus;
  latency: number | null;
  measuredAt: number | null;
  loss: number | null;
};
export type MonitorHistoryData = {
  status: ReturnType<typeof metricHistory>;
  latency: ReturnType<typeof metricHistory>;
};

// Each request contains at most 20 independent expressions. Requests within a
// collection run sequentially, so a large inventory cannot burst at the server.
const batchSize = 20;
function orderedMonitors(monitors: ResultMonitor[]) {
  return monitors
    .map(
      ({ id, protocol, agent_instance_uid, interval_seconds, timeout_seconds, validations }) => ({
        id,
        protocol,
        agent_instance_uid,
        interval_seconds,
        timeout_seconds,
        ...(validations?.length ? { validations: validations.map(({ type }) => ({ type })) } : {}),
      }),
    )
    .sort((a, b) => a.id.localeCompare(b.id));
}

export function monitorBatchExpression(monitors: ResultMonitor[], samplingWindow?: number) {
  return monitors
    .map(
      (monitor, index) =>
        `label_replace((${monitorResultExpression(monitor, samplingWindow)}), "arveld_result_index", "${index}", "", "")`,
    )
    .join(' or ');
}

const clockOptions = queryOptions({
  queryKey: ['monitor-clock'],
  queryFn: async ({ signal }) => {
    const clock = await api.queryMetrics('vector(time())', signal);
    const value = clock[0]?.value[1];
    if (value === null || value === undefined || !Number.isFinite(value))
      throw new Error('Arveld returned an unexpected response.');
    return value;
  },
  staleTime: step * 1000,
  retry: false,
});

export function monitorResultsOptions(monitors: ResultMonitor[]) {
  const ordered = orderedMonitors(monitors);
  return queryOptions({
    queryKey: ['monitor-results', ordered],
    queryFn: async ({ signal }): Promise<Record<string, MonitorReading>> => {
      const readings: Record<string, MonitorReading> = {};
      for (let offset = 0; offset < ordered.length; offset += batchSize) {
        signal.throwIfAborted();
        const batch = ordered.slice(offset, offset + batchSize);
        const rows = await api.queryMetrics(
          monitorBatchExpression(batch),
          signal,
          Math.max(...batch.map(monitorFreshness)),
        );
        batch.forEach((monitor, index) => {
          const value = (field: string) =>
            rows.find(
              (row) =>
                row.metric.arveld_result_index === String(index) && row.metric.result === field,
            )?.value[1] ?? null;
          readings[monitor.id] = {
            status: resultStatus(value('status')),
            latency: value('latency'),
            measuredAt: value('measured_at'),
            loss: value('loss'),
          };
        });
      }
      return readings;
    },
    retry: false,
    staleTime: step * 1000,
    refetchInterval: step * 1000,
    refetchOnWindowFocus: true,
  });
}

export function monitorHistoriesOptions(monitors: ResultMonitor[], range: Range = '1h') {
  const ordered = orderedMonitors(monitors);
  const duration = { '1h': 3600, '6h': 21600, '24h': 86400, '7d': 604800 }[range];
  // Keep 241 points at every range, as for the other overview trends.
  const samplingStep = duration / 240;
  const refreshInterval = Math.max(60000, samplingStep * 1000);
  return queryOptions({
    queryKey: ['monitor-histories', ordered, range],
    queryFn: async ({ signal, client }): Promise<Record<string, MonitorHistoryData>> => {
      const histories: Record<string, MonitorHistoryData> = {};
      if (!ordered.length) return histories;
      const serverTime = await client.fetchQuery(clockOptions);
      signal.throwIfAborted();
      const end = Math.floor(serverTime / samplingStep) * samplingStep;
      const start = end - duration;
      for (let offset = 0; offset < ordered.length; offset += batchSize) {
        signal.throwIfAborted();
        const batch = ordered.slice(offset, offset + batchSize);
        const rows = await api.queryMetricsRange(
          monitorBatchExpression(batch, samplingStep),
          { start, end, step: samplingStep, lookback: Math.max(...batch.map(monitorFreshness)) },
          signal,
        );
        batch.forEach((monitor, index) => {
          const history = (field: string) =>
            metricHistory(
              rows.find(
                (row) =>
                  row.metric.arveld_result_index === String(index) && row.metric.result === field,
              )?.values ?? [],
              start,
              end,
              samplingStep,
            );
          histories[monitor.id] = { status: history('status'), latency: history('latency') };
        });
      }
      return histories;
    },
    retry: false,
    staleTime: refreshInterval,
    refetchInterval: refreshInterval,
    refetchOnWindowFocus: true,
  });
}

export const httpDetailMetrics = {
  dns: { metric: 'httpcheck_dns_lookup_duration_nanoseconds', divisor: 1e6 },
  connection: { metric: 'httpcheck_client_connection_duration_nanoseconds', divisor: 1e6 },
  tls: { metric: 'httpcheck_tls_handshake_duration_nanoseconds', divisor: 1e6 },
  request: { metric: 'httpcheck_client_request_duration_nanoseconds', divisor: 1e6 },
  headers: { metric: 'httpcheck_response_duration_nanoseconds', divisor: 1e6 },
  size: { metric: 'httpcheck_response_size_bytes', divisor: 1 },
  certificate: { metric: 'httpcheck_tls_cert_remaining_seconds', divisor: 1 },
} as const;
export type HTTPDetail = keyof typeof httpDetailMetrics;

export function httpDetailsExpression(monitor: ResultMonitor) {
  const labels = `job="arveld-agent",instance=${JSON.stringify(monitor.agent_instance_uid)},arveld_monitor_id=${JSON.stringify(monitor.id)}`;
  const measured = `max(timestamp(httpcheck_status{${labels}}))`;
  const recent = `(${measured} > time() - ${monitorFreshness(monitor)}) and (${measured} <= time())`;
  return Object.entries(httpDetailMetrics)
    .map(([field, { metric, divisor }]) => {
      const series = `${metric}{${labels}}`;
      // Certificates may already be expired. Missing values remain absent; zero
      // timings from a reused connection remain zero, never a previous scrape.
      const value = `max((${series} > -Inf) and (${series} < +Inf) and (timestamp(${series}) == scalar(${recent}))) / ${divisor}`;
      return `label_replace((${value}), "result", "${field}", "", "")`;
    })
    .join(' or ');
}

export function httpDetailsOptions(monitor: ResultMonitor) {
  return queryOptions({
    queryKey: [
      'http-details',
      monitor.id,
      monitor.agent_instance_uid,
      monitor.interval_seconds,
      monitor.timeout_seconds,
    ],
    queryFn: async ({ signal }): Promise<Partial<Record<HTTPDetail, number | null>>> => {
      const rows = await api.queryMetrics(
        httpDetailsExpression(monitor),
        signal,
        monitorFreshness(monitor),
      );
      const values: Partial<Record<HTTPDetail, number | null>> = {};
      for (const field of Object.keys(httpDetailMetrics) as HTTPDetail[]) {
        values[field] = rows.find((row) => row.metric.result === field)?.value[1] ?? null;
      }
      return values;
    },
    retry: false,
    staleTime: step * 1000,
    refetchInterval: step * 1000,
  });
}
