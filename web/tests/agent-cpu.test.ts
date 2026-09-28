import { expect, test } from 'bun:test';
import { cpuExpression, metricHistory } from '../src/data/agent-metrics';
import { createApiClient } from '../src/data/api';

test('agent CPU reads PromQL through Arveld and preserves zeroes and gaps in the last hour', async () => {
  const uid = '0198f1ad-6f2a-7a4b-8c3d-123456789abc';
  const expression = cpuExpression(uid);
  expect(expression).toContain(`instance="${uid}"`);
  expect(expression).toContain('job="arveld-agent"');
  expect(expression).toContain('state="idle"');
  expect(expression).toContain('rate(system_cpu_time_seconds_total');
  expect(expression).toContain('[2m]');
  expect(expression).toContain('time() - max(timestamp(');
  expect(expression).toContain('< 60');
  expect(cpuExpression('uid"\\')).toContain('instance="uid\\"\\\\"');

  const end = 1_789_000_005;
  const start = end - 3600;
  const controller = new AbortController();
  const paths: string[] = [];
  const api = createApiClient(async (path, init) => {
    const url = new URL(path, 'http://arveld.test');
    paths.push(url.pathname);
    const parameters = new URLSearchParams(String(init.body));
    expect(parameters.get('query')).toBe(expression);
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    expect(init.signal).toBe(controller.signal);
    if (url.pathname.endsWith('/query')) {
      return Response.json({
        status: 'success',
        data: { resultType: 'vector', result: [{ metric: {}, value: [end, '0'] }] },
      });
    }
    expect(parameters.get('start')).toBe(String(start));
    expect(parameters.get('end')).toBe(String(end));
    expect(parameters.get('step')).toBe('15');
    return Response.json({
      status: 'success',
      data: {
        resultType: 'matrix',
        result: [
          {
            metric: {},
            values: [
              [start, '25.5'],
              [start + 30, '0'],
              [start + 45, 'NaN'],
            ],
          },
        ],
      },
    });
  });
  const current = await api.queryMetrics(expression, controller.signal);
  expect(current[0].value).toEqual([end, 0]);
  const history = await api.queryMetricsRange(
    expression,
    { start, end, step: 15 },
    controller.signal,
  );
  const points = metricHistory(history[0].values, start, end);
  expect(points).toHaveLength(241);
  expect(points.slice(0, 4)).toEqual([
    { timestamp: start * 1000, value: 25.5 },
    { timestamp: (start + 15) * 1000, value: null },
    { timestamp: (start + 30) * 1000, value: 0 },
    { timestamp: (start + 45) * 1000, value: null },
  ]);
  expect(points.at(-1)).toEqual({ timestamp: end * 1000, value: null });
  expect(paths).toEqual(['/api/v1/metrics/query', '/api/v1/metrics/query_range']);

  const empty = createApiClient(async () =>
    Response.json({ status: 'success', data: { resultType: 'vector', result: [] } }),
  );
  expect(await empty.queryMetrics(expression)).toEqual([]);
  expect(metricHistory([], start, end).every((point) => point.value === null)).toBe(true);
  for (const value of ['NaN', '+Inf', '-Inf']) {
    const nonFinite = createApiClient(async () =>
      Response.json({
        status: 'success',
        data: { resultType: 'vector', result: [{ metric: {}, value: [end, value] }] },
      }),
    );
    expect((await nonFinite.queryMetrics(expression))[0].value[1]).toBeNull();
  }
  const unavailable = createApiClient(
    async () => new Response('private Prometheus details', { status: 502 }),
  );
  await expect(unavailable.queryMetrics(expression)).rejects.toThrow(
    'Arveld is unavailable. Try again.',
  );
  const expired = createApiClient(async () => new Response(null, { status: 401 }));
  await expect(expired.queryMetrics(expression)).rejects.toMatchObject({ status: 401 });
  for (const body of [
    { status: 'error', error: 'private Prometheus details' },
    { status: 'success', data: { resultType: 'matrix', result: [] } },
    { status: 'success', data: { resultType: 'vector', result: [{ value: ['invalid', '0'] }] } },
  ]) {
    const malformed = createApiClient(async () => Response.json(body));
    await expect(malformed.queryMetrics(expression)).rejects.toThrow(
      'Arveld returned an unexpected response.',
    );
  }
});
