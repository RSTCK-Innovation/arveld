import '@fontsource-variable/inter/wght.css';
import '@fontsource-variable/manrope/wght.css';
import { CssBaseline, ThemeProvider } from '@mui/material';
import { QueryClientProvider } from '@tanstack/react-query';
import { RouterProvider } from '@tanstack/react-router';
import React, { useMemo } from 'react';
import ReactDOM from 'react-dom/client';
import { NotificationProvider } from './components/notifications';
import { queryClient } from './data/session';
import { LanguageProvider, useLanguage } from './i18n/provider';
import { router } from './router';
import { makeTheme } from './theme';

function App() {
  const { language } = useLanguage();
  const theme = useMemo(() => makeTheme(language), [language]);
  return (
    <ThemeProvider theme={theme}>
      <CssBaseline />
      <NotificationProvider>
        <QueryClientProvider client={queryClient}>
          <RouterProvider router={router} />
        </QueryClientProvider>
      </NotificationProvider>
    </ThemeProvider>
  );
}
const root = document.getElementById('root');
if (!root) throw new Error('Root element not found.');
ReactDOM.createRoot(root).render(
  <React.StrictMode>
    <LanguageProvider>
      <App />
    </LanguageProvider>
  </React.StrictMode>,
);
