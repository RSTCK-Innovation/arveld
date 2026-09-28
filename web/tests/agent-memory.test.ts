import { expect, test } from 'bun:test';
import { memoryExpression } from '../src/data/agent-metrics';
import { createApiClient } from '../src/data/api';

test('agent memory reads its usage and history through the authenticated PromQL endpoints', async () => {
  const uid = '0198f1ad-6f2a-7a4b-8c3d-123456789abc';
  const labels = `job="arveld-agent",instance="${uid}"`;
  const available = `system_linux_memory_available_bytes{${labels}}`;
  const total = `system_memory_limit_bytes{${labels}}`;
  const expression = memoryExpression(uid);
  expect(expression).toContain(`1 - ${available} / (${total} > 0)`);
  expect(expression).toContain(`time() - timestamp(${available}) < 60`);
  expect(expression).toContain(`time() - timestamp(${total}) < 60`);
  expect(expression).toContain(`${available} >= 0`);
  expect(expression).toContain(`${available} <= ${total}`);
  expect(expression).not.toContain('rate(');
  expect(memoryExpression('uid"\\')).toContain('instance="uid\\"\\\\"');

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
        data: {
          resultType: 'vector',
          result: [{ metric: { job: 'arveld-agent', instance: uid }, value: [end, '0'] }],
        },
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
            metric: { job: 'arveld-agent', instance: uid },
            values: [
              [start, '25'],
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
    [start, 25],
    [start + 15, 0],
  ]);
  expect(paths).toEqual(['/api/v1/metrics/query', '/api/v1/metrics/query_range']);
});
