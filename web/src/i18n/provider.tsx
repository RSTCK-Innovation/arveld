import { createContext, type ReactNode, useContext, useEffect, useState } from 'react';
import { I18nextProvider } from 'react-i18next';
import { setFormatLocale } from './format';
import { i18n } from './index';
import {
  LANGUAGE_KEY,
  type LanguagePreference,
  languagePreference,
  resolveLanguage,
} from './language';

function browserLanguages() {
  return navigator.languages.length ? navigator.languages : [navigator.language];
}
function savedPreference() {
  try {
    return languagePreference(localStorage.getItem(LANGUAGE_KEY));
  } catch {
    return 'auto' as const;
  }
}
function apply(preference: LanguagePreference) {
  const resolved = resolveLanguage(preference, browserLanguages());
  setFormatLocale(resolved.locale);
  document.documentElement.lang = resolved.locale;
  document.documentElement.dir = 'ltr';
  void i18n.changeLanguage(resolved.language);
  document.title = i18n.t('Arveld · Your services, in view.');
  document
    .querySelector('meta[name=description]')
    ?.setAttribute('content', i18n.t('Arveld — a clear view of your services.'));
  return resolved;
}
// Initialize before rendering so the first painted screen uses the detected language.
const initialPreference = savedPreference();
const initial = apply(initialPreference);
const LanguageContext = createContext({
  preference: initialPreference as LanguagePreference,
  ...initial,
  setPreference: (_value: LanguagePreference) => {},
});
export function LanguageProvider({ children }: { children: ReactNode }) {
  const [preference, setPreferenceState] = useState<LanguagePreference>(initialPreference);
  const [resolved, setResolved] = useState(initial);
  function setPreference(value: LanguagePreference) {
    try {
      if (value === 'auto') localStorage.removeItem(LANGUAGE_KEY);
      else localStorage.setItem(LANGUAGE_KEY, value);
    } catch {
      /* The current session can still change language when storage is blocked. */
    }
    setPreferenceState(value);
    setResolved(apply(value));
  }
  useEffect(() => {
    const onBrowserChange = () => {
      if (preference === 'auto') setResolved(apply('auto'));
    };
    const onStorage = (event: StorageEvent) => {
      if (event.key !== LANGUAGE_KEY && event.key !== null) return;
      const value = savedPreference();
      setPreferenceState(value);
      setResolved(apply(value));
    };
    window.addEventListener('languagechange', onBrowserChange);
    window.addEventListener('storage', onStorage);
    return () => {
      window.removeEventListener('languagechange', onBrowserChange);
      window.removeEventListener('storage', onStorage);
    };
  }, [preference]);
  return (
    <I18nextProvider i18n={i18n}>
      <LanguageContext.Provider value={{ preference, ...resolved, setPreference }}>
        {children}
      </LanguageContext.Provider>
    </I18nextProvider>
  );
}
export function useLanguage() {
  return useContext(LanguageContext);
}
