import { expect, test } from 'bun:test';
import { createApiClient } from '../src/data/api';

test('Monitor deletion uses the authenticated API and preserves failures', async () => {
  const requests: string[] = [];
  const api = createApiClient(async (path, init) => {
    requests.push(path);
    expect(init.method).toBe('DELETE');
    expect(init.credentials).toBe('same-origin');
    expect(init.body).toBeUndefined();
    return new Response(null, { status: requests.length === 1 ? 204 : 503 });
  });
  await api.deleteMonitor('monitor/id');
  expect(requests).toEqual(['/api/v1/monitors/monitor%2Fid']);
  await expect(api.deleteMonitor('unavailable')).rejects.toMatchObject({ status: 503 });
});

test('Monitor updates preserve identity, assignment and protocol-specific settings through PUT', async () => {
  const input = {
    protocol: 'dns' as const,
    name: 'Moved resolver',
    agent_instance_uid: '02000000-0000-0000-0000-000000000000',
    endpoint: 'example.net',
    dns_server: '8.8.8.8:53',
    record_type: 'AAAA' as const,
    transport: 'tcp' as const,
    interval_seconds: 60,
    timeout_seconds: 10,
  };
  const api = createApiClient(async (path, init) => {
    expect(path).toBe('/api/v1/monitors/existing%2Fid');
    expect(init.method).toBe('PUT');
    expect(init.credentials).toBe('same-origin');
    const { protocol, ...body } = input;
    expect(JSON.parse(String(init.body))).toEqual(body);
    return Response.json({ ...input, protocol, id: 'existing/id' });
  });
  expect(await api.updateMonitor('existing/id', input)).toEqual({ ...input, id: 'existing/id' });
  for (const status of [401, 404, 422, 503]) {
    await expect(
      createApiClient(async () => new Response(null, { status })).updateMonitor(
        'existing/id',
        input,
      ),
    ).rejects.toMatchObject({ status });
  }
});

test('all Monitor protocols use persisted API definitions and strict protocol-specific responses', async () => {
  const common = {
    name: 'Service',
    agent_instance_uid: '01000000-0000-0000-0000-000000000000',
    interval_seconds: 30,
    timeout_seconds: 5,
  };
  const inputs = [
    {
      ...common,
      protocol: 'http' as const,
      endpoint: 'https://example.com',
      method: 'HEAD' as const,
    },
    { ...common, protocol: 'tcp' as const, endpoint: 'localhost:5432' },
    { ...common, protocol: 'icmp' as const, endpoint: '127.0.0.1', ping_count: 3 },
    {
      ...common,
      protocol: 'dns' as const,
      endpoint: 'example.com',
      dns_server: '1.1.1.1:53',
      record_type: 'AAAA' as const,
      transport: 'udp' as const,
    },
  ];
  const values = inputs.map((input) => ({ ...input, id: input.protocol }));
  const controller = new AbortController();
  const api = createApiClient(async (path, init) => {
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    if (init.method === 'POST') {
      const input = inputs.find((input) => path === `/api/v1/monitors/${input.protocol}`);
      expect(input).toBeDefined();
      if (!input) throw new Error('Unknown protocol endpoint');
      const { protocol, ...body } = input;
      expect(JSON.parse(String(init.body))).toEqual(body);
      return Response.json(
        protocol === 'http' ? { ...body, id: protocol } : { ...input, id: protocol },
        { status: 201 },
      );
    }
    expect(init.signal).toBe(controller.signal);
    return Response.json(path === '/api/v1/monitors' ? { monitors: values } : values[2]);
  });
  for (const input of inputs)
    expect(await api.createMonitor(input)).toEqual({ ...input, id: input.protocol });
  expect(await api.listMonitors(controller.signal)).toEqual(values);
  expect(await api.monitor('icmp', controller.signal)).toEqual(values[2]);
  expect(
    await createApiClient(async () => new Response(null, { status: 404 })).monitor('missing'),
  ).toBeNull();
  await expect(
    createApiClient(async () => new Response(null, { status: 401 })).listMonitors(),
  ).rejects.toMatchObject({ status: 401 });
  await expect(
    createApiClient(async () =>
      Response.json({ monitors: [{ ...values[1], ping_count: 3 }] }),
    ).listMonitors(),
  ).rejects.toThrow('Arveld returned an unexpected response.');
  await expect(
    createApiClient(async () =>
      Response.json({ monitors: [{ ...values[3], record_type: 'INVALID' }] }),
    ).listMonitors(),
  ).rejects.toThrow('Arveld returned an unexpected response.');
});
