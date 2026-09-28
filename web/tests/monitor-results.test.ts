import { expect, spyOn, test } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { metricHistory } from '../src/data/agent-metrics';
import { api, createApiClient } from '../src/data/api';
import {
  healthyMonitorHistory,
  monitorFreshness,
  monitorHistoriesOptions,
  monitorResultsOptions,
  monitorUptime,
} from '../src/data/monitor-results';

test('healthy monitor trend aligns timestamps and preserves zeroes and incomplete coverage', () => {
  const first = [
    { timestamp: 15000, value: 1 },
    { timestamp: 30000, value: 1 },
    { timestamp: 45000, value: 0 },
    { timestamp: 60000, value: null },
  ];
  const second = [
    { timestamp: 30000, value: 1 },
    { timestamp: 45000, value: 0 },
    { timestamp: 60000, value: 1 },
    { timestamp: 75000, value: 1 },
  ];
  expect(healthyMonitorHistory([first, second])).toEqual([
    { timestamp: 30000, value: 2 },
    { timestamp: 45000, value: 0 },
    { timestamp: 60000, value: null },
    { timestamp: 75000, value: null },
  ]);
  expect(healthyMonitorHistory([first, []]).every((point) => point.value === null)).toBe(true);
  expect(healthyMonitorHistory([])).toEqual([]);
});

test('uptime bars preserve short failures and missing periods across the last hour', () => {
  const points = metricHistory([], 5400, 9000).map((point) => ({
    ...point,
    value: 1 as number | null,
  }));
  points[6].value = 0; // A single failed sample must remain visible in the first period.
  points[7].value = null;
  const uptime = monitorUptime(points);
  expect(uptime.periods).toHaveLength(40);
  expect(uptime.periods[0]).toEqual({ start: 5400000, end: 5490000, status: 'failure' });
  expect(uptime.periods[1].status).toBe('unknown');
  expect(uptime.periods.slice(2).every((period) => period.status === 'success')).toBe(true);
  expect(uptime.periods[39].end).toBe(9000000);
  expect(uptime.successRate).toBeCloseTo((238 / 239) * 100);
});

test('uptime bars never turn missing history into successful measurements', () => {
  for (const points of [[], metricHistory([], 5400, 9000)]) {
    const uptime = monitorUptime(points);
    expect(uptime.periods).toHaveLength(40);
    expect(uptime.periods.every((period) => period.status === 'unknown')).toBe(true);
    expect(uptime.successRate).toBeNull();
  }
});

const base = {
  id: 'monitor',
  agent_instance_uid: 'agent',
  interval_seconds: 30,
  timeout_seconds: 5,
};

test('a monitor list shares the clock and batches history reads', async () => {
  const client = new QueryClient();
  const clock = spyOn(api, 'queryMetrics').mockResolvedValue([{ metric: {}, value: [9012, 9012] }]);
  const range = spyOn(api, 'queryMetricsRange').mockResolvedValue([]);
  const monitors = Array.from({ length: 45 }, (_, index) => ({
    ...base,
    id: `monitor-${index}`,
    protocol: (['http', 'tcp', 'icmp', 'dns'] as const)[index % 4],
  }));
  try {
    const histories = await client.fetchQuery(monitorHistoriesOptions(monitors));
    expect(Object.keys(histories)).toHaveLength(45);
    expect(Object.values(histories).every((history) => history.status.length === 241)).toBe(true);
    expect(clock).toHaveBeenCalledTimes(1);
    expect(range).toHaveBeenCalledTimes(3);
  } finally {
    clock.mockRestore();
    range.mockRestore();
    client.clear();
  }
});

