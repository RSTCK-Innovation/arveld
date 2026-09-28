import Box from '@mui/material/Box';
import { useLocation } from '@tanstack/react-router';
import { useTranslation } from 'react-i18next';
import { ButtonLink } from './links';

export function SettingsNavigation() {
  const { t } = useTranslation();
  const { pathname } = useLocation();
  return (
    <Box
      component="nav"
      aria-label={t('Settings')}
      sx={{ display: 'flex', flexWrap: 'wrap', gap: 1, mb: 3 }}
    >
      {[
        { to: '/settings', label: t('General') },
        { to: '/settings/apikeys', label: t('API keys') },
        { to: '/settings/agent-keys', label: t('Agent keys') },
        { to: '/settings/instance', label: t('Arveld health') },
      ].map(({ to, label }) => (
        <ButtonLink
          key={to}
          to={to}
          activeOptions={{ exact: true }}
          color={pathname === to ? 'primary' : 'inherit'}
          variant={pathname === to ? 'contained' : 'text'}
        >
          {label}
        </ButtonLink>
      ))}
    </Box>
  );
}
