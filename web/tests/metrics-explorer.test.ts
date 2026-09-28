import { expect, spyOn, test } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { api, createApiClient } from '../src/data/api';
import { metricLabels, metricsCSV, metricsOptions } from '../src/data/metrics';

test('resubmitting after reopening Explorer reads a fresh server period', async () => {
  const client = new QueryClient();
  const clock = spyOn(api, 'queryMetrics').mockResolvedValue([{ metric: {}, value: [9005, 9005] }]);
  const range = spyOn(api, 'queryMetricsRange').mockResolvedValue([]);
  try {
    expect((await client.fetchQuery(metricsOptions('up', '1h', 1))).end).toBe(9000);
    clock.mockResolvedValue([{ metric: {}, value: [15005, 15005] }]);
    // The page's first submission counter restarts after leaving and returning.
    expect((await client.fetchQuery(metricsOptions('up', '1h', 1))).end).toBe(15000);
    expect(range).toHaveBeenCalledTimes(2);
  } finally {
    clock.mockRestore();
    range.mockRestore();
    client.clear();
  }
});

test('Explorer reads the requested expression on server time and preserves labelled series, zeroes and gaps', async () => {
  const client = new QueryClient();
  const expression = 'test_metric{job="arveld-agent"}';
  const requests: string[] = [];
  const transport = createApiClient(async (path, init) => {
    requests.push(path);
    expect(init.method).toBe('POST');
    expect(init.credentials).toBe('same-origin');
    expect(init.signal).toBeInstanceOf(AbortSignal);
    const form = new URLSearchParams(String(init.body));
    if (path === '/api/v1/metrics/query') {
      expect(form.get('query')).toBe('vector(time())');
      return Response.json({
        status: 'success',
        data: {
          resultType: 'vector',
          result: [{ metric: {}, value: [9005, '9005'] }],
        },
      });
    }
    expect(form.get('query')).toBe(expression);
    expect(form.get('start')).toBe('5400');
    expect(form.get('end')).toBe('9000');
    expect(form.get('step')).toBe('15');
    return Response.json({
      status: 'success',
      data: {
        resultType: 'matrix',
        result: [
          {
            metric: { instance: 'a', device: 'eth0' },
            values: [
              [8970, '0'],
              [9000, '-2.5'],
            ],
          },
          {
            metric: { instance: 'b', device: 'eth1' },
            values: [
              [8985, '4'],
              [9000, 'NaN'],
            ],
          },
        ],
      },
    });
  });
  const instant = spyOn(api, 'queryMetrics').mockImplementation(transport.queryMetrics);
  const range = spyOn(api, 'queryMetricsRange').mockImplementation(transport.queryMetricsRange);
  try {
    const result = await client.fetchQuery(metricsOptions(expression, '1h'));
    expect(requests).toEqual(['/api/v1/metrics/query', '/api/v1/metrics/query_range']);
    expect(result.start).toBe(5400);
    expect(result.end).toBe(9000);
    expect(result.series.map((series) => series.metric)).toEqual([
      { instance: 'a', device: 'eth0' },
      { instance: 'b', device: 'eth1' },
    ]);
    expect(result.series[0].values).toHaveLength(241);
    expect(result.series[0].values.slice(-3)).toEqual([
      [8970, 0],
      [8985, null],
      [9000, -2.5],
    ]);
    expect(result.series[1].values.slice(-3)).toEqual([
      [8970, null],
      [8985, 4],
      [9000, null],
    ]);
  } finally {
    instant.mockRestore();
    range.mockRestore();
    client.clear();
  }
});

test('Explorer CSV retains every series label, exact value and missing measurement', () => {
  const series = [
    {
      metric: { instance: 'a,"b', __name__: 'voltage' },
      values: [
        [0, 0],
        [15, null],
        [30, -2.5],
      ] as [number, number | null][],
    },
  ];
  expect(metricLabels(series[0].metric)).toBe('{"__name__":"voltage","instance":"a,\\"b"}');
  const csv = metricsCSV(series);
  expect(csv.split('\n')).toHaveLength(4);
  expect(csv.split('\n')[0]).toBe('timestamp,labels,value');
  expect(csv.split('\n')[1]).toBe(
    '1970-01-01T00:00:00.000Z,"{""__name__"":""voltage"",""instance"":""a,\\""b""}",0',
  );
  expect(csv.split('\n')[2]).toEndWith(',');
  expect(csv.split('\n')[3]).toEndWith(',-2.5');
});

test('Explorer distinguishes empty data, failed queries and unavailable server time', async () => {
  const client = new QueryClient();
  const clock = spyOn(api, 'queryMetrics').mockResolvedValue([{ metric: {}, value: [9005, 9005] }]);
  const range = spyOn(api, 'queryMetricsRange').mockResolvedValue([]);
  try {
    expect((await client.fetchQuery(metricsOptions('absent_metric', '7d'))).series).toEqual([]);
    range.mockRejectedValue(new Error('The metrics engine is unavailable'));
    await expect(client.fetchQuery(metricsOptions('metric', '1h'))).rejects.toThrow(
      'The metrics engine is unavailable',
    );
    range.mockClear();
    clock.mockResolvedValue([]);
    await expect(client.fetchQuery(metricsOptions('metric', '6h'))).rejects.toThrow(
      'Arveld returned an unexpected response.',
    );
    expect(range).not.toHaveBeenCalled();
  } finally {
    clock.mockRestore();
    range.mockRestore();
    client.clear();
  }
});