test.each(['http', 'tcp', 'icmp', 'dns'] as const)(
  '%s reads real results and preserves failure and zero latency',
  async (protocol) => {
    const client = new QueryClient();
    const read = spyOn(api, 'queryMetrics').mockResolvedValue([
      { metric: { arveld_result_index: '0', result: 'status' }, value: [9000, 0] },
      { metric: { arveld_result_index: '0', result: 'latency' }, value: [9000, 0] },
      { metric: { arveld_result_index: '0', result: 'measured_at' }, value: [9000, 8990] },
    ]);
    try {
      const options = { ...monitorResultsOptions([{ ...base, protocol }]), staleTime: 0 };
      expect((await client.fetchQuery(options))[base.id]).toEqual({
        status: 'failure',
        latency: 0,
        measuredAt: 8990,
        loss: null,
      });
      expect(read).toHaveBeenLastCalledWith(expect.any(String), expect.any(AbortSignal), 65);
      read.mockResolvedValue([]);
      expect((await client.fetchQuery(options))[base.id].status).toBe('unknown');
      read.mockRejectedValue(new Error('Arveld is unavailable. Try again.'));
      await expect(client.fetchQuery(options)).rejects.toThrow('Arveld is unavailable. Try again.');
    } finally {
      read.mockRestore();
      client.clear();
    }
  },
);

test('bulk reads preserve identity, missing data, cache sharing and refresh intervals', async () => {
  const client = new QueryClient();
  const monitors = ['a', 'b', 'c'].map((id) => ({ ...base, id, protocol: 'http' as const }));
  const read = spyOn(api, 'queryMetrics').mockResolvedValue([
    { metric: { arveld_result_index: '2', result: 'status' }, value: [9000, 0] },
    { metric: { arveld_result_index: '0', result: 'status' }, value: [9000, 1] },
    { metric: { arveld_result_index: '0', result: 'latency' }, value: [9000, 0] },
  ]);
  const range = spyOn(api, 'queryMetricsRange').mockResolvedValue([
    { metric: { arveld_result_index: '2', result: 'status' }, values: [[9000, 0]] },
    { metric: { arveld_result_index: '0', result: 'status' }, values: [[9000, 1]] },
  ]);
  try {
    const results = await client.fetchQuery(monitorResultsOptions(monitors));
    expect(results.a.status).toBe('success');
    expect(results.a.latency).toBe(0);
    expect(results.b.status).toBe('unknown');
    expect(results.c.status).toBe('failure');
    await client.fetchQuery(monitorResultsOptions([...monitors].reverse()));
    expect(read).toHaveBeenCalledTimes(1);
    read.mockResolvedValue([{ metric: {}, value: [9012, 9012] }]);
    const [history] = await Promise.all([
      client.fetchQuery(monitorHistoriesOptions(monitors)),
      client.fetchQuery(monitorHistoriesOptions(monitors, '6h')),
    ]);
    expect(read).toHaveBeenCalledTimes(2); // One instant batch and one shared clock.
    expect(history.a.status.at(-1)?.value).toBe(1);
    expect(history.b.status.every((point) => point.value === null)).toBe(true);
    expect(history.c.status.at(-1)?.value).toBe(0);
    await client.fetchQuery(monitorHistoriesOptions([...monitors].reverse()));
    expect(range).toHaveBeenCalledTimes(2);
    expect(monitorResultsOptions(monitors).refetchInterval).toBe(15000);
    expect(monitorHistoriesOptions(monitors).refetchInterval).toBe(60000);
    expect(await client.fetchQuery(monitorHistoriesOptions([]))).toEqual({});
    expect(await client.fetchQuery(monitorResultsOptions([]))).toEqual({});
    expect(read).toHaveBeenCalledTimes(2);
    expect(range).toHaveBeenCalledTimes(2);
  } finally {
    read.mockRestore();
    range.mockRestore();
    client.clear();
  }
});

test('failed or cancelled batches do not fan out into the remaining inventory', async () => {
  const client = new QueryClient();
  const monitors = Array.from({ length: 45 }, (_, index) => ({
    ...base,
    id: String(index),
    protocol: 'tcp' as const,
  }));
  const clock = spyOn(api, 'queryMetrics').mockResolvedValue([{ metric: {}, value: [9012, 9012] }]);
  const range = spyOn(api, 'queryMetricsRange').mockRejectedValue(new Error('Unavailable'));
  const options = monitorHistoriesOptions(monitors);
  try {
    await expect(client.fetchQuery(options)).rejects.toThrow('Unavailable');
    expect(range).toHaveBeenCalledTimes(1);
    range.mockImplementation(async () => {
      await client.cancelQueries({ queryKey: options.queryKey });
      return [];
    });
    await expect(client.fetchQuery(options)).rejects.toThrow();
    expect(range).toHaveBeenCalledTimes(2);
  } finally {
    clock.mockRestore();
    range.mockRestore();
    client.clear();
  }
});

