import { expect, test } from 'bun:test';
import { type ApiKey, createApiClient } from '../src/data/api';

test('agent keys use the controller for creation, listing and revocation without expiration', async () => {
  const agentKey = {
    id: 'agent/key',
    name: 'Agent Paris',
    prefix: 'arv_agent_public',
    createdAt: '2026-09-08T12:00:00Z',
    revokedAt: null,
  };
  const revoked = { ...agentKey, revokedAt: '2026-09-08T12:01:00Z' };
  const replies = [
    Response.json({ key: agentKey, token: 'arv_agent_public_secret' }, { status: 201 }),
    Response.json({ keys: [agentKey] }),
    new Response(null, { status: 204 }),
    Response.json({ keys: [revoked] }),
  ];
  const requests: { path: string; method: string; body: unknown }[] = [];
  const controller = new AbortController();
  const api = createApiClient(async (path, init) => {
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    if (init.method === 'GET') expect(init.signal).toBe(controller.signal);
    requests.push({
      path,
      method: init.method ?? 'GET',
      body: init.body ? JSON.parse(String(init.body)) : null,
    });
    const reply = replies.shift();
    if (!reply) throw new Error('Unexpected request');
    return reply;
  });
  expect(await api.createAgentKey({ name: '  Agent Paris  ' })).toEqual({
    key: agentKey,
    token: 'arv_agent_public_secret',
  });
  expect(await api.listAgentKeys(controller.signal)).toEqual([agentKey]);
  await api.revokeAgentKey(agentKey.id);
  expect(await api.listAgentKeys(controller.signal)).toEqual([revoked]);
  expect(requests).toEqual([
    { path: '/api/v1/agentkeys', method: 'POST', body: { name: 'Agent Paris' } },
    { path: '/api/v1/agentkeys', method: 'GET', body: null },
    { path: '/api/v1/agentkeys/agent%2Fkey', method: 'DELETE', body: null },
    { path: '/api/v1/agentkeys', method: 'GET', body: null },
  ]);

  const failed = createApiClient(
    async () => new Response('private database details', { status: 500 }),
  );
  await expect(failed.createAgentKey({ name: 'Agent Paris' })).rejects.toThrow(
    'Arveld is unavailable. Try again.',
  );
  await expect(failed.listAgentKeys()).rejects.toThrow('Arveld is unavailable. Try again.');
  await expect(failed.revokeAgentKey('agent/key')).rejects.toThrow(
    'Arveld is unavailable. Try again.',
  );
  const malformed = createApiClient(async () => Response.json({ key: agentKey, token: null }));
  await expect(malformed.createAgentKey({ name: 'Agent Paris' })).rejects.toThrow(
    'Arveld returned an unexpected response.',
  );
  const leaked = createApiClient(async () =>
    Response.json({ keys: [{ ...agentKey, token: 'secret' }] }),
  );
  await expect(leaked.listAgentKeys()).rejects.toThrow('Arveld returned an unexpected response.');
  await expect(api.createAgentKey({ name: '🔑' })).rejects.toThrow(
    'The key name must contain 2 to 80 characters.',
  );
  expect(requests).toHaveLength(4);
});

const key: ApiKey = {
  id: 'server-key',
  name: 'Automation',
  permission: 'read',
  prefix: 'arv_server-key',
  createdAt: '2026-09-07T12:00:00Z',
  expiresAt: null,
  revokedAt: null,
};

test('API keys display the server secret once and use persisted metadata for listing and revocation', async () => {
  const replies = [
    Response.json({ key, token: 'arv_server-key_server-secret' }, { status: 201 }),
    Response.json({ keys: [key] }),
    new Response(null, { status: 204 }),
    Response.json({ keys: [{ ...key, revokedAt: '2026-09-07T12:01:00Z' }] }),
  ];
  const requests: { path: string; method: string; body: unknown }[] = [];
  const api = createApiClient(async (path, init) => {
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    requests.push({
      path,
      method: init.method ?? 'GET',
      body: init.body ? JSON.parse(String(init.body)) : null,
    });
    const reply = replies.shift();
    if (!reply) throw new Error('Unexpected request');
    return reply;
  });
  expect(
    await api.createKey({ name: 'Automation', permission: 'read', expiresDays: null }),
  ).toEqual({ key, token: 'arv_server-key_server-secret' });
  expect(await api.listKeys()).toEqual([key]);
  await api.revokeKey('server-key');
  expect(await api.listKeys()).toEqual([{ ...key, revokedAt: '2026-09-07T12:01:00Z' }]);
  expect(requests).toEqual([
    {
      path: '/api/v1/apikeys',
      method: 'POST',
      body: { name: 'Automation', permission: 'read', expiresDays: null },
    },
    { path: '/api/v1/apikeys', method: 'GET', body: null },
    { path: '/api/v1/apikeys/server-key', method: 'DELETE', body: null },
    { path: '/api/v1/apikeys', method: 'GET', body: null },
  ]);
});

test('API key requests preserve one-day validity and reject unsuccessful or malformed responses', async () => {
  let requestBody: unknown;
  const api = createApiClient(async (_, init) => {
    requestBody = JSON.parse(String(init.body));
    return Response.json(
      { key: { ...key, expiresAt: '2026-09-08T12:00:00Z' }, token: 'arv_server-key_daily-secret' },
      { status: 201 },
    );
  });
  const created = await api.createKey({ name: 'Daily', permission: 'write', expiresDays: 1 });
  expect(requestBody).toEqual({ name: 'Daily', permission: 'write', expiresDays: 1 });
  expect(created.key.expiresAt).toBe('2026-09-08T12:00:00Z');
  const failed = createApiClient(
    async () => new Response('private database details', { status: 500 }),
  );
  await expect(
    failed.createKey({ name: 'Daily', permission: 'read', expiresDays: 1 }),
  ).rejects.toThrow('Arveld is unavailable. Try again.');
  const malformed = createApiClient(async () => Response.json({ key, token: null }));
  await expect(
    malformed.createKey({ name: 'Daily', permission: 'read', expiresDays: 1 }),
  ).rejects.toThrow('Arveld returned an unexpected response.');
});
