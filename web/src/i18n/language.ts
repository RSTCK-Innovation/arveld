export const supportedLanguages = ['en', 'fr'] as const;
export type Language = (typeof supportedLanguages)[number];
export type LanguagePreference = Language | 'auto';
export const LANGUAGE_KEY = 'arveld.language';

export function languagePreference(value: unknown): LanguagePreference {
  return value === 'en' || value === 'fr' ? value : 'auto';
}

// Respect the browser's ordered list, including regional language tags.
export function resolveLanguage(
  preference: LanguagePreference,
  browserLanguages: readonly string[],
): { language: Language; locale: string } {
  if (preference !== 'auto') {
    return { language: preference, locale: preference === 'fr' ? 'fr-FR' : 'en-US' };
  }
  for (const tag of browserLanguages) {
    try {
      const locale = Intl.getCanonicalLocales(tag)[0];
      const language = supportedLanguages.find(
        (supported) => supported === new Intl.Locale(locale).language,
      );
      if (language) return { language, locale };
    } catch {
      // Ignore malformed preferences and continue through the browser's list.
    }
  }
  return { language: 'en' as const, locale: 'en-US' };
}
