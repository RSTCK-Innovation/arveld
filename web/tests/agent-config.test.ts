import { expect, test } from 'bun:test';
import { createApiClient } from '../src/data/api';

test('configuration history reads metadata and exact YAML for the selected revision', async () => {
  const uid = '0198f1ad-6f2a-7a4b-8c3d-123456789abc';
  const revisions = [
    { revision: 2, created_at: '2026-09-10T12:00:00Z' },
    { revision: 1, created_at: null },
  ];
  // biome-ignore lint/suspicious/noTemplateCurlyInString: Collector placeholders must remain literal.
  const yaml = '# Kept verbatim\nendpoint: "${env:ARVELD_URL}"\n';
  const controller = new AbortController();
  const requests: string[] = [];
  const api = createApiClient(async (path, init) => {
    requests.push(path);
    expect(init.method).toBe('GET');
    expect(init.signal).toBe(controller.signal);
    expect(init.credentials).toBe('same-origin');
    return path.endsWith('/2')
      ? new Response(yaml, { headers: { 'Content-Type': 'application/x-yaml' } })
      : Response.json({ revisions });
  });
  expect(await api.agentConfigRevisions(uid, controller.signal)).toEqual(revisions);
  expect(await api.agentConfigYAML(uid, 2, controller.signal)).toBe(yaml);
  expect(requests).toEqual([
    `/api/v1/agents/${uid}/config/revisions`,
    `/api/v1/agents/${uid}/config/revisions/2`,
  ]);
  const missing = createApiClient(async () => new Response('', { status: 404 }));
  await expect(missing.agentConfigYAML(uid, 99)).rejects.toMatchObject({ status: 404 });
});

test('configuration status preserves desired, reported, working and failed revisions independently', async () => {
  const uid = '0198f1ad-6f2a-7a4b-8c3d-123456789abc';
  const report = {
    revision: 1,
    config_hash: 'a'.repeat(64),
    status: 'applied',
    error_message: '',
    reported_at: '2026-09-09T10:00:00Z',
  } as const;
  const status = {
    state: 'applying',
    desired: { revision: 2, config_hash: 'b'.repeat(64) },
    reported: report,
  } as const;
  const controller = new AbortController();
  const api = createApiClient(async (path, init) => {
    expect(path).toBe(`/api/v1/agents/${uid}/config/status`);
    expect(init.method).toBe('GET');
    expect(init.credentials).toBe('same-origin');
    expect(init.cache).toBe('no-store');
    expect(init.signal).toBe(controller.signal);
    return Response.json(status);
  });
  expect(await api.agentConfigStatus(uid, controller.signal)).toEqual(status);

  for (const response of [
    { ...status, reported: null },
    { ...status, reported: { ...report, revision: null, config_hash: '' } },
    {
      state: 'applied',
      desired: { revision: 1, config_hash: report.config_hash },
      reported: report,
      last_failure: { ...report, revision: 2, status: 'failed', error_message: 'Invalid receiver' },
    },
    {
      state: 'failed',
      desired: { revision: 3, config_hash: 'c'.repeat(64) },
      reported: report,
      last_working: report,
      last_failure: {
        ...report,
        revision: 3,
        config_hash: 'c'.repeat(64),
        status: 'failed',
        error_message: 'Invalid receiver',
      },
    },
  ] as const) {
    const client = createApiClient(async () => Response.json(response));
    expect(await client.agentConfigStatus(uid)).toEqual(response);
  }

  const unassigned = createApiClient(async () => new Response(null, { status: 404 }));
  expect(await unassigned.agentConfigStatus(uid)).toBeNull();
  const failed = createApiClient(
    async () => new Response('private database details', { status: 500 }),
  );
  await expect(failed.agentConfigStatus(uid)).rejects.toThrow('Arveld is unavailable. Try again.');
  const unauthorized = createApiClient(async () => new Response(null, { status: 401 }));
  await expect(unauthorized.agentConfigStatus(uid)).rejects.toMatchObject({ status: 401 });
  const malformed = createApiClient(async () => Response.json({ ...status, state: 'ready' }));
  await expect(malformed.agentConfigStatus(uid)).rejects.toThrow(
    'Arveld returned an unexpected response.',
  );
});
