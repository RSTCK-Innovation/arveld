import { z } from 'zod';

export const PREFERENCES_KEY = 'arveld.preferences.v1';
const preferencesSchema = z.object({
  name: z.string().trim().min(2).max(50),
  refresh: z.enum(['off', '15', '30', '60']),
});
export type Preferences = z.infer<typeof preferencesSchema>;
type Storage = Pick<globalThis.Storage, 'getItem' | 'setItem'>;

export function createPreferencesStore(storage: Storage) {
  let preferences: Preferences = { name: 'My infrastructure', refresh: '30' };
  let recovered = false;
  try {
    const saved = storage.getItem(PREFERENCES_KEY);
    if (saved !== null) {
      preferences = preferencesSchema.parse(JSON.parse(saved));
    } else {
      const legacy = storage.getItem('arveld.frontend.v1') ?? storage.getItem('arveld.demo.v1');
      if (legacy !== null) {
        preferences = preferencesSchema.parse(JSON.parse(legacy).settings);
        // Copy preferences only; leave the retired snapshot intact for recovery.
        storage.setItem(PREFERENCES_KEY, JSON.stringify(preferences));
      }
    }
  } catch {
    recovered = true;
  }
  return {
    recovered,
    read: () => ({ ...preferences }),
    save(input: Preferences) {
      const next = preferencesSchema.parse(input);
      try {
        storage.setItem(PREFERENCES_KEY, JSON.stringify(next));
      } catch {
        throw new Error('Unable to save preferences. Check browser storage.');
      }
      preferences = next;
      return { ...preferences };
    },
  };
}
