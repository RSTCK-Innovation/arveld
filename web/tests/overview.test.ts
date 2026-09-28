import { expect, spyOn, test } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { api, createApiClient } from '../src/data/api';
import {
  overviewExpression,
  overviewHistoryOptions,
  overviewMetricsOptions,
  summarizeFleet,
} from '../src/data/overview';

test('fleet queries fit behind an 8 KiB request-line limit', async () => {
  const paths: string[] = [];
  const client = createApiClient(async (path, init) => {
    paths.push(path);
    if (new TextEncoder().encode(path).length > 8192) return new Response(null, { status: 414 });
    const parameters = init.body
      ? new URLSearchParams(String(init.body))
      : new URL(path, 'https://arveld.test').searchParams;
    expect(parameters.get('query')).toContain('system_cpu_time_seconds_total');
    return Response.json({
      status: 'success',
      data: { resultType: path.includes('query_range') ? 'matrix' : 'vector', result: [] },
    });
  });
  for (const count of [5, 50]) {
    const ids = Array.from(
      { length: count },
      (_, index) => `00000000-0000-4000-8000-${String(index).padStart(12, '0')}`,
    );
    await expect(client.queryMetrics(overviewExpression(ids))).resolves.toEqual([]);
    await expect(
      client.queryMetricsRange(overviewExpression(ids, true), { start: 0, end: 3600, step: 15 }),
    ).resolves.toEqual([]);
  }
  expect(paths).toEqual([
    '/api/v1/metrics/query',
    '/api/v1/metrics/query_range',
    '/api/v1/metrics/query',
    '/api/v1/metrics/query_range',
  ]);
});

test('fleet readings preserve zero, missing coverage and agent boundaries', async () => {
  const client = new QueryClient();
  const read = spyOn(api, 'queryMetrics').mockResolvedValue([
    { metric: { instance: 'a', arveld_overview_metric: 'cpu' }, value: [9000, 0] },
    { metric: { instance: 'b', arveld_overview_metric: 'cpu' }, value: [9000, 40] },
    { metric: { instance: 'a', arveld_overview_metric: 'memory' }, value: [9000, 25] },
    { metric: { instance: 'a', arveld_overview_metric: 'disk' }, value: [9000, 90] },
    { metric: { instance: 'b', arveld_overview_metric: 'disk' }, value: [9000, 30] },
    { metric: { instance: 'unregistered', arveld_overview_metric: 'cpu' }, value: [9000, 100] },
  ]);
  try {
    const readings = await client.fetchQuery(overviewMetricsOptions(['b', 'a', 'a', 'c']));
    expect(summarizeFleet(readings, 'cpu')).toEqual({ value: 20, count: 2 });
    expect(summarizeFleet(readings, 'memory')).toEqual({ value: 25, count: 1 });
    expect(summarizeFleet(readings, 'disk')).toEqual({ value: 90, count: 2 });
    expect(summarizeFleet(readings, 'cores')).toEqual({ value: null, count: 0 });
    expect(readings.c.cpu).toBeNull();
    read.mockClear();
    expect(await client.fetchQuery(overviewMetricsOptions([]))).toEqual({});
    expect(read).not.toHaveBeenCalled();
    read.mockRejectedValue(new Error('Metrics unavailable'));
    await expect(client.fetchQuery(overviewMetricsOptions(['a']))).rejects.toThrow(
      'Metrics unavailable',
    );
  } finally {
    read.mockRestore();
    client.clear();
  }
});

test('fleet history uses server time, aligns the previous period and leaves missing steps empty', async () => {
  const client = new QueryClient();
  const clock = spyOn(api, 'queryMetrics').mockResolvedValue([{ metric: {}, value: [9012, 9012] }]);
  const range = spyOn(api, 'queryMetricsRange').mockResolvedValue([
    {
      metric: { arveld_overview_metric: 'cpu' },
      values: [
        [5370, 20],
        [8970, 0],
        [9000, 40],
      ],
    },
  ]);
  try {
    const result = await client.fetchQuery(overviewHistoryOptions(['a'], '1h'));
    expect(result.cpu.slice(-3)).toEqual([
      { timestamp: 8970000, value: 0, previous: 20 },
      { timestamp: 8985000, value: null, previous: null },
      { timestamp: 9000000, value: 40, previous: null },
    ]);
    expect(range).toHaveBeenLastCalledWith(
      expect.any(String),
      { start: 1800, end: 9000, step: 15 },
      expect.any(AbortSignal),
    );
    await client.fetchQuery(overviewHistoryOptions(['a'], '24h'));
    expect(range).toHaveBeenLastCalledWith(
      expect.any(String),
      { start: -163800, end: 9000, step: 360 },
      expect.any(AbortSignal),
    );
    clock.mockResolvedValue([]);
    await expect(client.fetchQuery(overviewHistoryOptions(['a'], '7d'))).rejects.toThrow(
      'Arveld returned an unexpected response.',
    );
  } finally {
    clock.mockRestore();
    range.mockRestore();
    client.clear();
  }
});
