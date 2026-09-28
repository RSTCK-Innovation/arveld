import { expect, test } from 'bun:test';
import { createApiClient } from '../src/data/api';

test('instance retention preserves the engine policy and never substitutes browser defaults on failure', async () => {
  const signal = new AbortController().signal;
  for (const policy of ['15d', '2w or 1GiB', '1GiB', '']) {
    const api = createApiClient(async (path, init) => {
      expect(path).toBe('/api/v1/settings/retention');
      expect(init.method).toBe('GET');
      expect(init.credentials).toBe('same-origin');
      expect(init.cache).toBe('no-store');
      expect(init.signal).toBe(signal);
      return Response.json({ storage_retention: policy });
    });
    expect(await api.retention(signal)).toEqual({ storage_retention: policy });
  }
  for (const data of [{}, { storage_retention: null }, { storage_retention: 15 }]) {
    const api = createApiClient(async () => Response.json(data));
    await expect(api.retention()).rejects.toThrow('Arveld returned an unexpected response.');
  }
  const failed = createApiClient(async () => new Response('private details', { status: 502 }));
  await expect(failed.retention()).rejects.toThrow('Arveld is unavailable. Try again.');
});