test('history follows the server clock and preserves gaps, failures and zero latency', async () => {
  const client = new QueryClient();
  const clock = spyOn(api, 'queryMetrics').mockResolvedValue([{ metric: {}, value: [9012, 9012] }]);
  const range = spyOn(api, 'queryMetricsRange').mockResolvedValue([
    {
      metric: { arveld_result_index: '0', result: 'status' },
      values: [
        [8970, 1],
        [9000, 0],
      ],
    },
    { metric: { arveld_result_index: '0', result: 'latency' }, values: [[8970, 0]] },
  ]);
  try {
    const history = (
      await client.fetchQuery(monitorHistoriesOptions([{ ...base, protocol: 'dns' }]))
    )[base.id];
    expect(history.status).toHaveLength(241);
    expect(history.status.slice(-3).map((p) => p.value)).toEqual([1, null, 0]);
    expect(history.latency.slice(-3).map((p) => p.value)).toEqual([0, null, null]);
    expect(range).toHaveBeenLastCalledWith(
      expect.any(String),
      { start: 5400, end: 9000, step: 15, lookback: 65 },
      expect.any(AbortSignal),
    );
    clock.mockResolvedValue([]);
    await client.invalidateQueries({ queryKey: ['monitor-clock'] });
    await expect(
      client.fetchQuery(monitorHistoriesOptions([{ ...base, protocol: 'http' }])),
    ).rejects.toThrow('Arveld returned an unexpected response.');
  } finally {
    clock.mockRestore();
    range.mockRestore();
    client.clear();
  }
});

test('monitor trend follows each selected period without sharing cached windows', async () => {
  const client = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  const clock = spyOn(api, 'queryMetrics').mockResolvedValue([
    { metric: {}, value: [1800012, 1800012] },
  ]);
  const request = spyOn(api, 'queryMetricsRange').mockImplementation(async (_, { end, step }) => [
    {
      metric: { arveld_result_index: '0', result: 'status' },
      values: [
        [end - 2 * step, 1],
        [end, 0],
      ],
    },
  ]);
  const monitor = { ...base, protocol: 'http' as const };
  try {
    for (const [range, start, end, step] of [
      ['1h', 1796400, 1800000, 15],
      ['6h', 1778400, 1800000, 90],
      ['24h', 1713600, 1800000, 360],
      ['7d', 1194480, 1799280, 2520],
    ] as const) {
      const history = (await client.fetchQuery(monitorHistoriesOptions([monitor], range)))[
        monitor.id
      ];
      expect(request).toHaveBeenLastCalledWith(
        expect.any(String),
        { start, end, step, lookback: 65 },
        expect.any(AbortSignal),
      );
      expect(history.status).toHaveLength(241);
      expect(history.status[0].timestamp).toBe(start * 1000);
      expect(history.status.at(-1)?.timestamp).toBe(end * 1000);
      expect(history.status.slice(-3).map((point) => point.value)).toEqual([1, null, 0]);
      expect(
        healthyMonitorHistory([history.status])
          .slice(-3)
          .map((point) => point.value),
      ).toEqual([1, null, 0]);
    }
    const hour = (await client.fetchQuery(monitorHistoriesOptions([monitor])))[monitor.id];
    expect(hour.status[0].timestamp).toBe(1796400000);
    expect(request).toHaveBeenCalledTimes(4);
  } finally {
    clock.mockRestore();
    request.mockRestore();
    client.clear();
  }
});

test('monitor reads carry the optional lookback through both browser API methods', async () => {
  const client = createApiClient(async (path, init) => {
    expect(init.signal).toBeInstanceOf(AbortSignal);
    expect(new URLSearchParams(String(init.body)).get('lookback_delta')).toBe('7260');
    return Response.json({
      status: 'success',
      data: { resultType: path.includes('query_range') ? 'matrix' : 'vector', result: [] },
    });
  });
  const signal = new AbortController().signal;
  await client.queryMetrics('up', signal, 7260);
  await client.queryMetricsRange('up', { start: 0, end: 1, step: 1, lookback: 7260 }, signal);
  expect(
    monitorFreshness({ ...base, protocol: 'http', interval_seconds: 3600, timeout_seconds: 60 }),
  ).toBe(7260);
});
