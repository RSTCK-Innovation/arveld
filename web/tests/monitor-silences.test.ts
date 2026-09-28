import { expect, test } from 'bun:test';
import { createApiClient, type MonitorSilence } from '../src/data/api';

const silence = {
  id: 'silence/1',
  monitor_id: 'monitor/1',
  starts_at: '2026-01-01T00:00:00Z',
  ends_at: '2026-01-01T00:30:00Z',
  created_by: 'Arveld',
  comment: 'Maintenance',
  state: 'active' as const,
};

test('global silence reads preserve Agent and Monitor owners and reject ambiguous ownership', async () => {
  const { monitor_id: _, ...window } = silence;
  const agentSilence = {
    ...window,
    id: 'agent-silence',
    agent_instance_uid: '01000000-0000-0000-0000-000000000000',
  };
  const controller = new AbortController();
  const api = createApiClient(async (path, init) => {
    expect(path).toBe('/api/v1/silences');
    expect(init.signal).toBe(controller.signal);
    return Response.json({ silences: [agentSilence, silence] });
  });
  expect(await api.listSilences(controller.signal)).toEqual([agentSilence, silence]);
  for (const invalid of [
    window,
    { ...agentSilence, monitor_id: 'monitor/1' },
    { ...agentSilence, agent_instance_uid: '' },
    { ...agentSilence, state: 'unknown' },
    { ...agentSilence, starts_at: 'invalid' },
  ]) {
    await expect(
      createApiClient(async () => Response.json({ silences: [invalid] })).listSilences(),
    ).rejects.toThrow('unexpected response');
  }
});

test('scheduled silences share the Monitor API and appear in the global collection', async () => {
  const starts_at = new Date(Date.now() + 3600_000).toISOString();
  const input = { starts_at, duration_seconds: 1800, comment: 'Planned maintenance' };
  const values: MonitorSilence[] = [
    {
      ...silence,
      starts_at,
      ends_at: new Date(Date.parse(starts_at) + 1800_000).toISOString(),
      state: 'pending',
    },
    { ...silence, id: 'other-silence', monitor_id: 'other-monitor' },
  ];
  const controller = new AbortController();
  const api = createApiClient(async (path, init) => {
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    if (init.method === 'POST') {
      expect(path).toBe('/api/v1/monitors/monitor%2F1/silences');
      expect(JSON.parse(String(init.body))).toEqual(input);
      return Response.json({ id: silence.id, monitor_id: silence.monitor_id }, { status: 201 });
    }
    expect(path).toBe('/api/v1/silences');
    expect(init.signal).toBe(controller.signal);
    return Response.json({ silences: values });
  });
  await api.createMonitorSilence('monitor/1', input);
  expect(await api.listSilences(controller.signal)).toEqual(values);
});

test('Agent silence creation validates windows, uses its owner and rejects uncertain receipts', async () => {
  const receipt = { id: 'native-id', agent_instance_uid: 'agent/1' };
  const starts_at = new Date(Date.now() + 3600_000).toISOString();
  for (const input of [
    { duration_seconds: 1800, comment: 'Machine maintenance' },
    { duration_seconds: 3600, comment: 'Planned maintenance', starts_at },
  ]) {
    const api = createApiClient(async (path, init) => {
      expect(path).toBe('/api/v1/agents/agent%2F1/silences');
      expect(init.method).toBe('POST');
      expect(init.credentials).toBe('same-origin');
      expect(init.cache).toBe('no-store');
      expect(JSON.parse(String(init.body))).toEqual(input);
      return Response.json(receipt, { status: 201 });
    });
    expect(
      await api.createAgentSilence('agent/1', { ...input, comment: ` ${input.comment} ` }),
    ).toEqual(receipt);
  }
  let writes = 0;
  const api = createApiClient(async () => {
    writes++;
    return Response.json(receipt, { status: 201 });
  });
  for (const input of [
    { duration_seconds: 0, comment: 'Maintenance' },
    { duration_seconds: 604801, comment: 'Maintenance' },
    { duration_seconds: 60, comment: 'é'.repeat(513) },
    { duration_seconds: 60, comment: '   ' },
    { duration_seconds: 60, comment: 'Maintenance', starts_at: '2020-01-01T00:00:00Z' },
  ])
    await expect(api.createAgentSilence('agent/1', input)).rejects.toThrow();
  expect(writes).toBe(0);
  for (const invalid of [
    { ...receipt, agent_instance_uid: 'another-agent' },
    { id: 'native-id', monitor_id: 'agent/1' },
    { ...receipt, monitor_id: 'monitor' },
    { ...receipt, id: '' },
  ]) {
    await expect(
      createApiClient(async () => Response.json(invalid)).createAgentSilence('agent/1', {
        duration_seconds: 60,
        comment: 'Maintenance',
      }),
    ).rejects.toThrow('unexpected response');
  }
  for (const status of [401, 403, 404, 422, 502]) {
    let attempts = 0;
    const unavailable = createApiClient(async () => {
      attempts++;
      return new Response(null, { status });
    });
    await expect(
      unavailable.createAgentSilence('agent/1', {
        duration_seconds: 60,
        comment: 'Maintenance',
      }),
    ).rejects.toThrow();
    expect(attempts).toBe(1);
  }
});

