import { expect, test } from 'bun:test';
import { createApiClient } from '../src/data/api';

test('HTTP Monitors are created and read through the controller without demo fallback', async () => {
  const input = {
    name: 'Homepage',
    agent_instance_uid: '01000000-0000-0000-0000-000000000000',
    endpoint: 'https://example.com/health',
    method: 'POST' as const,
    body: '{"ready":true}',
    headers: { Authorization: 'Bearer synthetic-token' },
    skip_tls_verify: true,
    validations: [{ type: 'json_path' as const, path: 'ready', equals: 'true' }],
    interval_seconds: 30,
    timeout_seconds: 5,
  };
  const value = { id: 'persisted-id', ...input };
  const controller = new AbortController();
  const requests: string[] = [];
  const api = createApiClient(async (path, init) => {
    requests.push(`${init.method} ${path}`);
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    if (init.method === 'POST') {
      expect(JSON.parse(String(init.body))).toEqual(input);
      return Response.json(value, { status: 201 });
    }
    expect(init.signal).toBe(controller.signal);
    return Response.json(path.endsWith('/persisted-id') ? value : { monitors: [value] });
  });
  expect(await api.createHTTPMonitor(input)).toEqual(value);
  expect(await api.listHTTPMonitors(controller.signal)).toEqual([value]);
  expect(await api.httpMonitor('persisted-id', controller.signal)).toEqual(value);
  expect(requests).toEqual([
    'POST /api/v1/monitors/http',
    'GET /api/v1/monitors/http',
    'GET /api/v1/monitors/http/persisted-id',
  ]);
  expect(
    await createApiClient(async () => Response.json({ monitors: [] })).listHTTPMonitors(),
  ).toEqual([]);
  expect(
    await createApiClient(async () => new Response(null, { status: 404 })).httpMonitor('missing'),
  ).toBeNull();
  await expect(
    createApiClient(async () => new Response(null, { status: 401 })).listHTTPMonitors(),
  ).rejects.toMatchObject({ status: 401 });
  await expect(
    createApiClient(async () =>
      Response.json({ monitors: [{ ...value, interval_seconds: '30' }] }),
    ).listHTTPMonitors(),
  ).rejects.toThrow('Arveld returned an unexpected response.');
  await expect(
    createApiClient(async () => new Response('private failure', { status: 500 })).createHTTPMonitor(
      input,
    ),
  ).rejects.toThrow('Arveld is unavailable. Try again.');
});
