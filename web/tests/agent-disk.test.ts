import { expect, spyOn, test } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { agentMetricOptions, diskExpression } from '../src/data/agent-metrics';
import { api, createApiClient } from '../src/data/api';

test('the disk gauge keeps the selected filesystem and its value together across refreshes', async () => {
  const query = spyOn(api, 'queryMetrics');
  const client = new QueryClient();
  try {
    for (const [mountpoint, device, value] of [
      ['/var/lib', '/dev/vda1', 62.5],
      ['/data', '/dev/vdb1', 75],
      ['/data', '/dev/vdb1', 0],
    ] as const) {
      const metric = { mountpoint, device };
      query.mockResolvedValue([{ metric, value: [100, value] }]);
      expect(await client.fetchQuery(agentMetricOptions('disk', 'agent-uid'))).toEqual({
        metric,
        value,
      });
    }
    query.mockResolvedValue([{ metric: { mountpoint: '/data' }, value: [100, null] }]);
    expect(await client.fetchQuery(agentMetricOptions('disk', 'agent-uid'))).toBeNull();
    query.mockResolvedValue([]);
    expect(await client.fetchQuery(agentMetricOptions('disk', 'agent-uid'))).toBeNull();
  } finally {
    client.clear();
    query.mockRestore();
  }
});

test('agent disk reads its usage and history through the authenticated PromQL endpoints', async () => {
  const uid = '0198f1ad-6f2a-7a4b-8c3d-123456789abc';
  const expression = diskExpression(uid);
  expect(expression).toContain(`instance="${uid}"`);
  expect(expression).toContain('mode="rw"');
  expect(expression).not.toContain('rate(');
  expect(diskExpression('uid"\\')).toContain('instance="uid\\"\\\\"');

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
              [start, '80'],
              [start + 15, '0'],
            ],
          },
        ],
      },
    });
  });
  expect((await api.queryMetrics(expression, controller.signal))[0].value[1]).toBe(0);
  const history = await api.queryMetricsRange(
    expression,
    { start, end, step: 15 },
    controller.signal,
  );
  expect(history[0].values).toEqual([
    [start, 80],
    [start + 15, 0],
  ]);
  expect(paths).toEqual(['/api/v1/metrics/query', '/api/v1/metrics/query_range']);
});