test('Agent silence reads enforce the requested owner and preserve native states', async () => {
  const uid = '01000000-0000-0000-0000-000000000000';
  const { monitor_id: _, ...window } = silence;
  const value = { ...window, agent_instance_uid: uid };
  const controller = new AbortController();
  const api = createApiClient(async (path, init) => {
    expect(path).toBe(`/api/v1/agents/${uid}/silences`);
    expect(init.signal).toBe(controller.signal);
    expect(init.cache).toBe('no-store');
    expect(init.credentials).toBe('same-origin');
    return Response.json({ silences: [value] });
  });
  expect(await api.listAgentSilences(uid, controller.signal)).toEqual([value]);
  for (const invalid of [silence, { ...value, agent_instance_uid: 'another-agent' }]) {
    await expect(
      createApiClient(async () => Response.json({ silences: [invalid] })).listAgentSilences(uid),
    ).rejects.toThrow('unexpected response');
  }
});

test('Agent silence cancellation uses the scoped API once and preserves expiration across reads', async () => {
  const { monitor_id: _, ...window } = silence;
  const value = { ...window, agent_instance_uid: 'agent/1' };
  const calls: string[] = [];
  let state = 'active';
  const api = createApiClient(async (path, init) => {
    calls.push(`${init.method} ${path}`);
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    if (init.method === 'DELETE') {
      state = 'expired';
      return new Response(null, { status: 204 });
    }
    return Response.json({ silences: [{ ...value, state }] });
  });
  await api.cancelAgentSilence('agent/1', 'silence/1');
  expect(await api.listAgentSilences('agent/1')).toEqual([{ ...value, state: 'expired' }]);
  expect(await api.listSilences()).toEqual([{ ...value, state: 'expired' }]);
  expect(calls).toEqual([
    'DELETE /api/v1/agents/agent%2F1/silences/silence%2F1',
    'GET /api/v1/agents/agent%2F1/silences',
    'GET /api/v1/silences',
  ]);
  for (const status of [401, 403, 404, 502]) {
    let attempts = 0;
    const unavailable = createApiClient(async () => {
      attempts++;
      return new Response(null, { status });
    });
    await expect(unavailable.cancelAgentSilence('agent/1', 'silence/1')).rejects.toThrow();
    expect(attempts).toBe(1);
  }
});

