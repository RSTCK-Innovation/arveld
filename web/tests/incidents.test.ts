import { expect, test } from 'bun:test';
import { createApiClient, type Incident } from '../src/data/api';

test('incident history reads durable pages and acknowledges through the API', async () => {
  const incident: Incident = {
    id: '12',
    rule_id: 'rule',
    owner_kind: 'monitor',
    owner_id: 'monitor',
    owner_name: 'Checkout API',
    condition: 'latency',
    threshold: 500,
    for_seconds: 60,
    severity: 'warning',
    status: 'open',
    opened_at: '2026-09-19T13:00:00Z',
    last_evaluated_at: '2026-09-19T13:00:05Z',
  };
  const calls: string[] = [];
  const api = createApiClient(async (path, init) => {
    calls.push(`${init.method} ${path}`);
    if (path.endsWith('/acknowledgment'))
      return Response.json({
        ...incident,
        acknowledged_at: '2026-09-19T13:01:00Z',
        acknowledged_by: 'Operator',
      });
    if (path.includes('?'))
      return Response.json({ incidents: [incident], total: 8, next_before: '12' });
    return Response.json(incident);
  });
  expect(await api.listIncidents({ status: 'open', limit: 1 })).toEqual({
    incidents: [incident],
    total: 8,
    next_before: '12',
  });
  expect(await api.incident('12')).toEqual(incident);
  expect((await api.acknowledgeIncident('12')).acknowledged_by).toBe('Operator');
  expect(calls).toEqual([
    'GET /api/v1/incidents?status=open&limit=1',
    'GET /api/v1/incidents/12',
    'POST /api/v1/incidents/12/acknowledgment',
  ]);
  await expect(
    createApiClient(async () => new Response(null, { status: 503 })).listIncidents(),
  ).rejects.toThrow('unavailable');
  expect(
    await createApiClient(async () => new Response(null, { status: 404 })).incident('12'),
  ).toBeNull();
});
