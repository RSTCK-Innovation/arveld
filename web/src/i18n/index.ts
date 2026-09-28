import { createInstance } from 'i18next';
import { initReactI18next } from 'react-i18next';
import { supportedLanguages } from './language';
import en from './locales/en.json';
import fr from './locales/fr.json';

export function createI18n(language = 'en') {
  const instance = createInstance();
  void instance.use(initReactI18next).init({
    resources: { en: { translation: en }, fr: { translation: fr } },
    lng: language,
    fallbackLng: 'en',
    supportedLngs: [...supportedLanguages],
    keySeparator: false,
    nsSeparator: false,
    initAsync: false,
    returnEmptyString: false,
    interpolation: { escapeValue: false },
  });
  return instance;
}
export const i18n = createI18n();
