import { expect, test } from 'bun:test';
import { createApiClient } from '../src/data/api';

test('agent inventory reads controller presence and preserves unknown metadata without demo fallback', async () => {
  const agents = [
    {
      instance_uid: '0198f1ad-6f2a-7a4b-8c3d-123456789abc',
      hostname: 'lab-paris',
      version: '0.159.0',
      connected: true,
      last_seen_at: '2026-09-08T18:00:00Z',
    },
    {
      instance_uid: '0198f1ad-6f2a-7a4b-8c3d-123456789abd',
      hostname: null,
      version: null,
      connected: false,
      last_seen_at: null,
    },
  ];
  const controller = new AbortController();
  const api = createApiClient(async (path, init) => {
    expect(path).toBe('/api/v1/agents');
    expect(init.method).toBe('GET');
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    expect(init.signal).toBe(controller.signal);
    return Response.json({ agents });
  });
  expect(await api.listAgents(controller.signal)).toEqual(agents);

  const empty = createApiClient(async () => Response.json({ agents: [] }));
  expect(await empty.listAgents()).toEqual([]);

  const failed = createApiClient(
    async () => new Response('private database details', { status: 500 }),
  );
  await expect(failed.listAgents()).rejects.toThrow('Arveld is unavailable. Try again.');
  const unauthorized = createApiClient(async () => new Response(null, { status: 401 }));
  await expect(unauthorized.listAgents()).rejects.toMatchObject({ status: 401 });
  const malformed = createApiClient(async () =>
    Response.json({ agents: [{ ...agents[0], connected: 'true' }] }),
  );
  await expect(malformed.listAgents()).rejects.toThrow('Arveld returned an unexpected response.');
});
