import { MenuItem, TextField } from '@mui/material';
import { useTranslation } from 'react-i18next';
import { languagePreference } from '../i18n/language';
import { useLanguage } from '../i18n/provider';
export function LanguageSelect({ compact = false }: { compact?: boolean }) {
  const { t } = useTranslation();
  const { preference, setPreference } = useLanguage();
  return (
    <TextField
      fullWidth
      select
      label={t('Language')}
      value={preference}
      onChange={(event) => setPreference(languagePreference(event.target.value))}
      helperText={
        compact
          ? undefined
          : t('The browser language is used by default. Your choice is saved on this device.')
      }
      sx={compact ? { maxWidth: '15rem' } : undefined}
    >
      <MenuItem value="auto">{t('Automatic (browser)')}</MenuItem>
      <MenuItem value="fr">
        <span lang="fr">{t('French', { lng: 'fr' })}</span>
      </MenuItem>
      <MenuItem value="en">
        <span lang="en">English</span>
      </MenuItem>
    </TextField>
  );
}
