import { expect, spyOn, test } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { agentMetricHistoryOptions } from '../src/data/agent-metrics';
import { api } from '../src/data/api';

const uid = '0198f1ad-6f2a-7a4b-8c3d-123456789abc';
const serverTime = 1_789_000_012.345;
const end = 1_789_000_005;

test('network history preserves interface directions, zero traffic and missing samples', async () => {
  const client = new QueryClient();
  const clock = spyOn(api, 'queryMetrics').mockResolvedValue([
    { metric: {}, value: [serverTime, serverTime] },
  ]);
  const range = spyOn(api, 'queryMetricsRange').mockResolvedValue([
    { metric: { device: 'eth0', direction: 'receive' }, values: [[end, 1000]] },
    { metric: { device: 'eth0', direction: 'transmit' }, values: [[end, 0]] },
    { metric: { device: 'eth1', direction: 'receive' }, values: [[end - 15, 25]] },
  ]);
  try {
    const history = await client.fetchQuery(agentMetricHistoryOptions('network', uid));
    expect(history.map((series) => series.metric)).toEqual([
      { device: 'eth0', direction: 'receive' },
      { device: 'eth0', direction: 'transmit' },
      { device: 'eth1', direction: 'receive' },
    ]);
    expect(history.map((series) => series.values.at(-1)?.value)).toEqual([1000, 0, null]);
    expect(range.mock.calls[0][0]).toContain('system_network_io_bytes_total');
    expect(range.mock.calls[0][0]).toContain(`instance="${uid}"`);
    expect(range.mock.calls[0][1]).toEqual({ start: end - 3600, end, step: 15 });
  } finally {
    client.clear();
    clock.mockRestore();
    range.mockRestore();
  }
});

test('disk history retains each filesystem and its gaps instead of taking only the first series', async () => {
  const client = new QueryClient();
  const clock = spyOn(api, 'queryMetrics').mockResolvedValue([
    { metric: {}, value: [serverTime, serverTime] },
  ]);
  const range = spyOn(api, 'queryMetricsRange').mockResolvedValue([
    { metric: { mountpoint: '/data', device: '/dev/a' }, values: [[end, 80]] },
    { metric: { mountpoint: '/backup', device: '/dev/b' }, values: [[end - 15, 0]] },
  ]);
  try {
    const history = await client.fetchQuery(agentMetricHistoryOptions('disk', uid));
    expect(history).toHaveLength(2);
    expect(history[0].metric).toEqual({ mountpoint: '/data', device: '/dev/a' });
    expect(history[1].metric).toEqual({ mountpoint: '/backup', device: '/dev/b' });
    expect(history[0].values.slice(-2)).toEqual([
      { timestamp: (end - 15) * 1000, value: null },
      { timestamp: end * 1000, value: 80 },
    ]);
    expect(history[1].values.slice(-2)).toEqual([
      { timestamp: (end - 15) * 1000, value: 0 },
      { timestamp: end * 1000, value: null },
    ]);
    expect(range.mock.calls[0][0]).toContain('topk(5,');
    expect(range.mock.calls[0][0]).toContain(`@ ${end}`);
  } finally {
    client.clear();
    clock.mockRestore();
    range.mockRestore();
  }
});

test.each(['cpu', 'memory', 'disk', 'network'] as const)(
  '%s history uses the Prometheus clock even when the browser is a day ahead or behind',
  async (metric) => {
    const clock = spyOn(Date, 'now');
    const instant = spyOn(api, 'queryMetrics').mockResolvedValue([
      { metric: {}, value: [serverTime, serverTime] },
    ]);
    const range = spyOn(api, 'queryMetricsRange').mockResolvedValue([
      {
        metric: {},
        values: [
          [end - 30, 0],
          [end, 25],
        ],
      },
    ]);
    try {
      for (const offset of [-86400, 86400]) {
        clock.mockReturnValue((serverTime + offset) * 1000);
        const client = new QueryClient();
        try {
          const history = await client.fetchQuery(agentMetricHistoryOptions(metric, uid));
          expect(range).toHaveBeenLastCalledWith(
            expect.stringContaining(`instance="${uid}"`),
            { start: end - 3600, end, step: 15 },
            expect.any(AbortSignal),
          );
          expect(instant).toHaveBeenLastCalledWith('vector(time())', expect.any(AbortSignal));
          expect(history[0].values).toHaveLength(241);
          expect(history[0].values.slice(-3)).toEqual([
            { timestamp: (end - 30) * 1000, value: 0 },
            { timestamp: (end - 15) * 1000, value: null },
            { timestamp: end * 1000, value: 25 },
          ]);
        } finally {
          client.clear();
        }
      }
    } finally {
      clock.mockRestore();
      instant.mockRestore();
      range.mockRestore();
    }
  },
);

test.each(['empty', 'invalid', 'unavailable'])(
  'history does not fall back to browser time when server time is %s',
  async (mode) => {
    const client = new QueryClient();
    const instant = spyOn(api, 'queryMetrics');
    if (mode === 'unavailable')
      instant.mockRejectedValue(new Error('Arveld is unavailable. Try again.'));
    else
      instant.mockResolvedValue(
        mode === 'empty' ? [] : [{ metric: {}, value: [serverTime, null] }],
      );
    const range = spyOn(api, 'queryMetricsRange').mockResolvedValue([]);
    try {
      await expect(client.fetchQuery(agentMetricHistoryOptions('cpu', uid))).rejects.toThrow(
        mode === 'unavailable'
          ? 'Arveld is unavailable. Try again.'
          : 'Arveld returned an unexpected response.',
      );
      expect(range).not.toHaveBeenCalled();
    } finally {
      client.clear();
      instant.mockRestore();
      range.mockRestore();
    }
  },
);
