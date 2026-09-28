import { expect, test } from 'bun:test';
import { createPreferencesStore, PREFERENCES_KEY } from '../src/data/preferences';

test('browser preferences migrate independently of obsolete demo data without rewriting the legacy snapshot', () => {
  for (const key of ['arveld.frontend.v1', 'arveld.demo.v1']) {
    const legacy = JSON.stringify({
      version: 1,
      settings: { name: 'Retained workspace', refresh: '60', retention: '90d', compact: true },
      agents: 'obsolete schema',
      checks: [{ id: 'custom-demo-monitor' }],
      account: { email: 'old-demo@example.test' },
      accessKeys: [{ token: 'old-demo-token' }],
    });
    const values = new Map([[key, legacy]]);
    const storage = {
      getItem: (name: string) => values.get(name) ?? null,
      setItem: (name: string, value: string) => {
        values.set(name, value);
      },
    };
    const store = createPreferencesStore(storage);
    expect(store.recovered).toBe(false);
    expect(store.read()).toEqual({ name: 'Retained workspace', refresh: '60' });
    expect(JSON.parse(values.get(PREFERENCES_KEY) ?? '{}')).toEqual({
      name: 'Retained workspace',
      refresh: '60',
    });
    expect(values.get(key)).toBe(legacy);
    expect(store.save({ name: 'New workspace', refresh: 'off' })).toEqual({
      name: 'New workspace',
      refresh: 'off',
    });
    expect(createPreferencesStore(storage).read()).toEqual({
      name: 'New workspace',
      refresh: 'off',
    });
    expect(values.get(key)).toBe(legacy);
  }
});

test('current preferences take priority over legacy data and snapshots cannot mutate the store', () => {
  const store = createPreferencesStore({
    getItem: (key) => {
      if (key !== PREFERENCES_KEY) throw new Error('Legacy data must not be read again');
      return JSON.stringify({ name: 'Current workspace', refresh: 'off' });
    },
    setItem: () => {
      throw new Error('A current preference read must not write storage');
    },
  });
  expect(store.recovered).toBe(false);
  const snapshot = store.read();
  snapshot.name = 'Changed by caller';
  expect(store.read()).toEqual({ name: 'Current workspace', refresh: 'off' });
});

test('missing preferences start cleanly and invalid or blocked storage reports recovery', () => {
  const defaults = { name: 'My infrastructure', refresh: '30' } as const;
  const fresh = createPreferencesStore({ getItem: () => null, setItem: () => {} });
  expect(fresh.recovered).toBe(false);
  expect(fresh.read()).toEqual(defaults);
  for (const saved of ['{broken', 'null', '{}', '{"name":"","refresh":"30"}']) {
    const store = createPreferencesStore({ getItem: () => saved, setItem: () => {} });
    expect(store.recovered).toBe(true);
    expect(store.read()).toEqual(defaults);
  }
  const blocked = createPreferencesStore({
    getItem: () => {
      throw new Error('Storage blocked');
    },
    setItem: () => {},
  });
  expect(blocked.recovered).toBe(true);
  expect(blocked.read()).toEqual(defaults);
});

test('failed or invalid preference saves preserve the last successful value', () => {
  const values = new Map<string, string>();
  const storage = {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => {
      values.set(key, value);
    },
  };
  const store = createPreferencesStore(storage);
  const saved = store.save({ name: '  Saved workspace  ', refresh: '15' });
  expect(saved).toEqual({ name: 'Saved workspace', refresh: '15' });
  expect(() => store.save({ name: ' ', refresh: '30' })).toThrow();
  storage.setItem = () => {
    throw new Error('Quota exceeded');
  };
  expect(() => store.save({ name: 'Unsaved workspace', refresh: '60' })).toThrow(
    'Unable to save preferences',
  );
  expect(store.read()).toEqual(saved);
  expect(createPreferencesStore(storage).read()).toEqual(saved);
  saved.name = 'Changed by caller';
  expect(store.read().name).toBe('Saved workspace');
});

test('migration write failures retain readable legacy preferences', () => {
  const legacy = JSON.stringify({
    settings: { name: 'Legacy workspace', refresh: 'off' },
    checks: [],
  });
  const store = createPreferencesStore({
    getItem: (key) => (key === 'arveld.frontend.v1' ? legacy : null),
    setItem: () => {
      throw new Error('Quota exceeded');
    },
  });
  expect(store.recovered).toBe(true);
  expect(store.read()).toEqual({ name: 'Legacy workspace', refresh: 'off' });
});
