import { Box, Paper, Stack, Typography } from '@mui/material';
import type { ReactNode } from 'react';
import { useTranslation } from 'react-i18next';
import { LanguageSelect } from './language-select';
import { Brand } from './shell';

// Both auth pages share the same surfaces and desktop-only presentation panel.
export function AuthLayout({
  title,
  description,
  children,
}: {
  title: ReactNode;
  description: string;
  children: ReactNode;
}) {
  const { t } = useTranslation();
  return (
    <Box
      sx={{
        display: 'grid',
        gridTemplateColumns: { xs: '1fr', lg: '1fr 1fr' },
        minHeight: '100dvh',
      }}
    >
      <Paper
        component="section"
        variant="soft"
        square
        sx={{
          display: { xs: 'none', lg: 'flex' },
          flexDirection: 'column',
          justifyContent: 'space-between',
          gap: 6,
          p: { lg: 6, xl: 8 },
        }}
      >
        <Brand />
        <Stack spacing={3} sx={{ maxWidth: 480, py: 6 }}>
          <Typography variant="overline" color="text.secondary">
            {t('YOUR LAB, WITH PEACE OF MIND')}
          </Typography>
          <Typography component="p" variant="h2">
            {title}
          </Typography>
          <Typography color="text.secondary">{description}</Typography>
        </Stack>
        <Typography variant="body2" color="text.secondary">
          {t('Everything stays with you.')}
        </Typography>
      </Paper>
      <Box
        component="section"
        sx={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: 4,
          p: { xs: 2, sm: 4, lg: 6 },
        }}
      >
        <Stack
          direction="row"
          sx={{
            width: '100%',
            maxWidth: 480,
            alignItems: 'center',
            justifyContent: 'space-between',
            flexWrap: 'wrap',
            gap: 2,
          }}
        >
          <Box sx={{ display: { xs: 'block', lg: 'none' } }}>
            <Brand />
          </Box>
          <Box sx={{ ml: 'auto' }}>
            <LanguageSelect compact />
          </Box>
        </Stack>
        <Paper
          variant="outlined"
          sx={{ width: '100%', maxWidth: 480, p: { xs: 3, sm: 4 }, my: 'auto' }}
        >
          {children}
        </Paper>
      </Box>
    </Box>
  );
}