test('Monitor silences use real scoped APIs and preserve native state across reads', async () => {
  const calls: string[] = [];
  const controller = new AbortController();
  let state = 'active';
  const api = createApiClient(async (path, init) => {
    calls.push(`${init.method} ${path}`);
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    if (init.method === 'POST') {
      expect(JSON.parse(String(init.body))).toEqual({
        duration_seconds: 1800,
        comment: 'Maintenance',
      });
      return Response.json({ id: silence.id, monitor_id: silence.monitor_id }, { status: 201 });
    }
    if (init.method === 'DELETE') {
      state = 'expired';
      return new Response(null, { status: 204 });
    }
    expect(init.signal).toBe(controller.signal);
    return Response.json({ silences: [{ ...silence, state }] });
  });
  expect(
    await api.createMonitorSilence('monitor/1', {
      duration_seconds: 1800,
      comment: ' Maintenance ',
    }),
  ).toEqual({ id: 'silence/1', monitor_id: 'monitor/1' });
  // These dates are in the past: the browser must still preserve the engine's active state.
  expect(await api.listMonitorSilences('monitor/1', controller.signal)).toEqual([silence]);
  await api.cancelMonitorSilence('monitor/1', 'silence/1');
  expect(await api.listMonitorSilences('monitor/1', controller.signal)).toEqual([
    { ...silence, state: 'expired' },
  ]);
  expect(calls).toEqual([
    'POST /api/v1/monitors/monitor%2F1/silences',
    'GET /api/v1/monitors/monitor%2F1/silences',
    'DELETE /api/v1/monitors/monitor%2F1/silences/silence%2F1',
    'GET /api/v1/monitors/monitor%2F1/silences',
  ]);
});

test('Monitor silence reads reject unavailable or invalid data instead of showing an empty list', async () => {
  for (const body of [
    { silences: null },
    { silences: [{ ...silence, monitor_id: 'another-monitor' }] },
    { silences: [{ ...silence, state: 'unknown' }] },
    { silences: [{ ...silence, ends_at: 'invalid date' }] },
    { silences: [{ id: silence.id }] },
  ]) {
    await expect(
      createApiClient(async () => Response.json(body)).listMonitorSilences('monitor/1'),
    ).rejects.toThrow('unexpected response');
  }
  await expect(
    createApiClient(async () =>
      Response.json({ id: 'native-id', monitor_id: 'another-monitor' }),
    ).createMonitorSilence('monitor/1', { duration_seconds: 60, comment: 'Maintenance' }),
  ).rejects.toThrow('unexpected response');
  expect(
    await createApiClient(async () => Response.json({ silences: [] })).listMonitorSilences(
      'monitor/1',
    ),
  ).toEqual([]);
  for (const status of [401, 403, 404, 502]) {
    let calls = 0;
    const api = createApiClient(async () => {
      calls++;
      return new Response(null, { status });
    });
    await expect(api.listMonitorSilences('monitor/1')).rejects.toThrow();
    await expect(
      api.createMonitorSilence('monitor/1', { duration_seconds: 60, comment: 'Maintenance' }),
    ).rejects.toThrow();
    await expect(api.cancelMonitorSilence('monitor/1', 'silence/1')).rejects.toThrow();
    expect(calls).toBe(3);
  }
});

test('Monitor silence creation validates duration and UTF-8 comments before sending', async () => {
  let calls = 0;
  const api = createApiClient(async () => {
    calls++;
    return Response.json({ id: silence.id, monitor_id: silence.monitor_id }, { status: 201 });
  });
  for (const input of [
    { duration_seconds: 60, comment: 'Maintenance', starts_at: '2020-01-01T00:00:00Z' },
    { duration_seconds: 60, comment: 'Maintenance', starts_at: 'tomorrow' },
    { duration_seconds: 60, comment: 'Maintenance', starts_at: '2030-01-01T12:00:00' },
    { duration_seconds: 60, comment: 'Maintenance', starts_at: '9999-12-31T23:59:59Z' },
    { duration_seconds: 0, comment: 'Maintenance' },
    { duration_seconds: 1.5, comment: 'Maintenance' },
    { duration_seconds: 604801, comment: 'Maintenance' },
    { duration_seconds: 60, comment: '  ' },
    { duration_seconds: 60, comment: 'Null\0byte' },
    { duration_seconds: 60, comment: 'é'.repeat(513) },
  ])
    await expect(api.createMonitorSilence('monitor/1', input)).rejects.toThrow();
  expect(calls).toBe(0);
  await api.createMonitorSilence('monitor/1', {
    duration_seconds: 604800,
    comment: 'é'.repeat(512),
  });
  expect(calls).toBe(1);
});
