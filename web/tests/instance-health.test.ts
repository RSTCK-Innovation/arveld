import { expect, test } from 'bun:test';
import { createApiClient } from '../src/data/api';

test('instance health preserves component readiness on HTTP 200 and 503 without inventing state on errors', async () => {
  const signal = new AbortController().signal;
  for (const prometheus of [true, false]) {
    for (const alertmanager of [true, false]) {
      const readiness = {
        version: 'v0.1.0-rc.2',
        ready: prometheus && alertmanager,
        components: {
          arveld: { ready: true },
          prometheus: { ready: prometheus },
          alertmanager: { ready: alertmanager },
        },
      };
      const api = createApiClient(async (path, init) => {
        expect(path).toBe('/readyz');
        expect(init.method).toBe('GET');
        expect(init.credentials).toBe('same-origin');
        expect(init.cache).toBe('no-store');
        expect(init.signal).toBe(signal);
        return Response.json(readiness, { status: readiness.ready ? 200 : 503 });
      });
      expect(await api.readiness(signal)).toEqual(readiness);
    }
  }

  for (const response of [
    Response.json({ ready: true }),
    Response.json({ ready: false, components: {} }, { status: 503 }),
    new Response('<html>proxy failure</html>', { status: 503 }),
  ]) {
    const malformed = createApiClient(async () => response);
    await expect(malformed.readiness()).rejects.toThrow('Arveld returned an unexpected response.');
  }
  const failed = createApiClient(async () => new Response('private details', { status: 502 }));
  await expect(failed.readiness()).rejects.toThrow('Arveld is unavailable. Try again.');
  const offline = createApiClient(async () => {
    throw new TypeError('Failed to fetch');
  });
  await expect(offline.readiness()).rejects.toThrow(
    'Unable to reach Arveld. Check your connection.',
  );

  const controller = new AbortController();
  controller.abort();
  const aborted = createApiClient(async (_path, init) => {
    init.signal?.throwIfAborted();
    throw new Error('Unexpected request');
  });
  await expect(aborted.readiness(controller.signal)).rejects.toMatchObject({ name: 'AbortError' });
});
