import { expect, spyOn, test } from 'bun:test';
import { QueryClient } from '@tanstack/react-query';
import { agentUptimeOptions } from '../src/data/agent-metrics';
import { api } from '../src/data/api';
import { formatDuration, setFormatLocale } from '../src/i18n/format';

test('host uptime uses a fresh UID-scoped metric and preserves missing measurements', async () => {
  const uid = '0198f1ad-6f2a-7a4b-8c3d-123456789abc';
  const query = spyOn(api, 'queryMetrics');
  const client = new QueryClient();
  try {
    for (const value of [90061, 0, null]) {
      query.mockResolvedValue(value === null ? [] : [{ metric: {}, value: [100, value] }]);
      expect(await client.fetchQuery(agentUptimeOptions(uid))).toBe(value);
    }
    expect(query).toHaveBeenLastCalledWith(
      expect.stringContaining(`system_uptime_seconds{job="arveld-agent",instance="${uid}"}`),
      expect.any(AbortSignal),
    );
    const expression = query.mock.calls[0][0];
    expect(expression).toContain('timestamp(');
    expect(expression).toContain('< 60');
    expect(expression).toContain('>= 0');
    expect(expression).toContain('< +Inf');
  } finally {
    client.clear();
    query.mockRestore();
  }
});

test('host uptime formats elapsed seconds without turning them into a date', () => {
  setFormatLocale('en-US');
  expect(formatDuration(0)).toBe('0 sec');
  expect(formatDuration(59.9)).toBe('59 sec');
  expect(formatDuration(60)).toBe('1 min');
  expect(formatDuration(3661)).toBe('1 hr 1 min');
  expect(formatDuration(90061)).toBe('1 day 1 hr 1 min');
});
