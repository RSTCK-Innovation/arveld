import type { AlertColor } from '@mui/material';
import { Alert, Snackbar } from '@mui/material';
import { createContext, type ReactNode, useCallback, useContext, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { errorMessage } from '../i18n/messages';

type Notice = {
  id: number;
  severity: AlertColor;
  message: string | Error;
};
type Notify = (message: string | Error, severity?: AlertColor) => void;
const NotificationContext = createContext<Notify | null>(null);
export function NotificationProvider({ children }: { children: ReactNode }) {
  const { t } = useTranslation();
  const [queue, setQueue] = useState<Notice[]>([]);
  const notify = useCallback<Notify>((message, severity = 'success') => {
    setQueue((items) => [
      ...items.slice(-4),
      {
        id: performance.now(),
        message,
        severity,
      },
    ]);
  }, []);
  const close = () => setQueue((items) => items.slice(1));
  const current = queue[0];
  return (
    <NotificationContext.Provider value={notify}>
      {children}
      <Snackbar
        key={current?.id}
        open={!!current}
        autoHideDuration={4500}
        anchorOrigin={{
          vertical: 'bottom',
          horizontal: 'right',
        }}
        onClose={(_, reason) => {
          if (reason !== 'clickaway') close();
        }}
      >
        <Alert
          severity={current?.severity}
          variant="filled"
          onClose={close}
          closeText={t('Dismiss notification')}
          sx={{ width: '100%' }}
        >
          {current ? errorMessage(current.message) : ''}
        </Alert>
      </Snackbar>
    </NotificationContext.Provider>
  );
}
export function useNotify() {
  const notify = useContext(NotificationContext);
  if (!notify) throw new Error('NotificationProvider is missing.');
  return notify;
}
