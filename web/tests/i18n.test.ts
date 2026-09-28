import { describe, expect, test } from 'bun:test';
import { readdirSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { AppError } from '../src/data/errors';
import { createI18n, i18n } from '../src/i18n';
import { formatDate, formatNumber, formatPercent, setFormatLocale } from '../src/i18n/format';
import { languagePreference, resolveLanguage } from '../src/i18n/language';
import en from '../src/i18n/locales/en.json';
import fr from '../src/i18n/locales/fr.json';
import { errorMessage } from '../src/i18n/messages';

describe('language selection', () => {
  test('respects browser priority and regional variants', () => {
    expect(resolveLanguage('auto', ['fr-CA', 'en-US'])).toEqual({
      language: 'fr',
      locale: 'fr-CA',
    });
    expect(resolveLanguage('auto', ['en-GB', 'fr'])).toEqual({ language: 'en', locale: 'en-GB' });
    expect(resolveLanguage('auto', ['de-DE', 'fr-FR', 'en'])).toEqual({
      language: 'fr',
      locale: 'fr-FR',
    });
    expect(resolveLanguage('auto', ['FR-ca'])).toEqual({ language: 'fr', locale: 'fr-CA' });
  });
  test('explicit preferences take priority and missing or invalid languages fall back', () => {
    expect(resolveLanguage('en', ['fr-FR']).language).toBe('en');
    expect(resolveLanguage('fr', ['en-US']).language).toBe('fr');
    for (const languages of [[], ['de-DE'], ['not_a_language', '']]) {
      expect(resolveLanguage('auto', languages)).toEqual({ language: 'en', locale: 'en-US' });
    }
    for (const value of [null, 'de', 'corrupted', 'auto'])
      expect(languagePreference(value)).toBe('auto');
    expect(languagePreference('fr')).toBe('fr');
  });
});

describe('catalogs and formatting', () => {
  test('both catalogs are complete and preserve matching placeholders', () => {
    expect(Object.keys(en).sort()).toEqual(Object.keys(fr).sort());
    const placeholders = (text: string) =>
      [...text.matchAll(/{{\s*([^},]+)(?:,[^}]+)?}}/g)].map((m) => m[1]).sort();
    for (const key of Object.keys(fr) as (keyof typeof fr)[]) {
      expect(en[key].trim().length, key).toBeGreaterThan(0);
      expect(placeholders(en[key]), key).toEqual(placeholders(fr[key]));
    }
  });
  test('all literal translation keys used by screens exist in the catalogs', () => {
    function files(directory: string): string[] {
      return readdirSync(directory, { withFileTypes: true }).flatMap((entry) =>
        entry.isDirectory()
          ? files(join(directory, entry.name))
          : /\.tsx?$/.test(entry.name)
            ? [join(directory, entry.name)]
            : [],
      );
    }
    const missing: string[] = [];
    for (const file of files(join(import.meta.dir, '../src'))) {
      for (const [, , key] of readFileSync(file, 'utf8').matchAll(/\bt\(\s*(['"])(.*?)\1/gs)) {
        if (!(key in en) && !(`${key}_one` in en)) missing.push(`${file}: ${key}`);
      }
    }
    expect(missing).toEqual([]);
  });
  test('pluralizes complete sentences in French and English', () => {
    const instance = createI18n('en');
    expect(instance.t('{{count}} agent', { count: 0 })).toBe('0 agents');
    expect(instance.t('{{count}} agent', { count: 1 })).toBe('1 agent');
    expect(instance.t('{{count}} agent', { count: 2 })).toBe('2 agents');
    void instance.changeLanguage('fr');
    expect(instance.t('{{count}} healthy service', { count: 1 })).toBe(
      fr['{{count}} healthy service_one'].replace('{{count}}', '1'),
    );
    expect(instance.t('{{count}} healthy service', { count: 2 })).toBe(
      fr['{{count}} healthy service_other'].replace('{{count}}', '2'),
    );
  });
  test('formats measurements and dates without changing their underlying values', () => {
    setFormatLocale('fr-FR');
    expect(formatNumber(1234.5)).toBe('1 234,5');
    expect(formatPercent(99.98)).toBe('99,98 %');
    expect(formatDate('2026-09-05T12:00:00Z', { timeZone: 'UTC' })).toBe('05/09/2026');
    setFormatLocale('en-GB');
    expect(formatDate('2026-09-05T12:00:00Z', { timeZone: 'UTC' })).toBe('05/09/2026');
    setFormatLocale('en-US');
    expect(formatNumber(1234.5)).toBe('1,234.5');
    expect(formatPercent(99.98)).toBe('99.98%');
  });
});

test('translates errors after language changes without changing their source values', () => {
  const error = new AppError('Invalid YAML: {{detail}}', { detail: 'unexpected token' });
  const plain = new Error('Custom message 🔧');
  void i18n.changeLanguage('en');
  expect(errorMessage(error)).toBe('Invalid YAML: unexpected token');
  expect(errorMessage(plain)).toBe(plain.message);
  void i18n.changeLanguage('fr');
  expect(errorMessage(error)).toBe(
    fr['Invalid YAML: {{detail}}'].replace('{{detail}}', 'unexpected token'),
  );
  expect(errorMessage(plain)).toBe(plain.message);
  expect(error.message).toBe('Invalid YAML: {{detail}}');
  expect(error.values).toEqual({ detail: 'unexpected token' });
});
